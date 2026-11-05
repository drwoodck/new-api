package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/asyncimage"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// RelaySyncImageAsTask 把「同步上游的慢生图请求」包装成任务。
//
// 背景:有些同步图片渠道出图要 2–5 分钟。请求一路阻塞在源站,Cloudflare 等不到
// 100 秒就回 524 —— 客户端拿到网关错误,而上游随后其实生成成功了,结果谁也拿不到。
//
// 这里的做法:对显式配置了 `async_image_provider = "sync_task"` 的渠道/模型,
// 立刻建任务行并返回 task_id,把原来的同步中转丢到后台跑,跑完把结果写回任务行;
// 客户端照常轮询 GET /v1/images/{task_id}。请求耗时降到毫秒级,100 秒上限再也碰不到。
//
// 计费不在这里做:后台跑的仍然是标准同步链路(controller.Relay),预扣/结算/退款
// 全部沿用它的逻辑,任务行只承载状态与结果。
func RelaySyncImageAsTask(c *gin.Context) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "读取请求体失败: " + err.Error(),
			"type":    "invalid_request_error",
			"code":    "read_request_body_failed",
		}})
		return
	}
	body, err := storage.Bytes()
	if err != nil || len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "请求体为空,无法提交图片生成任务",
			"type":    "invalid_request_error",
			"code":    "empty_request_body",
		}})
		return
	}

	modelName := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     model.GenerateTaskID(),
		UserId:     c.GetInt("id"),
		Group:      common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		ChannelId:  common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		Platform:   constant.TaskPlatformSyncImage,
		Status:     model.TaskStatusInProgress,
		Progress:   "10%",
		SubmitTime: now,
		StartTime:  now,
		ModelName:  modelName,
		Properties: model.Properties{OriginModelName: modelName},
	}
	if err := task.Insert(); err != nil {
		common.SysError("insert sync image task error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"message": "创建图片任务失败,请稍后重试",
			"type":    "server_error",
			"code":    "create_task_failed",
		}})
		return
	}

	// 先把 task_id 交出去,再开始后台执行 —— 顺序反了就会重新踩 524。
	c.JSON(http.StatusOK, gin.H{
		"task_id":    task.TaskID,
		"id":         task.TaskID,
		"object":     "generation.task",
		"model":      modelName,
		"status":     "queued",
		"created_at": now,
	})

	// 交接给后台前把需要的东西全部拷出来:handler 返回后 gin 会复用这个 Context,
	// 而原始请求的 context 会随连接关闭被取消(后台任务不能挂在它上面)。
	keys := make(map[string]any, len(c.Keys))
	for k, v := range c.Keys {
		keys[k] = v
	}
	params := c.Params
	req := c.Request.Clone(context.Background())
	if q := req.URL.Query(); q.Has("async") {
		// 上游同步接口不认这个客户端侧开关,别把它转发出去
		q.Del("async")
		req.URL.RawQuery = q.Encode()
	}

	go runSyncImageTask(req, keys, params, body, task)
}

// runSyncImageTask 在后台跑一次标准同步中转,把结果写回任务行。
func runSyncImageTask(req *http.Request, keys map[string]any, params gin.Params, body []byte, task *model.Task) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("sync image task %s panic: %v", task.TaskID, r))
			finishSyncImageTask(task, model.TaskStatusFailure, fmt.Sprintf("内部错误: %v", r), nil)
		}
	}()

	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	cw := newCaptureResponseWriter()
	inner, _ := gin.CreateTestContext(cw)
	inner.Request = req
	inner.Params = params
	for k, v := range keys {
		inner.Set(k, v)
	}

	Relay(inner, types.RelayFormatOpenAIImage)

	respBody := cw.Body()
	statusCode := cw.Status()
	urls := asyncimage.ResultURLsFromRaw(respBody)
	if statusCode >= 200 && statusCode < 300 && len(urls) > 0 {
		finishSyncImageTask(task, model.TaskStatusSuccess, "", urls)
		return
	}
	finishSyncImageTask(task, model.TaskStatusFailure, syncImageFailReason(statusCode, respBody), nil)
}

// finishSyncImageTask 落定任务终态。失败原因保持上游原话(去掉 URL),便于对账。
func finishSyncImageTask(task *model.Task, status model.TaskStatus, failReason string, urls []string) {
	task.Status = status
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	if len(urls) > 0 {
		task.PrivateData.ResultURL = urls[0]
		payload, err := json.Marshal(map[string]any{
			"data": urlsToPayload(urls),
		})
		if err == nil {
			task.Data = json.RawMessage(payload)
		}
	}
	if status == model.TaskStatusFailure {
		task.FailReason = failReason
	} else {
		task.FailReason = ""
	}
	err := model.DB.Model(task).
		Select("status", "progress", "finish_time", "fail_reason", "data", "private_data").
		Updates(task).Error
	if err != nil {
		common.SysError(fmt.Sprintf("update sync image task %s error: %v", task.TaskID, err))
	}
}

func urlsToPayload(urls []string) []map[string]string {
	items := make([]map[string]string, 0, len(urls))
	for _, u := range urls {
		items = append(items, map[string]string{"url": u})
	}
	return items
}

// syncImageFailReason 把同步链路的失败响应转成一句可读原因。
func syncImageFailReason(statusCode int, body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(body, &payload); err == nil {
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(payload.Message); msg != "" {
			return msg
		}
	}
	preview := strings.TrimSpace(string(body))
	if len(preview) > 300 {
		preview = preview[:300]
	}
	if preview == "" {
		if statusCode >= 200 && statusCode < 300 {
			return "上游没有返回图片地址"
		}
		return fmt.Sprintf("上游返回 HTTP %d", statusCode)
	}
	return fmt.Sprintf("上游返回 HTTP %d: %s", statusCode, preview)
}

// captureResponseWriter 收集后台同步调用的响应体与状态码。
//
// 必须实现完整的 gin.ResponseWriter:中转链路会设置响应头、写 JSON,失败时还会
// 带状态码;拿不到这些就没法判断这一单到底成功没有。
type captureResponseWriter struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	size    int
	written bool
}

func newCaptureResponseWriter() *captureResponseWriter {
	return &captureResponseWriter{header: http.Header{}, status: http.StatusOK}
}

func (w *captureResponseWriter) Header() http.Header { return w.header }

func (w *captureResponseWriter) WriteHeader(code int) {
	if w.written {
		return
	}
	w.status = code
	w.written = true
}

func (w *captureResponseWriter) WriteHeaderNow() { w.written = true }

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	w.written = true
	n, err := w.body.Write(data)
	w.size += n
	return n, err
}

func (w *captureResponseWriter) WriteString(s string) (int, error) {
	w.written = true
	n, err := w.body.WriteString(s)
	w.size += n
	return n, err
}

func (w *captureResponseWriter) Status() int {
	if !w.written {
		return http.StatusOK
	}
	return w.status
}

func (w *captureResponseWriter) Size() int { return w.size }

func (w *captureResponseWriter) Written() bool { return w.written }

// Body 返回已经写下的响应体副本。
func (w *captureResponseWriter) Body() []byte {
	return append([]byte(nil), w.body.Bytes()...)
}

func (w *captureResponseWriter) Flush() {}

func (w *captureResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack not supported")
}

func (w *captureResponseWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}

func (w *captureResponseWriter) Pusher() http.Pusher { return nil }

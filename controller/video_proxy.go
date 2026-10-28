package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	// 优先读本地副本。命中则跳过渠道查询、URL 解析、SSRF 校验与上游请求。
	//
	// 缺失是正常情况不是错误,三种成因都必须静默回退到下面的实时透传:
	//   - 多节点部署,文件在别的节点磁盘上
	//   - 保留期已过,文件被清理
	//   - 落盘当时失败(厂商链接已过期、上游限流)
	if shouldTryLocalArtifact(task.PrivateData.ArtifactPath, task.PrivateData.ArtifactNode, common.NodeName) {
		if f, err := service.OpenArtifact(task.PrivateData.ArtifactPath); err == nil {
			defer f.Close()
			if st, statErr := f.Stat(); statErr == nil {
				// 与透传路径保持同一个缓存策略(原第 178 行)
				c.Header("Cache-Control", "public, max-age=86400")
				c.Header("Content-Type", contentTypeForArtifactPath(task.PrivateData.ArtifactPath))
				// ServeContent 顺带支持 Range 请求 —— 透传路径不支持,
				// 这让本地命中时拖进度条能秒定位
				http.ServeContent(c.Writer, c.Request, filepath.Base(task.PrivateData.ArtifactPath), st.ModTime(), f)
				return
			}
		}
		// 打开或 stat 失败 —— 落到下面的实时透传,不记 error 级日志
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client := service.GetSSRFProtectedHTTPClient()
	if proxy != "" {
		// 渠道代理路径的连接由代理侧建立，无法做拨号时逐 IP 校验，
		// 因此后面对 videoURL 保留请求前的一次性 SSRF 校验。
		client, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
			return
		}
	}

	// 只给「建连 + 拿到响应头」设上限;整条 body 不设总时长(见下方流式拷贝)。
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		// fdai 等新API形状上游:轮询响应直接给结果直链(已存 ResultURL),它们
		// 没有 /content 子路径 —— 透传 /content 只会 404→502(实测)。有直链就
		// 流式转发直链;真 OpenAI/Sora(无直链)才走 /content 透传。
		if direct := taskDirectVideoURL(task); direct != "" {
			videoURL = direct
		} else {
			videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
			req.Header.Set("Authorization", "Bearer "+singleLineKey(channel, task))
		}
	default:
		// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
		videoURL = task.GetResultURL()
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	var validateErr error
	if proxy == "" {
		validateErr = service.ValidateSSRFProtectedFetchURL(videoURL)
	} else {
		fetchSetting := system_setting.GetFetchSetting()
		validateErr = common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
	}
	if validateErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, validateErr))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", validateErr))
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	// 抖音/TOS 这类 CDN 封 Go 的 TLS 指纹:同一 IP、同一 URL、同样的头,curl(OpenSSL)
	// 200 而 Go 一律 403(openresty `Forbid_code: 020200` / "ERROR: ACCESS DENIED"),
	// 而厂商的结果链接基本都是 307/302 跳到这类 CDN —— 让中转站代取必然失败
	// (线上 7 天 1507 次 /content 请求,0 次成功)。
	//
	// 所以这里**不跟随跳转**,把 Location 原样 302 给客户端:客户端用自己的 TLS 栈
	// 直连 CDN(实测 reqwest/schannel 200、8.6 MB/s),既绕开指纹封禁,中转站也不用
	// 转发几十 MB 的流量。真正需要中转站带 key 去取的(OpenAI /content、Gemini、
	// x-goog-api-key)拿到的本来就是 200 + 字节,走下面的流式分支。
	noRedirectClient := *client
	noRedirectClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// 客户端的 Range 原样转发:大文件断点续传要它。
	if rng := c.GetHeader("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	headerTimer := time.AfterFunc(videoUpstreamHeaderTimeout, cancel)
	resp, err := noRedirectClient.Do(req)
	headerTimer.Stop()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if location, ok := videoRedirectLocation(videoURL, resp); ok {
		// 上游一句 302 就能把客户端引向内网地址,跳转目标必须同样过 SSRF 校验。
		if locationErr := validateVideoFetchURL(location, proxy); locationErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream redirect target blocked for task %s: %v", taskID, locationErr))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Upstream redirect target blocked")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Video source redirects for task %s, handing URL to client: %s", taskID, location))
		// 临时签名链接,不能让任何一层缓存住。
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, location)
		return
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		// 逐跳头不能转发:它们描述的是「上游那一段连接」,不是这条连接。
		if strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Connection") {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)
	flush := func() {
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}
	flush()

	// 空闲看门狗:连续 videoUpstreamIdleTimeout 没有任何字节进展才判定卡死。
	// 「慢但一直在走」的下载不再受总时长限制 —— 旧实现给整条 body 套了 60s 的
	// ctx,40 MB 的片子只要低于约 680 KB/s 就必然被拦腰掐断。
	idle := time.AfterFunc(videoUpstreamIdleTimeout, cancel)
	defer idle.Stop()
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(videoUpstreamIdleTimeout)
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content for task %s: %s", taskID, writeErr.Error()))
				return
			}
			flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content for task %s: %s", taskID, readErr.Error()))
			}
			return
		}
	}
}

const (
	// 上游响应头等待上限:建连/排队超过这么久就判定失败(而不是像以前那样把
	// 「等头」和「读完 body」共用一个 60s 预算)。
	videoUpstreamHeaderTimeout = 60 * time.Second
	// body 空闲上限:连续这么久没有任何字节进展才判定卡死。
	videoUpstreamIdleTimeout = 120 * time.Second
)

// videoRedirectLocation 判断上游响应是不是「结果直链的跳转」,是则解析出绝对地址。
// 只认带 Location 的 3xx。客户端自己发 Range 时也可以用:Range 是普通头,客户端
// 跟随 302 时会一并带给 CDN。
func videoRedirectLocation(baseURL string, resp *http.Response) (string, bool) {
	if resp == nil || resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", false
	}
	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(loc)
	if err != nil {
		return "", false
	}
	return base.ResolveReference(ref).String(), true
}

// validateVideoFetchURL 对「即将被访问的地址」做 SSRF 校验。
func validateVideoFetchURL(target, proxy string) error {
	if proxy == "" {
		return service.ValidateSSRFProtectedFetchURL(target)
	}
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(target, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain)
}

// artifactContentTypes 是 service.artifactExtByType 的反向映射。
// 本地分支没有上游响应可复制 header,必须自己推断 Content-Type ——
// 留空会让浏览器猜,可能猜成 text/html 并把视频当文本渲染。
var artifactContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
}

func contentTypeForArtifactPath(relPath string) string {
	if ct, ok := artifactContentTypes[strings.ToLower(filepath.Ext(relPath))]; ok {
		return ct
	}
	return "application/octet-stream"
}

// shouldTryLocalArtifact 判断是否值得尝试读本地文件。
//
// 落盘节点为空时仍尝试:单节点部署下 NodeName 可能是空串,
// 严格比对会让落盘完全失效。文件真不在时 OpenArtifact 会失败并自然回退。
func shouldTryLocalArtifact(artifactPath, artifactNode, thisNode string) bool {
	if artifactPath == "" {
		return false
	}
	if artifactNode != "" && thisNode != "" && artifactNode != thisNode {
		return false
	}
	return true
}

// taskDirectVideoURL 从任务里提取上游结果直链。优先 ResultURL,但跳过旧版
// 写成的自引用 /content 代理地址(那会让代理请求自己,而 Sora 渠道的上游根本
// 没有 /content 子路径,实测 404→502 死循环);再回退 task.Data 里的
// url/video_url 字段(轮询响应原文,直链的另一个落点)。都没有返回空串,
// 调用方自行走 /content 透传。
func taskDirectVideoURL(task *model.Task) string {
	if r := strings.TrimSpace(task.GetResultURL()); r != "" &&
		!strings.HasSuffix(r, "/v1/videos/"+task.TaskID+"/content") &&
		strings.HasPrefix(r, "http") {
		return r
	}
	var dataMap map[string]any
	if err := common.Unmarshal(task.Data, &dataMap); err == nil {
		for _, field := range []string{"url", "video_url"} {
			if v, ok := dataMap[field].(string); ok {
				if v = strings.TrimSpace(v); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// singleLineKey 取一行合法的 key 给 Authorization 头。多 key 渠道的 ch.Key
// 是「key1\nkey2」整串,直接进请求头会被 net/http 以 invalid header field
// value 拒绝 —— 优先任务回存的提交 key,兜底取整串的第一行。
func singleLineKey(channel *model.Channel, task *model.Task) string {
	if task.PrivateData.Key != "" {
		return task.PrivateData.Key
	}
	if i := strings.IndexByte(channel.Key, '\n'); i > 0 {
		return channel.Key[:i]
	}
	return channel.Key
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}

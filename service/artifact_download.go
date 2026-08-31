package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 单个产物的下载超时。视频可能几十 MB,但也不能无界等待 ——
// video_proxy.go 对代理请求用 60s,下载给更宽的预算。
const artifactDownloadTimeout = 5 * time.Minute

// shouldDownloadURL 判断某个 ResultURL 值是否值得落盘。
//
// 三种不下载:
//   - 空 —— 任务没有产物 URL
//   - data: —— 内容已内联,代理的 writeVideoDataURL 直接处理
//   - 我们自己的代理路径 —— 下载它会自我递归
func shouldDownloadURL(resultURL string) bool {
	if resultURL == "" {
		return false
	}
	if strings.HasPrefix(resultURL, "data:") {
		return false
	}
	// 代理 URL 形如 /v1/videos/{id}/content,可能带或不带主机名
	if strings.Contains(resultURL, "/videos/") && strings.HasSuffix(resultURL, "/content") {
		return false
	}
	return true
}

// fetchAndStore 拉取 URL 并落盘。复用 DoDownloadRequest —— 它已包含
// SSRF 校验、Worker 代理路由与日志遮蔽(service/download.go)。
// 它自身不设超时,所以调用方(这里)负责包 context。
func fetchAndStore(ctx context.Context, taskID string, url string) (string, int64, error) {
	// 先看 context —— 已取消就别发请求了。放在 DoDownloadRequest 之后检查的话,
	// 一个早已超时的任务仍会打一次上游,白费一次带宽和一个连接。
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	resp, err := DoDownloadRequest(url, "artifact-persist")
	if err != nil {
		return "", 0, fmt.Errorf("拉取失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("上游返回 %d", resp.StatusCode)
	}

	// 读 body 之前再看一次 —— 请求往返期间可能已经超时
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	contentType := resp.Header.Get("Content-Type")
	return StoreArtifact(ctx, taskID, contentType, resp.Body)
}

// DownloadTaskArtifact 为某个已完成任务落盘产物,并把路径写回 DB。
//
// 幂等:已有 ArtifactPath 且文件存在时直接返回。
func DownloadTaskArtifact(ctx context.Context, taskID string) error {
	task, err := model.GetTaskByTaskID(taskID)
	if err != nil {
		return fmt.Errorf("取任务失败: %w", err)
	}
	if task == nil {
		return fmt.Errorf("任务不存在: %s", taskID)
	}
	if task.Status != model.TaskStatusSuccess {
		return fmt.Errorf("任务未成功,跳过落盘")
	}

	// 已落盘且文件仍在 —— 幂等返回
	if task.PrivateData.ArtifactPath != "" &&
		task.PrivateData.ArtifactNode == common.NodeName {
		if f, err := OpenArtifact(task.PrivateData.ArtifactPath); err == nil {
			f.Close()
			return nil
		}
	}

	resultURL := task.GetResultURL()
	if !shouldDownloadURL(resultURL) {
		return nil
	}

	relPath, size, err := fetchAndStore(ctx, taskID, resultURL)
	if err != nil {
		return err
	}

	// 写回 DB。PrivateData 不参与 taskSnapshot 的相等性判定,
	// 所以不能走 UpdateWithStatus 的 CAS —— 用专门的字段更新。
	if err := model.UpdateTaskArtifact(taskID, relPath, common.NodeName); err != nil {
		// 文件已落盘但 DB 没记上 —— 下次轮询会重新下载并覆盖,
		// 旧文件由保留期清理兜底。记日志便于排查。
		common.SysError(fmt.Sprintf(
			"[artifact] 任务 %s 产物已落盘(%d 字节)但更新 DB 失败: %v", taskID, size, err))
		return err
	}

	common.SysLog(fmt.Sprintf("[artifact] 任务 %s 产物已落盘: %s(%d 字节)", taskID, relPath, size))
	return nil
}

// TriggerArtifactDownload 异步启动下载。永不返回错误 ——
// 落盘是尽力而为的优化,失败只意味着代理退回实时透传(当前行为)。
//
// 异步是必要的:调用点在 15 秒一轮的任务轮询批处理里
// (service/task_polling.go),同步下载一个大视频会拖慢整批任务的处理。
func TriggerArtifactDownload(taskID string) {
	if taskID == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				common.SysError(fmt.Sprintf("[artifact] 下载 goroutine panic: %v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), artifactDownloadTimeout)
		defer cancel()

		if err := DownloadTaskArtifact(ctx, taskID); err != nil {
			// 降级为 debug 级别 —— 失败是预期内的(URL 已过期、上游限流),
			// 且不影响功能(代理会实时透传)。用 error 级别会淹没日志。
			common.SysLog(fmt.Sprintf("[artifact] 任务 %s 落盘失败(将退回实时透传): %v", taskID, err))
		}
	}()
}

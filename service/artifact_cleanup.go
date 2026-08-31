package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	artifactCleanupInterval = 6 * time.Hour
	// .tmp 存活超过这个时长必定是被杀死的下载留下的 ——
	// 单个下载的超时预算是 5 分钟(artifactDownloadTimeout)
	staleTempThreshold = 1 * time.Hour
)

// StartArtifactCleanup 启动产物清理。
//
// 与 auth_cleanup.go 不同,这里**不做 IsMasterNode 门控** ——
// master 节点删不掉其他节点磁盘上的文件。任务轮询没有 master 门控
// (谁赢 CAS 谁落盘),所以文件散落各节点。每个节点跑自己的清理器,
// 只处理自己磁盘上的文件,合起来才能覆盖全部。
func StartArtifactCleanup() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				common.SysError(fmt.Sprintf("[artifact-cleanup] panic: %v", r))
			}
		}()

		// 启动后先等一会,避开启动高峰
		time.Sleep(5 * time.Minute)

		ticker := time.NewTicker(artifactCleanupInterval)
		defer ticker.Stop()

		runArtifactCleanupOnce()
		for range ticker.C {
			runArtifactCleanupOnce()
		}
	}()
}

func runArtifactCleanupOnce() {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("[artifact-cleanup] 单轮 panic: %v", r))
		}
	}()

	if n, err := cleanupStaleTempFiles(); err != nil {
		common.SysError(fmt.Sprintf("[artifact-cleanup] 清理临时文件失败: %v", err))
	} else if n > 0 {
		common.SysLog(fmt.Sprintf("[artifact-cleanup] 清理了 %d 个残留临时文件", n))
	}

	if n, err := cleanupOrphanFiles(common.ArtifactRetentionDays); err != nil {
		common.SysError(fmt.Sprintf("[artifact-cleanup] 清理过期产物失败: %v", err))
	} else if n > 0 {
		common.SysLog(fmt.Sprintf("[artifact-cleanup] 清理了 %d 个过期产物", n))
	}
}

// cleanupOrphanFiles 按文件修改时间删除超过保留期的产物。
//
// 按 mtime 而非 DB 记录判断,能同时覆盖两种情况:
//   - 正常过期的产物
//   - Task 2 失败路径留下的孤儿(文件已落盘但 DB 更新失败)
//
// 不需要先查 DB —— mtime 超过保留期就该删,DB 里那条 ArtifactPath
// 变成悬空引用后,代理的 OpenArtifact 会失败并自然回退到实时透传。
func cleanupOrphanFiles(retentionDays int) (int, error) {
	// 非正的保留期一律不清理 —— 配置写成 0 不该导致全量删除
	if retentionDays <= 0 {
		return 0, nil
	}

	root := ArtifactDir()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	removed := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 单个文件出错不中断整轮
		}
		if info.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		if rmErr := os.Remove(path); rmErr == nil {
			removed++
		}
		return nil
	})

	return removed, err
}

// cleanupStaleTempFiles 删除残留的 .tmp —— 下载中途进程被杀会留下它们。
// 活跃下载的 .tmp 不到一小时,不会被误删。
func cleanupStaleTempFiles() (int, error) {
	root := ArtifactDir()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0, nil
	}

	cutoff := time.Now().Add(-staleTempThreshold)
	removed := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".tmp") {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		if rmErr := os.Remove(path); rmErr == nil {
			removed++
		}
		return nil
	})

	return removed, err
}

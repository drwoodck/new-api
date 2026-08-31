package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 测试注入点。生产代码永远不设它。
var artifactDirOverride string

func ArtifactDir() string {
	if artifactDirOverride != "" {
		return artifactDirOverride
	}
	return common.ArtifactStorageDir
}

// content-type → 扩展名。只列实际会遇到的几种,其余落 .bin。
// 不用 mime.ExtensionsByType 是因为它对 image/jpeg 返回 .jfif 之类的意外结果。
var artifactExtByType = map[string]string{
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/quicktime": ".mov",
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"audio/mpeg":      ".mp3",
	"audio/wav":       ".wav",
}

func extForContentType(ct string) string {
	if ct == "" {
		return ".bin"
	}
	// 去掉 "; charset=..." 之类的参数
	if base, _, err := mime.ParseMediaType(ct); err == nil {
		ct = base
	}
	if ext, ok := artifactExtByType[strings.ToLower(ct)]; ok {
		return ext
	}
	return ".bin"
}

// taskID 只允许这些字符 —— 与 model.Task.TaskID 的实际取值范围一致,
// 且排除任何路径分隔符与 `.`,从根上挡掉遍历。
func safeTaskIDSegment(taskID string) (string, error) {
	if taskID == "" {
		return "", errors.New("taskID 为空")
	}
	for _, r := range taskID {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return "", fmt.Errorf("taskID 含非法字符: %q", taskID)
		}
	}
	return taskID, nil
}

// ArtifactPathFor 规划某任务产物的存放位置。
//
// 分两级子目录(取 taskID 前两字符),避免单目录堆几十万文件 ——
// ext4 与 NTFS 在单目录文件数很大时目录操作会显著变慢。
func ArtifactPathFor(taskID string, contentType string) (relPath string, absPath string, err error) {
	seg, err := safeTaskIDSegment(taskID)
	if err != nil {
		return "", "", err
	}

	// 取**末尾**两个字符分片,不是开头。GenerateTaskID 返回 "task_" + 随机串
	// (model/task.go),所以开头两个字符恒为 "ta" —— 按前缀分片会把每一个产物
	// 都堆进同一个目录,正是分片要避免的情况。末尾字符来自随机部分,分布均匀。
	shard := "00"
	if len(seg) >= 2 {
		shard = strings.ToLower(seg[len(seg)-2:])
	}

	relPath = filepath.Join(shard, seg+extForContentType(contentType))
	absPath = filepath.Join(ArtifactDir(), relPath)

	// 二次确认没逃逸 —— 即便上面的字符白名单被改坏,这一层仍能挡住
	root, err := filepath.Abs(ArtifactDir())
	if err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", "", fmt.Errorf("路径逃逸出存储根目录: %s", relPath)
	}

	return relPath, absPath, nil
}

// StoreArtifact 把 body 原子地写入产物目录。
//
// 先写 .tmp 再 rename —— 下载中途失败不能留下半个文件,
// 否则代理会把它当完整产物读出去,用户看到损坏的视频。
func StoreArtifact(ctx context.Context, taskID string, contentType string, body io.Reader) (string, int64, error) {
	relPath, absPath, err := ArtifactPathFor(taskID, contentType)
	if err != nil {
		return "", 0, err
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("建目录失败: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(absPath), ".dl-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()

	// 失败路径上必须清掉临时文件
	success := false
	defer func() {
		tmp.Close()
		if !success {
			os.Remove(tmpName)
		}
	}()

	// context 已取消时不再写入,交给调用方处理取消错误
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	size, err := io.Copy(tmp, body)
	if err != nil {
		return "", 0, fmt.Errorf("写入失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync 失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("关闭失败: %w", err)
	}

	if err := os.Rename(tmpName, absPath); err != nil {
		return "", 0, fmt.Errorf("重命名失败: %w", err)
	}
	success = true

	return relPath, size, nil
}

// OpenArtifact 打开已落盘的产物。relPath 从 DB 读出,所以仍要防逃逸。
func OpenArtifact(relPath string) (*os.File, error) {
	if relPath == "" {
		return nil, errors.New("relPath 为空")
	}
	root, err := filepath.Abs(ArtifactDir())
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Join(root, relPath))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("relPath 逃逸出存储根目录: %s", relPath)
	}
	return os.Open(abs)
}

func RemoveArtifact(relPath string) error {
	if relPath == "" {
		return nil
	}
	root, err := filepath.Abs(ArtifactDir())
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(filepath.Join(root, relPath))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return fmt.Errorf("relPath 逃逸出存储根目录: %s", relPath)
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

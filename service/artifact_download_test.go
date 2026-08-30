package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// 下载成功后应返回相对路径与实际写入的字节数
func TestDownloadArtifactStoresBody(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Write([]byte("fake video bytes"))
	}))
	defer srv.Close()

	rel, size, err := fetchAndStore(context.Background(), "task-dl-1", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if size != 16 {
		t.Errorf("size = %d, 期望 16", size)
	}
	if !strings.HasSuffix(rel, ".mp4") {
		t.Errorf("期望 .mp4 后缀,得到 %q", rel)
	}
	if _, err := os.Stat(dir + "/" + rel); err != nil {
		t.Errorf("文件不存在: %v", err)
	}
}

// 上游返回非 200 时不能落盘 —— 否则会把一个错误页当产物存下来,
// 用户预览时拿到一段 HTML
func TestDownloadArtifactRejectsNon200(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("<html>not found</html>"))
	}))
	defer srv.Close()

	_, _, err := fetchAndStore(context.Background(), "task-404", srv.URL)
	if err == nil {
		t.Fatal("期望报错")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("错误信息应含状态码,得到: %v", err)
	}
	// 且不留下文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		sub, _ := os.ReadDir(dir + "/" + e.Name())
		if len(sub) > 0 {
			t.Errorf("失败的下载留下了文件: %v", sub)
		}
	}
}

// data: URL 不需要下载 —— 内容已经内联在 ResultURL 里,
// 代理有专门的 writeVideoDataURL 快速路径处理它
func TestDownloadSkipsDataURLs(t *testing.T) {
	if shouldDownloadURL("data:video/mp4;base64,AAAA") {
		t.Error("data: URL 不该触发下载")
	}
	if shouldDownloadURL("") {
		t.Error("空 URL 不该触发下载")
	}
	if !shouldDownloadURL("https://example.com/v.mp4") {
		t.Error("普通 https URL 应触发下载")
	}
}

// 代理 URL 也不该下载 —— 那是我们自己的地址,下载它会自我递归
func TestDownloadSkipsOwnProxyURLs(t *testing.T) {
	for _, u := range []string{
		"/videos/task-x/content",
		"https://relay.example.com/videos/task-x/content",
	} {
		if shouldDownloadURL(u) {
			t.Errorf("代理 URL %q 不该触发下载", u)
		}
	}
}

// 超时必须生效 —— 上游挂住时不能让 goroutine 永久泄漏
func TestDownloadRespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	_, _, err := fetchAndStore(ctx, "task-cancel", "https://example.com/v.mp4")
	if err == nil {
		t.Fatal("期望 context 取消导致的错误")
	}
}

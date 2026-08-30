package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// allowLoopbackFetch 让 SSRF 防护放行 httptest 服务器。
//
// 为什么必须有:DoDownloadRequest 走 ValidateSSRFProtectedFetchURL,而默认配置是
// EnableSSRFProtection=true / AllowPrivateIp=false / AllowedPorts=[80,443,8080,8443]。
// httptest.NewServer 绑在 127.0.0.1 的随机高端口上,私网 IP 与端口白名单两道都会拒。
// 不放行的话这些测试测的是「SSRF 拦截生效」,而不是它们声称要测的下载逻辑。
//
// 只放行本次 httptest 的那一个端口,不整体关掉防护 —— 防护本身的行为由
// protected_fetch_client_test.go 覆盖,这里不该把它变成没测过的状态。
// 改的是共享的 *FetchSetting,所以照 configureSSRFTestFetchSetting 的先例用
// t.Cleanup 还原,避免污染同包其它测试。
func allowLoopbackFetch(t *testing.T, serverURL string) {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("解析测试服务器地址失败: %v", err)
	}

	ensureHTTPClientsInitialized(t)

	fetchSetting := system_setting.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() { *fetchSetting = original })

	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = append([]string{}, original.AllowedPorts...)
	if p := parsed.Port(); p != "" {
		fetchSetting.AllowedPorts = append(fetchSetting.AllowedPorts, p)
	}
}

// ensureHTTPClientsInitialized 补上 InitHttpClient()。
//
// 为什么必须有:`ssrfProtectedHTTPClient` 是包级变量,只由 InitHttpClient() 在
// **服务启动时**赋值,测试进程里没人调过它,所以它是 nil。
// DoDownloadRequest 直接 `GetSSRFProtectedHTTPClient().Get(...)`,对 nil
// *http.Client 调 Get 会 panic(nil pointer dereference),而不是返回错误。
//
// 这是 DoDownloadRequest 既有的地雷,不是产物落盘引入的 —— 任何在测试里
// 走这条路径的代码都会踩到。调真正的初始化函数而不是塞一个 &http.Client{},
// 这样测试用的就是生产同一个受保护客户端。
func ensureHTTPClientsInitialized(t *testing.T) {
	t.Helper()
	if ssrfProtectedHTTPClient == nil {
		InitHttpClient()
	}
}

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
	allowLoopbackFetch(t, srv.URL)

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
	allowLoopbackFetch(t, srv.URL)

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

// 已取消的 context 必须让下载报错,而且**不能落盘**。
//
// 原版这个测试用 https://example.com/v.mp4 —— 它会真的发一次外网请求,
// 然后靠「网络失败」拿到一个错误就算通过,根本没测到取消。改成本地
// httptest 服务器:请求打得通,于是唯一能让它报错的原因就是 context 已取消。
func TestDownloadRespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	old := artifactDirOverride
	artifactDirOverride = dir
	defer func() { artifactDirOverride = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Write([]byte("should never be stored"))
	}))
	defer srv.Close()
	allowLoopbackFetch(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	_, _, err := fetchAndStore(ctx, "task-cancel", srv.URL)
	if err == nil {
		t.Fatal("期望 context 取消导致的错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误应能追溯到 context.Canceled,得到: %v", err)
	}

	// 取消后绝不能留下文件 —— 否则代理会把它当完整产物送给用户
	var found []string
	filepath.Walk(dir, func(p string, info os.FileInfo, e error) error {
		if e == nil && !info.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	if len(found) > 0 {
		t.Errorf("已取消的下载留下了文件: %v", found)
	}
}

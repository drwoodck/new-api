package controller

import (
	"net/http"
	"testing"
)

// 线上 7 天 1507 次 /v1/videos/*/content 请求 0 次成功:厂商结果链接 307 跳到
// 抖音/TOS CDN,而这类 CDN 封 Go 的 TLS 指纹(同一 IP、同一 URL:curl 200 /
// Go 403,Forbid_code 020200)。所以代理必须把跳转交给客户端,而不是自己跟随。
//
// 这组用例钉住「什么时候才把跳转交出去」:只认带 Location 的 3xx。
func TestVideoRedirectLocation(t *testing.T) {
	mk := func(status int, location string) *http.Response {
		h := http.Header{}
		if location != "" {
			h.Set("Location", location)
		}
		return &http.Response{StatusCode: status, Header: h}
	}

	const base = "https://api.lecapi.net/v1/videos/task_x/content?expires=1&signature=abc"

	cases := []struct {
		name   string
		resp   *http.Response
		want   string
		wantOK bool
	}{
		{
			name:   "307 绝对地址(抖音 CDN 的真实形态)",
			resp:   mk(http.StatusTemporaryRedirect, "https://v9-videoweb.doubao.com/a/b/c.mp4?a=1&b=2"),
			want:   "https://v9-videoweb.doubao.com/a/b/c.mp4?a=1&b=2",
			wantOK: true,
		},
		{
			name:   "302 相对地址按原始 URL 解析",
			resp:   mk(http.StatusFound, "/files/x.mp4"),
			want:   "https://api.lecapi.net/files/x.mp4",
			wantOK: true,
		},
		{
			name:   "200 直接给字节 —— 说明上游不需要跳转,继续由中转站转发",
			resp:   mk(http.StatusOK, ""),
			wantOK: false,
		},
		{
			name:   "403 是错误不是跳转",
			resp:   mk(http.StatusForbidden, ""),
			wantOK: false,
		},
		{
			name:   "3xx 但没有 Location 不能当作跳转",
			resp:   mk(http.StatusFound, ""),
			wantOK: false,
		},
		{
			name:   "304 没有 Location,也不是结果直链",
			resp:   mk(http.StatusNotModified, ""),
			wantOK: false,
		},
	}

	for _, c := range cases {
		got, ok := videoRedirectLocation(base, c.resp)
		if ok != c.wantOK || (c.wantOK && got != c.want) {
			t.Errorf("%s: 得到 (%q, %v), 期望 (%q, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}

	if _, ok := videoRedirectLocation(base, nil); ok {
		t.Error("nil 响应不能当作跳转")
	}
}

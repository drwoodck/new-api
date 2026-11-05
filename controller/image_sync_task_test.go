package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncImageFailReason(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "openai error shape keeps the upstream message",
			status: http.StatusBadRequest,
			body:   `{"error":{"message":"content policy violation","code":"bad_request"}}`,
			want:   "content policy violation",
		},
		{
			name:   "flat message shape",
			status: http.StatusBadGateway,
			body:   `{"message":"upstream overloaded"}`,
			want:   "upstream overloaded",
		},
		{
			name:   "plain text body is kept as the reason",
			status: http.StatusInternalServerError,
			body:   "boom",
			want:   "上游返回 HTTP 500: boom",
		},
		{
			name:   "empty 2xx means the upstream answered without an image",
			status: http.StatusOK,
			body:   "",
			want:   "上游没有返回图片地址",
		},
		{
			name:   "empty 5xx still reports the status",
			status: http.StatusGatewayTimeout,
			body:   "",
			want:   "上游返回 HTTP 504",
		},
		{
			name:   "huge html error pages are truncated",
			status: http.StatusBadGateway,
			body:   "<html>" + strings.Repeat("x", 500),
			want:   "上游返回 HTTP 502: " + "<html>" + strings.Repeat("x", 294),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, syncImageFailReason(tc.status, []byte(tc.body)))
		})
	}
}

func TestURLsToPayload(t *testing.T) {
	payload := urlsToPayload([]string{"https://a/1.png", "https://a/2.png"})
	assert.Equal(t, []map[string]string{
		{"url": "https://a/1.png"},
		{"url": "https://a/2.png"},
	}, payload)
	assert.Empty(t, urlsToPayload(nil))
}

// 后台同步调用的响应必须能被完整收集:状态码、响应体、写入量,缺一不可 ——
// 判断这一单成功与否全靠它。
func TestCaptureResponseWriter(t *testing.T) {
	w := newCaptureResponseWriter()
	assert.Equal(t, http.StatusOK, w.Status(), "未写入时按 200 处理")
	assert.False(t, w.Written())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	n, err := w.WriteString(`{"error":"boom"}`)
	assert.NoError(t, err)
	assert.Equal(t, len(`{"error":"boom"}`), n)
	assert.True(t, w.Written())
	assert.Equal(t, http.StatusBadRequest, w.Status())
	assert.Equal(t, n, w.Size())
	assert.Equal(t, `{"error":"boom"}`, string(w.Body()))
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// 重复 WriteHeader 不能覆盖首个状态码(与 net/http 一致)
	w.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusBadRequest, w.Status())

	// Body 返回副本,调用方改动不会影响已收集的响应
	body := w.Body()
	body[0] = 'X'
	assert.Equal(t, `{"error":"boom"}`, string(w.Body()))
}

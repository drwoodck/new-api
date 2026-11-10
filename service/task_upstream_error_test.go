package service

import (
	"net/http"
	"strings"
	"testing"
)

// 线上真实报文:kungai → 下一跳(dubai3000) → paipu。
// 下一跳把 paipu 的报错包成一层信封,我们再包一层 fail_to_fetch_task。
// 这个测试锁住「最终透出去的是最内层那句话」。
func TestNormalizeUpstreamErrorBodyDoubleEnvelope(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantMsgPart string
		wantCode    string
	}{
		{
			name: "余额不足(内层有具体码)",
			body: `{"code":"fail_to_fetch_task","message":"{\"code\":\"insufficient_user_quota\",\"message\":\"余额不足：本次需要 ¥349.65，您当前余额 ¥137.73，还差 ¥211.92（本次未扣费）；请充值后再提交 (request id: 2026...)\",\"data\":null,\"request_id\":\"2026...\"}","data":null}`,
			// 内层有具体码 → 用它
			wantMsgPart: "余额不足：本次需要",
			wantCode:    "insufficient_user_quota",
		},
		{
			name:        "时长越界(内外都是信封码)",
			body:        `{"code":"fail_to_fetch_task","message":"{\"code\":\"upstream_task_submit_failed\",\"message\":\"任务提交失败（HTTP 400）：invalid_duration; ND duration must be between 4 and 15 seconds (request id: ...)\",\"data\":null}"}`,
			wantMsgPart: "invalid_duration; ND duration must be between 4 and 15 seconds",
			// 没有更具体的码 → 回落到默认信封码
			wantCode: "fail_to_fetch_task",
		},
		{
			name:        "参考图不可达",
			body:        `{"code":"fail_to_fetch_task","message":"{\"code\":\"upstream_task_submit_failed\",\"message\":\"任务提交失败（HTTP 400）：build_request_failed; invalid image reference: reference URL is not allowed\"}"}`,
			wantMsgPart: "invalid image reference",
			wantCode:    "fail_to_fetch_task",
		},
		{
			name:        "OpenAI/paipu 形态",
			body:        `{"error":{"message":"prompt 最长 4096 字节（UTF-8），当前 7673 字节","type":"invalid_request_error"}}`,
			wantMsgPart: "prompt 最长 4096 字节",
			wantCode:    "fail_to_fetch_task",
		},
		{
			name:        "纯文本网关页",
			body:        "502 Bad Gateway",
			wantMsgPart: "502 Bad Gateway",
			wantCode:    "fail_to_fetch_task",
		},
		{
			name:        "空 body",
			body:        "   ",
			wantMsgPart: "上游未返回错误内容",
			wantCode:    "fail_to_fetch_task",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			te := TaskErrorFromUpstreamBody(http.StatusBadRequest, []byte(c.body))
			if te == nil {
				t.Fatal("nil TaskError")
			}
			if !strings.Contains(te.Message, c.wantMsgPart) {
				t.Fatalf("message 应包含 %q,实际 %q", c.wantMsgPart, te.Message)
			}
			if strings.Contains(te.Message, `"code":`) {
				t.Fatalf("message 里不该残留 JSON 信封,实际 %q", te.Message)
			}
			if te.Code != c.wantCode {
				t.Fatalf("code 应为 %q,实际 %q", c.wantCode, te.Code)
			}
			if te.StatusCode != http.StatusBadRequest {
				t.Fatalf("status 应透传 400,实际 %d", te.StatusCode)
			}
		})
	}
}

// HTML 错误页(网关拦截 / Next.js 404)只给摘要,不把整页塞进错误条。
func TestNormalizeUpstreamErrorBodyHTML(t *testing.T) {
	body := `{"code":"fail_to_fetch_task","message":"<!DOCTYPE html><html lang=\"zh\"><head><title>404: This page could not be found.</title></head><body>x</body></html>"}`
	te := TaskErrorFromUpstreamBody(http.StatusNotFound, []byte(body))
	if !strings.Contains(te.Message, "HTML") {
		t.Fatalf("应提示是 HTML 页面,实际 %q", te.Message)
	}
	if !strings.Contains(te.Message, "404: This page could not be found.") {
		t.Fatalf("应带上 title,实际 %q", te.Message)
	}
	if strings.Contains(te.Message, "<html") {
		t.Fatalf("不该整页塞进来,实际 %q", te.Message)
	}
}

// unmarshal 失败时 new-api 会把原始 body 以 `body: {json}` 塞进 message。
func TestNormalizeUpstreamErrorBodyStripsBodyPrefix(t *testing.T) {
	body := `{"code":"unmarshal_response_body_failed","message":"body: {\"success\":true,\"task\":{\"id\":\"cmsr5puzm04e4lt2c2alou1tt\"}}"}`
	te := TaskErrorFromUpstreamBody(http.StatusInternalServerError, []byte(body))
	if !strings.Contains(te.Message, "cmsr5puzm04e4lt2c2alou1tt") {
		t.Fatalf("应保留原始 body 线索,实际 %q", te.Message)
	}
}

// 超长消息要截断,避免把客户端错误条撑爆。
func TestNormalizeUpstreamErrorBodyTruncates(t *testing.T) {
	long := strings.Repeat("错", upstreamMessageMaxRunes+50)
	te := TaskErrorFromUpstreamBody(http.StatusBadRequest, []byte(long))
	if !strings.HasSuffix(te.Message, "…") {
		t.Fatalf("超长消息应以省略号结尾,实际长度 %d", len([]rune(te.Message)))
	}
	if len([]rune(te.Message)) > upstreamMessageMaxRunes+1 {
		t.Fatalf("截断后仍过长: %d", len([]rune(te.Message)))
	}
}

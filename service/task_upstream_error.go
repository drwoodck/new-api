package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	taskdto "github.com/QuantumNous/new-api/dto"
)

// 上游(以及上游的下一跳)把失败原因包成什么形状,这里统一拆成一句人话。
//
// 背景(线上真实链路):客户端 → 我们(api.kungai.cc) → 下一跳中转 → paipu。
// 下一跳把 paipu 的报错包成
//
//	{"code":"upstream_task_submit_failed",
//	 "message":"任务提交失败（HTTP 400）：invalid_duration; ND duration must be between 4 and 15 seconds"}
//
// 我们再把这整段 body 原样当字符串塞进 `code=fail_to_fetch_task` 的 message 里,
// 用户最终看到的是
//
//	fail_to_fetch_task: {"code":"upstream_task_submit_failed","message":"..."}
//
// 读不出「到底哪一步错了」。所以在我们这层就把信封拆开:只把最内层的人话透出去,
// 能拿到具体错误码就用具体的(如 insufficient_user_quota),拿不到才回落到信封码。
//
// 这个函数只处理**失败响应体**,不碰成功响应,也不改变状态码语义。

// upstreamEnvelopeCodes 是「只说明哪个环节失败、不说明原因」的通用码。
// 这些码对内是日志锚点,对用户没有信息量,所以不作为最终 code 展示。
var upstreamEnvelopeCodes = map[string]bool{
	"fail_to_fetch_task":             true,
	"upstream_task_submit_failed":    true,
	"upstream_task_failed":           true,
	"unmarshal_response_body_failed": true,
	"unmarshal_response_failed":      true,
	"upstream_error":                 true,
	"relay_error":                    true,
	"task_failed":                    true,
}

// upstreamMessageKeys 按优先级排列:error 是 OpenAI/paipu 的标准形态,message 是
// new-api 系信封的形态,其余是各家变体。
var upstreamMessageKeys = []string{
	"error", "message", "msg", "detail", "reason", "fail_reason", "failReason", "upstream_error",
}

// 单条错误消息的长度上限。中转站偶尔会把整页 HTML / 整个上游 body 塞进来,
// 原样透传会把客户端错误条撑爆。
const upstreamMessageMaxRunes = 800

// TaskErrorFromUpstreamBody 把上游返回的非 2xx body 变成对外错误。
//
// 默认码 fail_to_fetch_task 与改动前的行为一致 —— 只有 body 里带着更具体的错误码
// (且不是通用信封码)时才替换它。
func TaskErrorFromUpstreamBody(statusCode int, body []byte) *taskdto.TaskError {
	msg, code := normalizeUpstreamErrorBody(string(body))
	if msg == "" {
		msg = "上游未返回错误内容"
	}
	if code == "" {
		code = "fail_to_fetch_task"
	}
	return TaskErrorWrapper(errors.New(msg), code, statusCode)
}

// normalizeUpstreamErrorBody 返回 (人话消息, 具体错误码)。两者都可能为空。
func normalizeUpstreamErrorBody(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	if looksLikeHTML(trimmed) {
		return htmlErrorGist(trimmed), ""
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		// 非 JSON(纯文本网关页等):保留原文,截断到可展示长度
		return truncateRunes(trimmed, upstreamMessageMaxRunes), ""
	}
	return unwrapUpstreamMessage(v, 0)
}

// unwrapUpstreamMessage 递归拆信封,直到拿到一句真正的人话。
//
// 深度上限防止畸形/环状嵌套把栈打爆;每层只认 upstreamMessageKeys 里列出的承载键,
// 避免把 data/status 之类的字段误当错误消息。
func unwrapUpstreamMessage(v any, depth int) (string, string) {
	if depth > 8 {
		return "", ""
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "", ""
		}
		if inner, ok := parseEmbeddedJSON(s); ok {
			if msg, code := unwrapUpstreamMessage(inner, depth+1); msg != "" {
				return msg, code
			}
		}
		if looksLikeHTML(s) {
			return htmlErrorGist(s), ""
		}
		return truncateRunes(s, upstreamMessageMaxRunes), ""
	case map[string]any:
		ownCode := codeOf(t)
		for _, key := range upstreamMessageKeys {
			child, ok := t[key]
			if !ok || child == nil {
				continue
			}
			if msg, code := unwrapUpstreamMessage(child, depth+1); msg != "" {
				if code == "" && ownCode != "" && !upstreamEnvelopeCodes[ownCode] {
					code = ownCode
				}
				return msg, code
			}
		}
		return "", ""
	default:
		return "", ""
	}
}

// codeOf 取本层的错误码(只认字符串,数字码对用户没意义)。
func codeOf(m map[string]any) string {
	if s, ok := m["code"].(string); ok {
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	return ""
}

// parseEmbeddedJSON 处理「message 本身是字符串化 JSON」的形态。
// 顺带剥掉 new-api 的 `body: {json}` 前缀(unmarshal_response_body_failed 用的就是它)。
func parseEmbeddedJSON(s string) (any, bool) {
	t := s
	if strings.HasPrefix(strings.ToLower(t), "body:") {
		t = strings.TrimSpace(t[len("body:"):])
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return nil, false
	}
	if t[0] != '{' && t[0] != '[' {
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return nil, false
	}
	return v, true
}

func looksLikeHTML(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) < 2 {
		return false
	}
	lower := strings.ToLower(t[:min(len(t), 15)])
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
}

// htmlErrorGist 把整页 HTML 压成一句能读的摘要(取 <title>,否则只报长度)。
func htmlErrorGist(s string) string {
	title := ""
	if i := strings.Index(strings.ToLower(s), "<title"); i >= 0 {
		rest := s[i:]
		if gt := strings.Index(rest, ">"); gt >= 0 {
			rest = rest[gt+1:]
			if end := strings.Index(strings.ToLower(rest), "</title"); end >= 0 {
				title = strings.TrimSpace(rest[:end])
			}
		}
	}
	if title != "" {
		return "上游返回了 HTML 页面「" + truncateRunes(title, 120) + "」(疑似网关/Cloudflare 拦截," +
			strconv.Itoa(len(s)) + " 字节)"
	}
	return "上游返回了 HTML 页面(疑似网关/Cloudflare 拦截," + strconv.Itoa(len(s)) + " 字节)"
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

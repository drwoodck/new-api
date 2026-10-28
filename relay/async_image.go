package relay

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// 异步生图入口判定。
//
// 判定放在 relay 包里，路由层与 controller 共用同一实现：body storage 是请求级
// 缓存的，重复解析不会二次读流，但两处各写一份判断迟早会不一致。
//
// 之所以要显式判定而不是看渠道：同一个 OpenAI 兼容渠道里可能既有异步模型也有
// 同步模型，请求是否要任务化只能由客户端表达（query 或 body 的 async）。

// IsAsyncImageRequest reports whether this image request asks for task mode.
// Both `?async=true` and `{"async": true}` are accepted — the canvas client uses
// the query form, plain API users tend to use the body form.
func IsAsyncImageRequest(c *gin.Context) bool {
	if isTruthyValue(c.Query("async")) {
		return true
	}
	if !strings.HasPrefix(c.ContentType(), "application/json") {
		return false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return false
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return false
	}
	var probe struct {
		Async *bool `json:"async"`
	}
	if err := common.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Async != nil && *probe.Async
}

// AsyncImageProviderForRequest returns the effective async image upstream for
// the request being served, or "" when it should use the synchronous image
// pipeline.
//
// Resolution order (first hit wins):
//
//  1. per-model override in `async_image_provider_models` (a `sync`/`none`
//     value opts a single model out of the task pipeline);
//  2. channel-level `async_image_provider`;
//  3. inferred from the channel base URL (toapis.cn/toapis.com → toapis,
//     rightapi.ai → rightapi);
//
// and, for any of the above, a Midjourney model on ToAPIs is rerouted to the
// `toapis_mj` protocol because that upstream serves it under /v1/midjourney/*.
//
// Step 3 exists so operating the channels needs no per-channel switch at all: a
// channel pointing at one of these hosts is an async image channel, full stop.
// The explicit fields stay as the escape hatch for mirrors/custom domains and
// for mixing a synchronous model into such a channel.
func AsyncImageProviderForRequest(c *gin.Context) string {
	model := strings.TrimSpace(c.GetString("original_model"))
	settings, hasSettings := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)

	if hasSettings && model != "" {
		if override, ok := settings.AsyncImageProviderModels[model]; ok {
			return finalizeAsyncProvider(explicitAsyncProvider(override), model)
		}
	}
	if hasSettings {
		if provider := explicitAsyncProvider(settings.AsyncImageProvider); provider != "" {
			return finalizeAsyncProvider(provider, model)
		}
	}
	return inferAsyncProviderFromBaseURL(common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl), model)
}

// explicitAsyncProvider normalises a configured provider value; empty and the
// opt-out words mean "use the synchronous pipeline".
func explicitAsyncProvider(configured string) string {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "", "none", "sync", "off", "false":
		return ""
	}
	return strings.TrimSpace(configured)
}

// inferAsyncProviderFromBaseURL recognises the known async image hosts.
func inferAsyncProviderFromBaseURL(baseURL, model string) string {
	host := hostnameOf(baseURL)
	switch {
	case hostMatchesDomain(host, "toapis.cn"), hostMatchesDomain(host, "toapis.com"):
		return finalizeAsyncProvider(constant.AsyncImageProviderToapis, model)
	case hostMatchesDomain(host, "rightapi.ai"):
		return constant.AsyncImageProviderRightapi
	}
	return ""
}

// finalizeAsyncProvider routes Midjourney models of the ToAPIs family to the
// dedicated MJ endpoints.
func finalizeAsyncProvider(provider, model string) string {
	if provider == constant.AsyncImageProviderToapis && isMidjourneyModel(model) {
		return constant.AsyncImageProviderToapisMJ
	}
	return provider
}

// isMidjourneyModel covers both the public name (`TO-midjourney`) and the
// upstream one (`midjourney`); model mapping happens after this resolution.
func isMidjourneyModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "midjourney")
}

// hostnameOf strips scheme, path and port so `https://api.toapis.cn/x` and
// `api.toapis.cn:443` both become `api.toapis.cn`.
func hostnameOf(rawURL string) string {
	host := strings.ToLower(strings.TrimSpace(rawURL))
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return host
}

// hostMatchesDomain reports whether host is domain itself or a subdomain of it.
func hostMatchesDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// ShouldUseAsyncImageTask reports whether this image request has to go through
// the task pipeline: the client asked for task mode AND the selected channel
// actually has an async upstream.
//
// Both halves are required. The canvas image contract now always sends
// `?async=true` (it is async_poll so that async upstreams can be polled, with
// sync responses short-circuiting on the first reply), so a synchronous channel
// must keep ignoring the flag instead of being pushed into the task pipeline.
func ShouldUseAsyncImageTask(c *gin.Context) bool {
	return IsAsyncImageRequest(c) && AsyncImageProviderForRequest(c) != ""
}

// AsyncImagePlatformForRequest resolves the task platform for an async image
// request. ok=false means "this channel has no async image upstream" — the
// caller must not fall back to the channel type, because for an OpenAI-compatible
// channel that would silently hand an image submission to the video adaptor.
func AsyncImagePlatformForRequest(c *gin.Context) (constant.TaskPlatform, bool) {
	return constant.AsyncImageTaskPlatform(AsyncImageProviderForRequest(c))
}

func isTruthyValue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

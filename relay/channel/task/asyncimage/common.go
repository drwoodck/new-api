// Package asyncimage implements the async (task based) image generation
// adaptors used by channels whose upstream has no synchronous image endpoint.
//
// Two upstream families are supported, both configured through
// channels.settings.async_image_provider:
//
//	toapis    — POST /v1/images/generations, GET /v1/images/generations/{id}
//	toapis_mj — POST /v1/midjourney/generations, GET /v1/midjourney/{id}
//	rightapi  — POST /v1/images/generations (forced "async": true),
//	            GET /v1/tasks/{id}
//
// The client facing protocol is the one the canvas client already ships as
// `img_async_newapi`: submit answers {task_id,status,...}, polling answers
// {status,progress,data[].url}.
package asyncimage

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// taskPayload covers every response shape of the supported upstreams. Keeping
// one struct (instead of one per provider) means a provider that starts
// returning an extra field degrades instead of failing to parse.
type taskPayload struct {
	ID       string `json:"id"`
	TaskID   string `json:"task_id"`
	Object   string `json:"object"`
	Model    string `json:"model"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`

	Result *struct {
		Type string `json:"type"`
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	} `json:"result"`

	// Top level data: rightapi's finished payload, and toapis' OpenAI-ish alias.
	Data []struct {
		URL string `json:"url"`
	} `json:"data"`

	// rightapi's Gemini protocol result puts the image URL inside text parts.
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`

	// toapis Midjourney result fields.
	GridImageURL string   `json:"grid_image_url"`
	ImageURLs    []string `json:"image_urls"`
	Description  string   `json:"description"`

	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`

	// Usage is only present on token-billed models (ToAPIs reports
	// charge_type "per_token" for gpt-image-2-official / 2.5 VIP / 2.5
	// official). Per-request models report only {charge_type, requests}.
	Usage *struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails *struct {
			TextTokens          int `json:"text_tokens"`
			ImageTokens         int `json:"image_tokens"`
			CachedTokens        int `json:"cached_tokens"`
			CachedTokensDetails *struct {
				TextTokens  int `json:"text_tokens"`
				ImageTokens int `json:"image_tokens"`
			} `json:"cached_tokens_details"`
		} `json:"input_tokens_details"`
		OutputTokensDetails *struct {
			TextTokens  int `json:"text_tokens"`
			ImageTokens int `json:"image_tokens"`
		} `json:"output_tokens_details"`
		ChargeType string `json:"charge_type"`
	} `json:"usage"`
}

// upstreamTaskID returns whichever id field the provider used.
func (p *taskPayload) upstreamTaskID() string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.ID) != "" {
		return strings.TrimSpace(p.ID)
	}
	return strings.TrimSpace(p.TaskID)
}

// resultURLs collects every image URL the payload carries, in a stable order.
func (p *taskPayload) resultURLs() []string {
	if p == nil {
		return nil
	}
	urls := make([]string, 0, 4)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range urls {
			if existing == raw {
				return
			}
		}
		urls = append(urls, raw)
	}

	if p.Result != nil {
		for _, item := range p.Result.Data {
			add(item.URL)
		}
	}
	if p.GridImageURL != "" {
		add(p.GridImageURL)
	}
	for _, item := range p.Data {
		add(item.URL)
	}
	for _, item := range p.ImageURLs {
		add(item)
	}
	for _, candidate := range p.Candidates {
		for _, part := range candidate.Content.Parts {
			add(part.Text)
		}
	}
	return urls
}

func (p *taskPayload) errorMessage() string {
	if p == nil || p.Error == nil {
		return ""
	}
	if msg := strings.TrimSpace(p.Error.Message); msg != "" {
		return msg
	}
	return strings.TrimSpace(p.Error.Code)
}

// ResultURLsFromRaw extracts every image URL a stored upstream payload carries.
// Used by the fetch endpoint, which only has the persisted task row.
func ResultURLsFromRaw(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload taskPayload
	if err := common.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload.resultURLs()
}

// normalizeTaskStatus maps an upstream status word onto the task lifecycle.
//
// Providers spell the same states differently (queued/pending, processing/
// in_progress, completed/succeeded/SUCCESS, …), so the mapping is deliberately
// permissive; an unknown word must never be read as success.
func normalizeTaskStatus(raw string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "not_start", "notstart", "created", "pending", "queued", "submitted":
		return model.TaskStatusQueued
	case "in_progress", "inprogress", "processing", "running", "modal":
		return model.TaskStatusInProgress
	case "completed", "complete", "succeeded", "success", "finished":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "canceled", "cancelled", "expired", "rejected":
		return model.TaskStatusFailure
	}
	return model.TaskStatusInProgress
}

// publicSubmitResponse is what the client receives after a successful submit.
// `task_id` is the field the canvas async image profile reads; `id` is kept for
// OpenAI-shaped clients that expect the video/task convention.
type publicSubmitResponse struct {
	TaskID    string `json:"task_id"`
	ID        string `json:"id"`
	Object    string `json:"object"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Progress  int    `json:"progress"`
	CreatedAt int64  `json:"created_at"`
}

func writePublicSubmitResponse(c *gin.Context, info *relaycommon.RelayInfo, taskID string) {
	if c == nil || taskID == "" {
		return
	}
	c.JSON(http.StatusOK, publicSubmitResponse{
		TaskID:    taskID,
		ID:        taskID,
		Object:    "generation.task",
		Model:     info.OriginModelName,
		Status:    "queued",
		Progress:  0,
		CreatedAt: common.GetTimestamp(),
	})
}

// readJSONBody loads the request body as a generic map so unknown provider
// parameters (quality, background, output_format, …) survive the hop.
func readJSONBody(c *gin.Context) (map[string]interface{}, []byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, fmt.Errorf("read request body failed: %w", err)
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("read request body bytes failed: %w", err)
	}
	body := map[string]interface{}{}
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &body); err != nil {
			return nil, raw, fmt.Errorf("request body is not valid JSON: %w", err)
		}
	}
	return body, raw, nil
}

// parseTaskSubmitReq decodes the shared task request view (prompt / images /
// size / resolution / aspect_ratio) and stores it for tier-price resolution.
//
// Tier billing (BuildTaskTierInput) reads it back from the context, so every
// async image adaptor must call this before the price is computed.
func parseTaskSubmitReq(c *gin.Context) (relaycommon.TaskSubmitReq, *dto.TaskError) {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return req, service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return req, service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if taskErr := relaycommon.ValidateInputMaterialCount(req); taskErr != nil {
		return req, taskErr
	}
	c.Set("task_request", req)
	return req, nil
}

// rejectMultipleImages guards the billing model: async image channels are
// priced per request, so n > 1 would either under-charge (one request price for
// several images) or need a multiplier the tier-request path deliberately
// skips. Fail loudly instead of silently producing a wrong charge.
func rejectMultipleImages(body map[string]interface{}) *dto.TaskError {
	raw, ok := body["n"]
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(fmt.Sprintf("%v", raw))
	if err != nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("invalid n"), "invalid_request", http.StatusBadRequest)
	}
	if n > 1 {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("async image channels only support n=1 (got %d); submit one task per image", n),
			"invalid_request", http.StatusBadRequest)
	}
	return nil
}

// referenceImages extracts reference image URLs from the several spellings the
// providers and the canvas use.
func referenceImages(body map[string]interface{}) []string {
	urls := make([]string, 0, 4)
	appendValue := func(value interface{}) {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				urls = append(urls, strings.TrimSpace(typed))
			}
		case []interface{}:
			for _, item := range typed {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					urls = append(urls, strings.TrimSpace(s))
				}
			}
		case []string:
			for _, s := range typed {
				if strings.TrimSpace(s) != "" {
					urls = append(urls, strings.TrimSpace(s))
				}
			}
		}
	}
	for _, key := range []string{"images", "image", "image_urls", "reference_images", "input_reference"} {
		if value, ok := body[key]; ok {
			appendValue(value)
			delete(body, key)
		}
	}
	return urls
}

// imageListToJSON converts a URL slice to the generic JSON array form used when
// rewriting the request body.
func imageListToJSON(urls []string) []interface{} {
	out := make([]interface{}, 0, len(urls))
	for _, url := range urls {
		out = append(out, url)
	}
	return out
}

// newFetchRequest performs the polling GET against the upstream.
func newFetchRequest(uri, key, proxy string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// buildTaskInfo converts a polled upstream payload into the task lifecycle
// view. finished reports whether the payload reached a terminal state.
func buildTaskInfo(payload *taskPayload, finishedByResult bool, requiredStatus bool) (*relaycommon.TaskInfo, error) {
	result := &relaycommon.TaskInfo{}

	status := normalizeTaskStatus(payload.Status)
	if status == "" {
		// rightapi omits `status` once the task is done: the payload then only
		// carries the result. A response without status and without result is
		// unrecognised, not success.
		if finishedByResult && len(payload.resultURLs()) > 0 {
			status = model.TaskStatusSuccess
		} else if requiredStatus {
			return nil, fmt.Errorf("upstream returned no status")
		} else {
			status = model.TaskStatusInProgress
		}
	}
	if status == model.TaskStatusSuccess {
		urls := payload.resultURLs()
		if len(urls) == 0 {
			return relaycommon.FailTaskInfo("upstream reported success without any image url"), nil
		}
		result.Url = urls[0]
	}
	if status == model.TaskStatusFailure {
		reason := payload.errorMessage()
		if reason == "" {
			reason = "upstream reported failure: " + strings.TrimSpace(payload.Status)
		}
		return relaycommon.FailTaskInfo(reason), nil
	}

	result.TaskID = payload.upstreamTaskID()
	result.Status = string(status)
	if payload.Progress > 0 {
		result.Progress = fmt.Sprintf("%d%%", payload.Progress)
	}
	// 按 token 计费的上游会在终态带上用量明细；按次计费的只有 requests，
	// 这些字段保持 0，结算继续走原有的档位/模板倍率路径。
	if u := payload.Usage; u != nil && u.TotalTokens > 0 {
		result.PromptTokens = u.InputTokens
		result.TotalTokens = u.TotalTokens
		result.CompletionTokens = u.OutputTokens
		if d := u.InputTokensDetails; d != nil {
			result.TextInputTokens = d.TextTokens
			result.ImageInputTokens = d.ImageTokens
			if c := d.CachedTokensDetails; c != nil {
				result.CachedTextInputTokens = c.TextTokens
				result.CachedImageInputTokens = c.ImageTokens
			} else {
				result.CachedTextInputTokens = d.CachedTokens
			}
		}
		if d := u.OutputTokensDetails; d != nil {
			result.TextOutputTokens = d.TextTokens
			result.ImageOutputTokens = d.ImageTokens
		}
	}
	return result, nil
}

// adaptorBase holds the per-request upstream coordinates shared by all
// providers.
type adaptorBase struct {
	apiKey  string
	baseURL string
}

func (a *adaptorBase) initBase(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = strings.TrimSuffix(info.ChannelBaseUrl, "/")
}

func (a *adaptorBase) fetchTask(uri, key, proxy string) (*http.Response, error) {
	return newFetchRequest(uri, key, proxy)
}

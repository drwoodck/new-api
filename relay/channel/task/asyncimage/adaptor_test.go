package asyncimage

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJSONContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestToapisParseTaskResult(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus model.TaskStatus
		wantURL    string
		wantReason string
		wantErr    bool
	}{
		{
			name:       "queued",
			body:       `{"id":"img_1","object":"generation.task","status":"queued","progress":0}`,
			wantStatus: model.TaskStatusQueued,
		},
		{
			name:       "in progress keeps progress",
			body:       `{"id":"img_1","status":"in_progress","progress":50}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "completed exposes result url",
			body:       `{"id":"img_1","status":"completed","progress":100,"result":{"type":"image","data":[{"url":"https://cdn.example.com/a.png"}]}}`,
			wantStatus: model.TaskStatusSuccess,
			wantURL:    "https://cdn.example.com/a.png",
		},
		{
			name:       "failed keeps upstream message",
			body:       `{"id":"img_1","status":"failed","error":{"code":"generation_failed","message":"call upstream API failed"}}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: "call upstream API failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := (&ToapisAdaptor{}).ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, string(tc.wantStatus), info.Status)
			assert.Equal(t, tc.wantURL, info.Url)
			if tc.wantReason != "" {
				assert.Contains(t, info.Reason, tc.wantReason)
			}
		})
	}
}

// 按 token 计费的上游在终态带回 usage，任务结算要按这些字段重算额度，
// 所以字段映射必须锁死：ToAPIs 的 input_tokens 是**输入总量**，缓存是它的
// 子集（cached_tokens_details 再拆文本/图片）。
func TestToapisParseTaskResultCarriesTokenUsage(t *testing.T) {
	body := `{
		"id":"tsk_1","status":"completed","progress":100,
		"result":{"type":"image","data":[{"url":"https://cdn.example.com/a.png"}]},
		"usage":{
			"charge_type":"per_token",
			"input_tokens":21,"output_tokens":196,"total_tokens":217,
			"input_tokens_details":{
				"text_tokens":9,"image_tokens":12,"cached_tokens":5,
				"cached_tokens_details":{"text_tokens":3,"image_tokens":2}
			},
			"output_tokens_details":{"text_tokens":0,"image_tokens":196}
		}
	}`

	info, err := (&ToapisAdaptor{}).ParseTaskResult([]byte(body))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 21, info.PromptTokens)
	assert.Equal(t, 217, info.TotalTokens)
	assert.Equal(t, 9, info.TextInputTokens)
	assert.Equal(t, 12, info.ImageInputTokens)
	assert.Equal(t, 3, info.CachedTextInputTokens)
	assert.Equal(t, 2, info.CachedImageInputTokens)
	assert.Equal(t, 196, info.ImageOutputTokens)

	usage := info.Usage()
	require.NotNil(t, usage)
	// OpenAI 口径：prompt 是总量，缓存是子集
	assert.Equal(t, 21, usage.PromptTokens)
	assert.Equal(t, 196, usage.CompletionTokens)
	assert.Equal(t, 217, usage.TotalTokens)
	assert.Equal(t, 5, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 2, usage.PromptTokensDetails.CachedImageTokens)
	assert.Equal(t, 196, usage.CompletionTokenDetails.ImageTokens)
}

// 按次计费的上游只回 {charge_type, requests}，不能造出 token 用量 ——
// 否则会从档位/按次结算误切到 token 结算。
func TestToapisParseTaskResultWithoutTokenUsage(t *testing.T) {
	body := `{"id":"tsk_1","status":"completed","progress":100,
		"result":{"type":"image","data":[{"url":"https://cdn.example.com/a.png"}]},
		"usage":{"charge_type":"per_request","requests":1}}`

	info, err := (&ToapisAdaptor{}).ParseTaskResult([]byte(body))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Zero(t, info.TotalTokens)
	assert.Nil(t, info.Usage())
}

func TestToapisParseCompletedWithoutURLFails(t *testing.T) {
	info, err := (&ToapisAdaptor{}).ParseTaskResult([]byte(`{"id":"img_1","status":"completed"}`))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, string(model.TaskStatusFailure), info.Status,
		"success without any url must not be reported as a usable result")
	assert.Contains(t, info.Reason, "without any image url")
}

func TestRightapiParseTaskResult(t *testing.T) {
	// rightapi drops the status field once finished — presence of data[].url is
	// the completion signal.
	t.Run("finished payload has no status", func(t *testing.T) {
		info, err := (&RightapiAdaptor{}).ParseTaskResult([]byte(`{"created":1,"data":[{"url":"https://cdn.example.com/r.png"}]}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
		assert.Equal(t, "https://cdn.example.com/r.png", info.Url)
	})

	t.Run("in progress", func(t *testing.T) {
		info, err := (&RightapiAdaptor{}).ParseTaskResult([]byte(`{"task_id":"task_1","status":"in_progress","progress":45}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusInProgress), info.Status)
		assert.Equal(t, "45%", info.Progress)
	})

	t.Run("failed", func(t *testing.T) {
		info, err := (&RightapiAdaptor{}).ParseTaskResult([]byte(`{"task_id":"task_1","status":"failed","error":{"message":"上游生成失败"}}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusFailure), info.Status)
		assert.Contains(t, info.Reason, "上游生成失败")
	})

	t.Run("gemini candidates carry the url in text parts", func(t *testing.T) {
		body := `{"candidates":[{"content":{"parts":[{"text":"https://cdn.example.com/g.png"}]}}]}`
		info, err := (&RightapiAdaptor{}).ParseTaskResult([]byte(body))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
		assert.Equal(t, "https://cdn.example.com/g.png", info.Url)
	})

	t.Run("no status and no result is not success", func(t *testing.T) {
		info, err := (&RightapiAdaptor{}).ParseTaskResult([]byte(`{"object":"image"}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusInProgress), info.Status)
	})
}

func TestToapisMJParseTaskResult(t *testing.T) {
	t.Run("success uses image_urls", func(t *testing.T) {
		body := `{"id":"tsk_1","status":"SUCCESS","image_urls":["https://cdn.example.com/mj1.png","https://cdn.example.com/mj2.png"]}`
		info, err := (&ToapisMJAdaptor{}).ParseTaskResult([]byte(body))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusSuccess), info.Status)
		assert.Equal(t, "https://cdn.example.com/mj1.png", info.Url)
	})

	t.Run("modal is a waiting state, not a failure", func(t *testing.T) {
		info, err := (&ToapisMJAdaptor{}).ParseTaskResult([]byte(`{"id":"tsk_1","status":"MODAL"}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusInProgress), info.Status)
	})

	t.Run("failure maps to FAILURE", func(t *testing.T) {
		info, err := (&ToapisMJAdaptor{}).ParseTaskResult([]byte(`{"id":"tsk_1","status":"FAILURE"}`))
		require.NoError(t, err)
		assert.Equal(t, string(model.TaskStatusFailure), info.Status)
	})
}

func TestResultURLsFromRawCollectsEveryImage(t *testing.T) {
	body := []byte(`{"id":"1","status":"completed","result":{"data":[{"url":"https://a/1.png"},{"url":"https://a/2.png"}]},"image_urls":["https://a/3.png"]}`)
	assert.Equal(t, []string{"https://a/1.png", "https://a/2.png", "https://a/3.png"}, ResultURLsFromRaw(body))
}

func TestRejectMultipleImages(t *testing.T) {
	assert.Nil(t, rejectMultipleImages(map[string]interface{}{}))
	assert.Nil(t, rejectMultipleImages(map[string]interface{}{"n": float64(1)}))
	assert.Nil(t, rejectMultipleImages(map[string]interface{}{"n": "1"}))
	err := rejectMultipleImages(map[string]interface{}{"n": float64(2)})
	require.NotNil(t, err)
	assert.Equal(t, http.StatusBadRequest, err.StatusCode)
	assert.Contains(t, err.Message, "n=1")
}

func TestToapisBuildRequestBodyNormalizesReferences(t *testing.T) {
	body := `{"model":"TO-gpt-image-2","prompt":"cat","async":true,"images":["https://a/1.png"],"size":"1:1"}`
	c, _ := newJSONContext(t, body)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-image-2"}}

	reader, err := (&ToapisAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), `"async"`, "toapis is async by nature; the flag must not leak upstream")
	assert.NotContains(t, string(raw), `"images"`, "reference images must be renamed to reference_images")
	assert.Contains(t, string(raw), `"reference_images":["https://a/1.png"]`)
	assert.Contains(t, string(raw), `"model":"gpt-image-2"`)
	assert.True(t, bytes.Contains(raw, []byte(`"size":"1:1"`)), "unknown provider params must survive")
}

func TestRightapiBuildRequestBodyForcesAsync(t *testing.T) {
	body := `{"model":"TO-gpt-image-2","prompt":"cat","image":"https://a/1.png"}`
	c, _ := newJSONContext(t, body)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-image-2"}}

	reader, err := (&RightapiAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Contains(t, string(raw), `"async":true`, "rightapi only returns a task id when async is set")
	assert.Contains(t, string(raw), `"image":"https://a/1.png"`, "reference fields must not be rewritten")
	assert.Contains(t, string(raw), `"model":"gpt-image-2"`)
}

func TestValidateRejectsMissingPrompt(t *testing.T) {
	c, _ := newJSONContext(t, `{"model":"TO-gpt-image-2","prompt":""}`)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	taskErr := (&ToapisAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "prompt")
}

func TestValidateRejectsMultipleImages(t *testing.T) {
	c, _ := newJSONContext(t, `{"model":"TO-gpt-image-2","prompt":"cat","n":2}`)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	taskErr := (&ToapisAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "n=1")
}

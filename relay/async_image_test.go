package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newImageRequestContext(t *testing.T, query, contentType, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	target := "/v1/images/generations"
	if query != "" {
		target += "?" + query
	}
	c.Request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

func TestIsAsyncImageRequest(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		contentType string
		body        string
		want        bool
	}{
		{name: "query async=true", query: "async=true", contentType: "application/json", body: `{"prompt":"cat"}`, want: true},
		{name: "query async=1", query: "async=1", contentType: "application/json", body: `{"prompt":"cat"}`, want: true},
		{name: "body async true", contentType: "application/json", body: `{"prompt":"cat","async":true}`, want: true},
		{name: "body async false", contentType: "application/json", body: `{"prompt":"cat","async":false}`, want: false},
		{name: "plain sync request", contentType: "application/json", body: `{"prompt":"cat"}`, want: false},
		{name: "multipart is never async", contentType: "multipart/form-data; boundary=x", body: "", want: false},
		{name: "query async=0", query: "async=0", contentType: "application/json", body: `{"prompt":"cat"}`, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newImageRequestContext(t, tc.query, tc.contentType, tc.body)
			assert.Equal(t, tc.want, IsAsyncImageRequest(c))
		})
	}
}

func TestIsAsyncImageRequestLeavesBodyReadable(t *testing.T) {
	c := newImageRequestContext(t, "", "application/json", `{"prompt":"cat","async":true}`)
	require.True(t, IsAsyncImageRequest(c))

	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	raw, err := storage.Bytes()
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"prompt":"cat"`,
		"probing the async flag must not consume the request body")
}

func TestShouldUseAsyncImageTask(t *testing.T) {
	t.Run("async channel plus async request goes to the task pipeline", func(t *testing.T) {
		c := newImageRequestContext(t, "async=true", "application/json", `{"prompt":"cat"}`)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://toapis.cn")
		assert.True(t, ShouldUseAsyncImageTask(c))
	})

	t.Run("sync channel ignores the async flag", func(t *testing.T) {
		// The canvas image contract always sends ?async=true after it became
		// async_poll; a synchronous channel must stay on the sync path.
		c := newImageRequestContext(t, "async=true", "application/json", `{"prompt":"cat"}`)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://api.paipu.net")
		assert.False(t, ShouldUseAsyncImageTask(c))
	})

	t.Run("async channel without the flag is a sync call (and gets rejected later)", func(t *testing.T) {
		c := newImageRequestContext(t, "", "application/json", `{"prompt":"cat"}`)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://rightapi.ai")
		assert.False(t, ShouldUseAsyncImageTask(c))
	})
}

func TestAsyncImagePlatformForRequest(t *testing.T) {
	c := newImageRequestContext(t, "", "application/json", `{}`)

	_, ok := AsyncImagePlatformForRequest(c)
	assert.False(t, ok, "a channel without a recognisable upstream must not resolve to a platform")

	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://toapis.cn")
	platform, ok := AsyncImagePlatformForRequest(c)
	require.True(t, ok)
	assert.Equal(t, constant.TaskPlatformToapisImage, platform)

	c.Set("original_model", "TO-midjourney")
	platform, ok = AsyncImagePlatformForRequest(c)
	require.True(t, ok)
	assert.Equal(t, constant.TaskPlatformToapisMJImage, platform)
}

func TestAsyncImageProviderForRequest(t *testing.T) {
	cases := []struct {
		name     string
		baseURL  string
		model    string
		settings *dto.ChannelOtherSettings
		want     string
	}{
		{
			name: "unknown plain channel stays synchronous",
			want: "",
		},
		{
			name:    "toapis host is recognised without any setting",
			baseURL: "https://toapis.cn",
			want:    "toapis",
		},
		{
			name:    "toapis subdomain with path and port",
			baseURL: "https://api.toapis.com:443/v1",
			want:    "toapis",
		},
		{
			name:    "rightapi host is recognised without any setting",
			baseURL: "https://rightapi.ai",
			want:    "rightapi",
		},
		{
			name:    "midjourney model on toapis uses the mj protocol",
			baseURL: "https://toapis.cn",
			model:   "TO-midjourney",
			want:    "toapis_mj",
		},
		{
			name:    "ordinary image model on toapis stays on the images api",
			baseURL: "https://toapis.cn",
			model:   "TO-gpt-image-2",
			want:    "toapis",
		},
		{
			name:     "explicit channel setting wins over the host",
			baseURL:  "https://mirror.example.com",
			settings: &dto.ChannelOtherSettings{AsyncImageProvider: "  toapis  "},
			want:     "toapis",
		},
		{
			name:     "per-model override wins over the channel default",
			baseURL:  "https://toapis.cn",
			model:    "TO-midjourney",
			settings: &dto.ChannelOtherSettings{AsyncImageProvider: "toapis", AsyncImageProviderModels: map[string]string{"TO-midjourney": "toapis_mj"}},
			want:     "toapis_mj",
		},
		{
			name:     "a model marked sync opts out of the inferred channel",
			baseURL:  "https://toapis.cn",
			model:    "TO-legacy-sync-model",
			settings: &dto.ChannelOtherSettings{AsyncImageProviderModels: map[string]string{"TO-legacy-sync-model": "sync"}},
			want:     "",
		},
		{
			name:    "lookalike domain is not mistaken for toapis",
			baseURL: "https://eviltoapis.com",
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newImageRequestContext(t, "", "application/json", `{}`)
			if tc.baseURL != "" {
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, tc.baseURL)
			}
			if tc.model != "" {
				c.Set("original_model", tc.model)
			}
			if tc.settings != nil {
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, *tc.settings)
			}
			assert.Equal(t, tc.want, AsyncImageProviderForRequest(c))
		})
	}
}

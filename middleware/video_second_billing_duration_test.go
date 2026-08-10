package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These tests cover per-second video billing behind a third-party relay
// ("中转站"), where the inbound payload may be OpenAI-compatible or a vendor
// native format. The billable duration must survive the entry-point
// conversion, otherwise every request would silently fall back to the channel
// default and be mis-charged.

// runConvertThenResolve pushes a body through an entry-point middleware (if
// any), then through the standard task validation, and returns the duration
// that per-second billing would charge for.
func runConvertThenResolve(
	t *testing.T,
	convert gin.HandlerFunc,
	path string,
	body string,
	channelType int,
) int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context.Request = request

	if convert != nil {
		convert(context)
	}

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelType: channelType},
	}
	taskErr := relaycommon.ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
	require.Nil(t, taskErr, "request should validate")

	return relaycommon.ResolveTaskVideoDuration(context, channelType)
}

// TestPerSecondDurationOpenAICompatibleRelay covers a relay exposing the
// OpenAI video API, configured as an OpenAI/Sora channel.
func TestPerSecondDurationOpenAICompatibleRelay(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "seconds as string (OpenAI videos API)",
			body: `{"model":"sora-2","prompt":"a cat","seconds":"12"}`,
			want: 12,
		},
		{
			name: "duration as number",
			body: `{"model":"sora-2","prompt":"a cat","duration":9}`,
			want: 9,
		},
		{
			name: "duration as string",
			body: `{"model":"sora-2","prompt":"a cat","duration":"7"}`,
			want: 7,
		},
		{
			name: "omitted falls back to sora default (OpenAI channel uses sora adaptor)",
			body: `{"model":"sora-2","prompt":"a cat"}`,
			want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runConvertThenResolve(t, nil, "/v1/video/generations", tt.body,
				constant.ChannelTypeOpenAI)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestPerSecondDurationKlingNativeRelay covers a relay speaking Kling's native
// format. KlingRequestConvert nests the whole original body under metadata, so
// the duration is only reachable via the metadata path.
func TestPerSecondDurationKlingNativeRelay(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "native duration as string",
			body: `{"model_name":"kling-v1-pro","prompt":"a cat","duration":"10"}`,
			want: 10,
		},
		{
			name: "native duration as number",
			body: `{"model_name":"kling-v1-pro","prompt":"a cat","duration":10}`,
			want: 10,
		},
		{
			name: "omitted falls back to kling default",
			body: `{"model_name":"kling-v1-pro","prompt":"a cat"}`,
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runConvertThenResolve(t, KlingRequestConvert(),
				"/kling/v1/videos/text2video", tt.body, constant.ChannelTypeKling)
			require.Equal(t, tt.want, got,
				"kling native duration must survive the metadata nesting")
		})
	}
}

// TestPerSecondDurationJimengNativeRelay covers the Jimeng native entry point,
// which uses the same metadata-nesting conversion.
func TestPerSecondDurationJimengNativeRelay(t *testing.T) {
	got := runConvertThenResolve(t, JimengRequestConvert(), "/jimeng/",
		`{"model":"jimeng-video-3.0","prompt":"a cat","duration":8}`,
		constant.ChannelTypeJimeng)
	require.Equal(t, 8, got)
}

// TestPerSecondDurationDoubaoNativeRelay covers Doubao/VolcEngine, whose
// EstimateBilling returns only a resolution ratio, so the duration must come
// from the generic resolver.
func TestPerSecondDurationDoubaoNativeRelay(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		channelType int
		want        int
	}{
		{
			name:        "duration in metadata",
			body:        `{"model":"doubao-seedance","prompt":"a cat","metadata":{"duration":10}}`,
			channelType: constant.ChannelTypeDoubaoVideo,
			want:        10,
		},
		{
			name:        "top-level duration on volcengine channel",
			body:        `{"model":"doubao-seedance","prompt":"a cat","duration":5}`,
			channelType: constant.ChannelTypeVolcEngine,
			want:        5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runConvertThenResolve(t, nil, "/v1/video/generations", tt.body, tt.channelType)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestPerSecondDurationHailuoRelay covers MiniMax/Hailuo, which also has no
// seconds ratio of its own.
func TestPerSecondDurationHailuoRelay(t *testing.T) {
	got := runConvertThenResolve(t, nil, "/v1/video/generations",
		`{"model":"MiniMax-Hailuo-02","prompt":"a cat","duration":6}`,
		constant.ChannelTypeMiniMax)
	require.Equal(t, 6, got)
}

// TestPerSecondDurationRejectsOversizedThroughNativeRelay guards that a native
// relay cannot smuggle an unbounded billing multiplier through the request.
// The oversized duration is caught by request validation (not clamped later).
func TestPerSecondDurationRejectsOversizedThroughNativeRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := `{"model_name":"kling-v1-pro","prompt":"a cat","duration":"999999999"}`
	request := httptest.NewRequest(http.MethodPost, "/kling/v1/videos/text2video", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context.Request = request

	convert := KlingRequestConvert()
	convert(context)

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeKling},
	}
	taskErr := relaycommon.ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
	require.NotNil(t, taskErr, "oversized duration must be rejected at validation")
	require.Equal(t, "invalid_seconds", taskErr.Code)
}

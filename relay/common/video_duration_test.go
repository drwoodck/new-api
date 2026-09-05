package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newDurationContext builds a gin context carrying a parsed task request,
// mirroring what the adaptors' ValidateRequestAndSetAction stores.
func newDurationContext(req *TaskSubmitReq) *gin.Context {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	if req != nil {
		context.Set("task_request", *req)
	}
	return context
}

// TestResolveTaskVideoDurationPriority pins the resolution order used as the
// per-second billing multiplier: duration > seconds > metadata > channel default.
func TestResolveTaskVideoDurationPriority(t *testing.T) {
	tests := []struct {
		name        string
		req         *TaskSubmitReq
		channelType int
		want        int
	}{
		{
			name: "duration field wins over seconds and metadata",
			req: &TaskSubmitReq{
				Duration: 7,
				Seconds:  "3",
				Metadata: map[string]interface{}{"durationSeconds": float64(12)},
			},
			channelType: constant.ChannelTypeSora,
			want:        7,
		},
		{
			name:        "seconds string is used when duration is absent",
			req:         &TaskSubmitReq{Seconds: "9"},
			channelType: constant.ChannelTypeSora,
			want:        9,
		},
		{
			name:        "metadata durationSeconds is used as float",
			req:         &TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": float64(6)}},
			channelType: constant.ChannelTypeKling,
			want:        6,
		},
		{
			name:        "metadata duration string is parsed",
			req:         &TaskSubmitReq{Metadata: map[string]interface{}{"duration": "11"}},
			channelType: constant.ChannelTypeKling,
			want:        11,
		},
		{
			name:        "falls back to sora default",
			req:         &TaskSubmitReq{},
			channelType: constant.ChannelTypeSora,
			want:        4,
		},
		{
			name:        "falls back to veo default",
			req:         &TaskSubmitReq{},
			channelType: constant.ChannelTypeGemini,
			want:        8,
		},
		{
			name:        "falls back to hailuo default",
			req:         &TaskSubmitReq{},
			channelType: constant.ChannelTypeMiniMax,
			want:        6,
		},
		{
			name:        "unknown channel uses generic default",
			req:         &TaskSubmitReq{},
			channelType: 99999,
			want:        5,
		},
		{
			name:        "missing task request falls back to channel default",
			req:         nil,
			channelType: constant.ChannelTypeKling,
			want:        5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTaskVideoDuration(newDurationContext(tt.req), tt.channelType)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestResolveTaskVideoDurationClamped guards the billing invariant: duration is
// a quota multiplier, so an oversized value must never pass through unbounded.
// Metadata bypasses standard request validation, hence it is covered too.
func TestResolveTaskVideoDurationClamped(t *testing.T) {
	tests := []struct {
		name string
		req  *TaskSubmitReq
	}{
		{name: "duration field", req: &TaskSubmitReq{Duration: 9999999}},
		{name: "seconds string", req: &TaskSubmitReq{Seconds: "9999999"}},
		{
			name: "metadata override",
			req:  &TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": float64(9999999)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTaskVideoDuration(newDurationContext(tt.req), constant.ChannelTypeSora)
			require.Equal(t, MaxTaskDurationSeconds, got)
		})
	}
}

// TestResolveTaskVideoDurationIgnoresNonPositive ensures zero/negative values
// fall through to the channel default rather than producing a free request.
func TestResolveTaskVideoDurationIgnoresNonPositive(t *testing.T) {
	tests := []struct {
		name string
		req  *TaskSubmitReq
	}{
		{name: "zero duration", req: &TaskSubmitReq{Duration: 0}},
		{name: "negative duration", req: &TaskSubmitReq{Duration: -5}},
		{name: "zero seconds string", req: &TaskSubmitReq{Seconds: "0"}},
		{name: "unparsable seconds string", req: &TaskSubmitReq{Seconds: "abc"}},
		{
			name: "zero metadata duration",
			req:  &TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": float64(0)}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTaskVideoDuration(newDurationContext(tt.req), constant.ChannelTypeSora)
			require.Equal(t, 4, got, "should fall back to the sora default")
		})
	}
}

// TestResolveTaskExplicitDuration pins the explicit-hint-only entry point used
// by input material billing: it must return the same request hints as
// ResolveTaskVideoDuration (duration > seconds > metadata, clamped) but 0 —
// never a channel default — when the request says nothing, so callers can
// distinguish "explicit" from "guessed" durations.
func TestResolveTaskExplicitDuration(t *testing.T) {
	tests := []struct {
		name string
		req  *TaskSubmitReq
		want int
	}{
		{name: "duration field", req: &TaskSubmitReq{Duration: 7, Seconds: "3"}, want: 7},
		{name: "seconds string", req: &TaskSubmitReq{Seconds: "9"}, want: 9},
		{name: "metadata durationSeconds float", req: &TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": float64(6)}}, want: 6},
		{name: "metadata duration string", req: &TaskSubmitReq{Metadata: map[string]interface{}{"duration": "11"}}, want: 11},
		{
			name: "no hint must not fall back to channel default",
			req:  &TaskSubmitReq{},
			want: 0,
		},
		{name: "missing task request", req: nil, want: 0},
		{name: "non-positive hints are ignored", req: &TaskSubmitReq{Duration: -5, Seconds: "0"}, want: 0},
		{
			name: "oversized metadata hint is clamped",
			req:  &TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": float64(9999999)}},
			want: MaxTaskDurationSeconds,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveTaskExplicitDuration(newDurationContext(tt.req)))
		})
	}
}

func TestDefaultVideoDurationSeconds(t *testing.T) {
	require.Equal(t, 4, DefaultVideoDurationSeconds(constant.ChannelTypeSora))
	require.Equal(t, 4, DefaultVideoDurationSeconds(constant.ChannelTypeOpenAI),
		"OpenAI-compatible relays route to the sora adaptor")
	require.Equal(t, 8, DefaultVideoDurationSeconds(constant.ChannelTypeVertexAi))
	require.Equal(t, 5, DefaultVideoDurationSeconds(constant.ChannelTypeVidu))
	require.Equal(t, 5, DefaultVideoDurationSeconds(-1), "unknown channel gets generic default")
}

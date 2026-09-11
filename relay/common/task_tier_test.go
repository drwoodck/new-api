package common

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionFromDimensionString(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"720x1280", "720p"},       // 竖屏：短边 720
		{"1280x720", "720p"},       // 横屏：短边 720
		{"1920x1080", "1080p"},     // 1080p 横屏
		{"832*480", "480p"},        // 通义万相用 * 分隔
		{"1792x1024", "720p"},      // 短边 1024 → 720p 档
		{"3840x2160", "4k"},        // 短边 ≥2000 → 4k
		{"1080p", "1080p"},         // 标签透传
		{"1080P", "1080p"},         // 大小写归一化
		{"4K", "4k"},               // 4k 归一化
		{"", ""},                   // 空
		{"junk", "junk"},           // 无法解析原样（档表没有即 Unavailable，不错配）
		{"720x1280x1080", "720x1280x1080"}, // 多段无法解析原样
	}
	for _, c := range cases {
		require.Equal(t, c.want, resolutionFromDimensionString(c.input), "input %q", c.input)
	}
}

func taskCtxWithRequest(t *testing.T, req TaskSubmitReq) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", req)
	return ctx
}

func TestBuildTaskTierInputSoraFromSize(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeSora}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{
		Size:     "720x1280",
		Duration: 8,
	}), info)

	assert.Equal(t, "720p", input.Resolution)
	assert.Equal(t, 8, input.DurationSeconds)
}

func TestBuildTaskTierInputSoraDefaultResolution(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeSora}}
	// 未带 size：Sora 默认 720x1280 → 720p
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{Duration: 8}), info)
	assert.Equal(t, "720p", input.Resolution)
}

// 以下四条覆盖「请求体顶层 resolution 字段」这条取值路径。
// 背景:画布一直把分辨率发在 resolution 字段里,而 TaskSubmitReq 此前没有这个
// 字段,encoding/json 直接丢弃 —— 于是档位表按分辨率分的档全部命中不了,
// 所有请求都落到渠道默认的 720p(480p 档白配、1080p 少收)。

func TestBuildTaskTierInputReadsResolutionField(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{
		Resolution: "480p",
		Duration:   5,
	}), info)

	assert.Equal(t, "480p", input.Resolution,
		"请求带的 resolution 必须被采纳,否则 480p 档永远命中不了")
}

func TestBuildTaskTierInputResolutionFieldNormalizesCase(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{Resolution: "720P"}), info)

	assert.Equal(t, "720p", input.Resolution, "大小写按档位键口径归一化")
}

func TestBuildTaskTierInputResolutionFieldBeatsSize(t *testing.T) {
	// 两者都有时以显式 resolution 为准 —— 它是画布实际选中并展示给用户的那一档。
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeSora}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{
		Resolution: "480p",
		Size:       "1280x720",
	}), info)

	assert.Equal(t, "480p", input.Resolution)
}

func TestBuildTaskTierInputIgnoresImageSizeInResolutionField(t *testing.T) {
	// 图片模型用的是同一个字段名,但发的是尺寸档("1K"/"2K"/"4K"),那属于
	// image_size 维度。不能把它当成分辨率 —— 否则图片的 4K 会被错配到视频档位。
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{Resolution: "2K"}), info)

	assert.NotEqual(t, "2k", input.Resolution, "尺寸档不能被当成分辨率")
	assert.Equal(t, "720p", input.Resolution, "认不出时退回渠道默认,而不是猜一个")
}

func TestBuildTaskTierInputDoubaoFromMetadata(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{
		Metadata: map[string]interface{}{"resolution": "1080p"},
	}), info)
	assert.Equal(t, "1080p", input.Resolution)
}

func TestBuildTaskTierInputAliStarSeparator(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeAli}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{Size: "832*480"}), info)
	assert.Equal(t, "480p", input.Resolution)
}

func TestBuildTaskTierInputGeminiDefaultsTo720p(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeGemini}}
	// metadata 与 size 都没有 → Veo 基准 720p
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{}), info)
	assert.Equal(t, "720p", input.Resolution)
}

func TestBuildTaskTierInputKlingModeFromMetadata(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeKling}}
	input := BuildTaskTierInput(taskCtxWithRequest(t, TaskSubmitReq{
		Metadata: map[string]interface{}{"mode": "pro"},
	}), info)
	assert.Equal(t, "pro", input.Mode, "Kling 的分辨率语义由 mode 表达")
}

func TestBuildTaskTierInputMissingRequestIsEmpty(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeSora}}
	input := BuildTaskTierInput(ctx, info)
	assert.Empty(t, input.Resolution, "请求缺失时返回空输入（调用方据 Unavailable 报错）")
}

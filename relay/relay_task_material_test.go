package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMaterialBillingContext 构造带已解析任务请求的 gin 上下文，模拟
// ValidateRequestAndSetAction 之后的提交链路状态。
func newMaterialBillingContext(req relaycommon.TaskSubmitReq) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	c.Set("task_request", req)
	return c
}

// submitExplicitSeconds 复刻 RelayTaskSubmit 步骤 5.7 的实参闭包：只有视频
// 素材采信请求级显式时长提示，音频恒 0（走探测/估算）。
// 注意：与 relay_task.go 步骤 5.7 内联闭包保持一致，改动需两处同步。
func submitExplicitSeconds(c *gin.Context) func(hosttypes.ResolvedInputMaterial) int {
	return func(material hosttypes.ResolvedInputMaterial) int {
		if material.MaterialType != hosttypes.MaterialTypeVideo {
			return 0
		}
		return relaycommon.ResolveTaskExplicitDuration(c)
	}
}

// TestApplyMaterialBillingChargesVideoExplicit 钉住素材计费提交链路落点的主
// 链：视频素材按显式时长 × 每秒原价 × 分组倍率计费，素材快照与额度写回
// PriceData；素材视频价生效时剔除豆包 video_input 倍率（素材已按条计价，
// 再乘该倍率即双计），其余倍率保持不变。
func TestApplyMaterialBillingChargesVideoExplicit(t *testing.T) {
	c := newMaterialBillingContext(relaycommon.TaskSubmitReq{
		Prompt: "dance",
		Metadata: map[string]interface{}{
			"video_url":       "https://example.invalid/input.mp4",
			"durationSeconds": float64(10),
		},
	})
	info := &relaycommon.RelayInfo{}
	info.PriceData = hosttypes.PriceData{
		Quota:          100000,
		GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
	}
	info.PriceData.MaterialPrices = hosttypes.InputMaterialPriceList{
		{MaterialType: hosttypes.MaterialTypeVideo, PricePerSecond: 0.1},
	}
	info.PriceData.AddOtherRatio("video_input", 1.5)
	info.PriceData.AddOtherRatio("seconds", 5)

	materialQuota := applyMaterialBilling(c, info, submitExplicitSeconds(c))

	assert.Equal(t, 500000, materialQuota) // 10s × $0.1/s × 1.0 × 500000
	assert.Equal(t, 500000, info.PriceData.MaterialQuota)
	require.Len(t, info.PriceData.Materials, 1)
	assert.Equal(t, hosttypes.MaterialTypeVideo, info.PriceData.Materials[0].MaterialType)
	assert.Equal(t, 10.0, info.PriceData.Materials[0].Seconds)
	assert.Equal(t, 0.1, info.PriceData.Materials[0].UnitPrice)
	assert.Equal(t, "explicit", info.PriceData.Materials[0].Source)
	ratios := info.PriceData.OtherRatios()
	require.NotNil(t, ratios)
	assert.NotContains(t, ratios, "video_input", "素材视频价生效时 video_input 倍率必须剔除")
	assert.InEpsilon(t, 5.0, ratios["seconds"], 1e-9, "与素材无关的倍率必须保留")
}

// TestApplyMaterialBillingKeepsVideoInputRatioWithoutVideoPrice 钉住双计防护
// 的边界：素材价表没有视频条目时（只配了图片价），video_input 倍率保持不变，
// 未配置价的素材类型不进快照也不计费。
func TestApplyMaterialBillingKeepsVideoInputRatioWithoutVideoPrice(t *testing.T) {
	c := newMaterialBillingContext(relaycommon.TaskSubmitReq{
		Prompt: "dance",
		Images: []string{"https://example.invalid/ref.png"},
		Metadata: map[string]interface{}{
			"video_url": "https://example.invalid/input.mp4",
		},
	})
	info := &relaycommon.RelayInfo{}
	info.PriceData = hosttypes.PriceData{
		GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
	}
	info.PriceData.MaterialPrices = hosttypes.InputMaterialPriceList{
		{MaterialType: hosttypes.MaterialTypeImage, PricePerUnit: 0.01},
	}
	info.PriceData.AddOtherRatio("video_input", 1.5)

	materialQuota := applyMaterialBilling(c, info, submitExplicitSeconds(c))

	assert.Equal(t, 5000, materialQuota) // 1 张 × $0.01 × 1.0 × 500000
	ratios := info.PriceData.OtherRatios()
	require.NotNil(t, ratios)
	assert.Contains(t, ratios, "video_input", "未配置视频素材价时不得剔除 video_input")
	require.Len(t, info.PriceData.Materials, 1)
	assert.Equal(t, hosttypes.MaterialTypeImage, info.PriceData.Materials[0].MaterialType)
}

// TestApplyMaterialBillingNotesQuotaClamp 钉住计费安全：素材条目超额饱和时
// clamp 记到 RelayInfo（经 noteTaskQuotaClamp），供提交日志 admin_info 审计。
func TestApplyMaterialBillingNotesQuotaClamp(t *testing.T) {
	c := newMaterialBillingContext(relaycommon.TaskSubmitReq{
		Prompt: "big",
		Images: []string{"https://example.invalid/huge.png"},
	})
	info := &relaycommon.RelayInfo{}
	info.PriceData = hosttypes.PriceData{
		GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
	}
	info.PriceData.MaterialPrices = hosttypes.InputMaterialPriceList{
		{MaterialType: hosttypes.MaterialTypeImage, PricePerUnit: 1e6},
	}

	materialQuota := applyMaterialBilling(c, info, nil)

	assert.Equal(t, common.MaxQuota, materialQuota)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

// TestRelayTaskSubmitRejectsSaturatedQuotaOnFreeModel 钉住提交链路的饱和守卫：
// 免费模型（GroupRatio=0 且关闭免费模型预扣 → FreeModel=true）跳过
// PreConsumeBilling 时无人检查 QuotaClamp，饱和的素材费/计费额度会经结算
// 无余额检查实扣——提交必须以 quota_saturated 400 拒绝，FreeModel 不例外。
func TestRelayTaskSubmitRejectsSaturatedQuotaOnFreeModel(t *testing.T) {
	prev := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	t.Cleanup(func() { operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = prev })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateGroupRatioByJSONString(`{}`) })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		strings.NewReader(`{"prompt":"dance","metadata":{"video_url":"https://example.invalid/in.mp4"}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel_type", constant.ChannelTypeDoubaoVideo)

	info := &relaycommon.RelayInfo{
		OriginModelName: "material-guard-model",
		UsingGroup:      "default",
		UserSetting:     kitdto.UserSetting{AcceptUnsetRatioModel: true},
		// Action 经内嵌 *TaskRelayInfo 提升,必须显式初始化
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	info.QuotaClamp = &common.QuotaClamp{Op: "QuotaFromFloat", Kind: common.QuotaClampOverflow, Clamped: common.MaxQuota}

	result, taskErr := RelayTaskSubmit(c, info)

	// 场景前置确认:该请求确实被判定为免费模型(即走了跳过预扣的分支)
	assert.True(t, info.PriceData.FreeModel, "场景前置:必须命中 FreeModel 分支")
	assert.Nil(t, info.Billing, "FreeModel 分支不得建立计费会话")
	require.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Equal(t, "quota_saturated", taskErr.Code)
}

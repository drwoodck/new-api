package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tierPriceContext 构造带 task_request 的计费上下文：档位解析需要从请求里
// 读分辨率/时长（BuildTaskTierInput），doubao 渠道读 metadata["resolution"]。
func tierPriceContext(t *testing.T, modelName string, req relaycommon.TaskSubmitReq) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	ctx.Set("group", "default")
	ctx.Set("task_request", req)

	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo},
	}
	return ctx, info
}

func doubao720pReq() relaycommon.TaskSubmitReq {
	return relaycommon.TaskSubmitReq{
		Duration: 10,
		Metadata: map[string]interface{}{"resolution": "720p"},
	}
}

func setTiers(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(jsonStr))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString("{}"))
	})
}

func setGroupRatioTo2(t *testing.T) {
	t.Helper()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
}

// TestModelPriceHelperPerCallTierResolutionSecond 统一模式：resolution 档 × GroupRatio。
func TestModelPriceHelperPerCallTierResolutionSecond(t *testing.T) {
	setTiers(t, `{"ptier-helper-second":[
		{"label":"480P","tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45},
		{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75},
		{"label":"1080P","tier_type":"resolution","key":"1080p","billing_unit":"second","price":1.55}]}`)
	setGroupRatioTo2(t)

	ctx, info := tierPriceContext(t, "ptier-helper-second", doubao720pReq())
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.TierBilling, "档表命中必须标记档位计费")
	require.True(t, priceData.UsePrice)
	// second 档走按秒路径：VideoSecondPrice 承载档位原价，Quota 是 1 秒的额度
	require.InDelta(t, 0.75, priceData.VideoSecondPrice, 1e-9)
	require.Equal(t, int(0.75*common.QuotaPerUnit*2), priceData.Quota, "1 秒额度 = 原价 × QuotaPerUnit × 分组倍率")
	require.InDelta(t, float64(2), priceData.GroupRatioInfo.GroupRatio, 1e-9)
	assert.Equal(t, "resolution", priceData.TierType)
	assert.Equal(t, "720p", priceData.TierKey)
	assert.Equal(t, "second", priceData.TierBillingUnit)
	require.NotNil(t, priceData.TierSnapshot, "预扣必须带快照")
}

// TestModelPriceHelperPerCallTierWinsOverVideoSecondPrice 档表与旧按秒价并存时档表赢。
func TestModelPriceHelperPerCallTierWinsOverVideoSecondPrice(t *testing.T) {
	setVideoSecondPrice(t, `{"ptier-vs-second":0.1}`)
	setTiers(t, `{"ptier-vs-second":[
		{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)

	ctx, info := tierPriceContext(t, "ptier-vs-second", doubao720pReq())
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.TierBilling)
	require.InDelta(t, 0.75, priceData.VideoSecondPrice, 1e-9, "档表价格必须赢过旧按秒单值")
}

// TestModelPriceHelperPerCallTierRequestFixedPrice request 空 key 档 = 固定价按次。
func TestModelPriceHelperPerCallTierRequestFixedPrice(t *testing.T) {
	setTiers(t, `{"ptier-helper-request":[
		{"label":"固定按次","tier_type":"request","billing_unit":"request","price":2.3}]}`)

	ctx, info := tierPriceContext(t, "ptier-helper-request", relaycommon.TaskSubmitReq{Duration: 10})
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.TierBilling)
	require.True(t, priceData.UsePrice)
	assert.Equal(t, "request", priceData.TierBillingUnit)
	require.Zero(t, priceData.VideoSecondPrice, "request 档不走按秒路径")
	require.InDelta(t, 2.3, priceData.ModelPrice, 1e-9)
	require.Equal(t, int(2.3*common.QuotaPerUnit), priceData.Quota, "固定价预扣整额，无差额结算")
}

// TestModelPriceHelperPerCallTierUnavailableIsError 请求档不在表内 → 报错不回退。
func TestModelPriceHelperPerCallTierUnavailableIsError(t *testing.T) {
	setTiers(t, `{"ptier-helper-miss":[
		{"tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45}]}`)

	// 请求 720p，表里只有 480p
	ctx, info := tierPriceContext(t, "ptier-helper-miss", doubao720pReq())
	_, err := ModelPriceHelperPerCall(ctx, info)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "不可用", "缺档必须硬拒绝而不是回退其它档位")
	assert.Contains(t, err.Error(), "ptier-helper-miss")
}

// TestModelPriceHelperPerCallNoTableFallsBackToVideoSecondPrice 无档表 → 旧按秒路径。
func TestModelPriceHelperPerCallNoTableFallsBackToVideoSecondPrice(t *testing.T) {
	setVideoSecondPrice(t, `{"ptier-legacy-model":0.1}`)

	ctx, info := tierPriceContext(t, "ptier-legacy-model", doubao720pReq())
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.False(t, priceData.TierBilling, "未配档表不得标记档位计费")
	require.InDelta(t, 0.1, priceData.VideoSecondPrice, 1e-9, "必须原样走既有按秒路径")
}

// TestModelPriceHelperPerCallGroupPricingRowTiersNoGroupRatio 分别定价：行内档表
// 是终价不叠乘分组倍率（GroupRatio=10 时也不能乘进去）。
func TestModelPriceHelperPerCallGroupPricingRowTiersNoGroupRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "ptier-gp-helper")

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10}`))

	rowTiers := hosttypes.PriceTierList{
		{Label: "480P", TierType: hosttypes.TierTypeResolution, Key: "480p", BillingUnit: hosttypes.BillingUnitSecond, Price: 0.45},
		{Label: "720P", TierType: hosttypes.TierTypeResolution, Key: "720p", BillingUnit: hosttypes.BillingUnitSecond, Price: 0.75},
	}
	require.NoError(t, model.ReplaceModelGroupPrices("ptier-gp-helper", []model.ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &rowTiers},
	}))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("group", "vip")
	ctx.Set("task_request", doubao720pReq())
	info := &relaycommon.RelayInfo{
		OriginModelName: "ptier-gp-helper",
		UserGroup:       "vip",
		UsingGroup:      "vip",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeDoubaoVideo},
	}
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.TierBilling)
	require.InDelta(t, 0.75, priceData.VideoSecondPrice, 1e-9, "行内档表价格不被 GroupRatio 放大")
	require.InDelta(t, float64(1), priceData.GroupRatioInfo.GroupRatio, 1e-9, "分别定价 GroupRatio 恒为 1")
	require.Equal(t, int(0.75*common.QuotaPerUnit), priceData.Quota, "分别定价 = 原价 × QuotaPerUnit × 1")
}

// TestHasModelBillingConfigRecognisesTierTable 只配了档表的模型必须
// 被识别为已配置计费(否则 acceptUnsetRatioModel=false 时从模型列表消失)。
func TestHasModelBillingConfigRecognisesTierTable(t *testing.T) {
	setTiers(t, `{"ptier-only-config":[{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)

	require.True(t, HasModelBillingConfig("ptier-only-config"),
		"纯档表配置必须计入 HasModelBillingConfig")
}

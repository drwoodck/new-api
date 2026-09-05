package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖「分别定价模式的行内秒价」接入 ModelPriceHelperPerCall 后的三个契约:
//   1. 配置分组的行内 VideoSecondPrice 是最终价,不叠乘全局 GroupRatio,
//      且优先于全局秒价(分别定价模式只认行内列,不回退全局);
//   2. 行缺失的对照分组不回退全局秒价 —— 落到标量分支判"分组不可用"报错;
//   3. 未开分别定价的模型继续走全局秒价 × GroupRatio(统一模式回归保护)。
// fixture 初始化复用 price_group_pricing_test.go 的内存库模式。

func saveGroupRatioForSecondPriceTest(t *testing.T, jsonStr string) {
	t.Helper()
	saved := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(saved)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(jsonStr))
}

// TestGroupSecondPriceRowPriceWinsInConfiguredGroup 行内秒价在配置分组生效:
// 终价 0.2,全局秒价(9.99)与全局 GroupRatio(10)都不得影响结果。
func TestGroupSecondPriceRowPriceWinsInConfiguredGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gsp-row-model")
	require.NoError(t, model.ReplaceModelGroupPrices("gsp-row-model", []model.ModelGroupPrice{
		{GroupName: "vip", VideoSecondPrice: float64Ptr(0.2)},
	}))

	// 全局秒价与分组倍率都故意设成显眼的值:行内秒价是最终价,两者都不该出现
	// 在结果里 —— 实现若回退全局或叠乘倍率,会得到 9.99 或 2.0 而非 0.2。
	setVideoSecondPrice(t, `{"gsp-row-model":9.99}`)
	saveGroupRatioForSecondPriceTest(t, `{"vip":10}`)

	ctx, info := newRelayInfoForGroup("gsp-row-model", "vip", "vip")
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.UsePrice)
	assert.InDelta(t, 0.2, priceData.VideoSecondPrice, 1e-9, "行内秒价必须生效")
	assert.InDelta(t, 0.2, priceData.ModelPrice, 1e-9)
	assert.InDelta(t, float64(1), priceData.GroupRatioInfo.GroupRatio, 1e-9,
		"行内秒价是最终价,不叠乘分组倍率")
	assert.False(t, priceData.GroupRatioInfo.HasSpecialRatio,
		"分别定价模式下不应残留特殊倍率标记")
	assert.Equal(t, int(0.2*common.QuotaPerUnit), priceData.Quota,
		"1 秒额度 = 0.2 × QuotaPerUnit × 1")
	assert.False(t, priceData.TierBilling, "无档表不得标记档位计费")
}

// TestGroupSecondPriceMissingRowGroupDoesNotFallBackToGlobal 行缺失的分组:
// 即便全局秒价存在,也不得回退 —— 分别定价模式下该分组不可用,必须报错,
// 而不是按全局秒价静默计费。
func TestGroupSecondPriceMissingRowGroupDoesNotFallBackToGlobal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gsp-miss-row-model")
	require.NoError(t, model.ReplaceModelGroupPrices("gsp-miss-row-model", []model.ModelGroupPrice{
		{GroupName: "vip", VideoSecondPrice: float64Ptr(0.2)},
	}))

	setVideoSecondPrice(t, `{"gsp-miss-row-model":0.1}`)

	ctx, info := newRelayInfoForGroup("gsp-miss-row-model", "default", "default")
	_, err := ModelPriceHelperPerCall(ctx, info)

	require.Error(t, err, "行缺失的分组必须报\"分组不可用\",不能回退全局秒价 0.1")
}

// TestGroupSecondPriceUnifiedModelStillUsesGlobalSecondPrice 统一模式回归保护:
// 未开分别定价的模型继续走全局秒价 × GroupRatio,与接入前行为一致。
func TestGroupSecondPriceUnifiedModelStillUsesGlobalSecondPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	// 同一库里有另一个开了分别定价的模型,锁住"别的模型开分别定价不影响
	// 统一模型的按秒计费"。
	insertGroupPricedModel(t, "gsp-unified-sibling-gp")

	setVideoSecondPrice(t, `{"gsp-unified-model":0.2}`)
	saveGroupRatioForSecondPriceTest(t, `{"default":0.5}`)

	ctx, info := newRelayInfoForGroup("gsp-unified-model", "default", "default")
	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.True(t, priceData.UsePrice)
	assert.InDelta(t, 0.2, priceData.VideoSecondPrice, 1e-9, "统一模式走全局秒价")
	assert.InDelta(t, 0.5, priceData.GroupRatioInfo.GroupRatio, 1e-9,
		"统一模式按全局 GroupRatio 叠乘")
	assert.Equal(t, int(0.2*common.QuotaPerUnit*0.5), priceData.Quota)
}

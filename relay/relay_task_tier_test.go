package relay

import (
	"testing"

	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskIsTierRequestBilling 锁定按次档位计费的判定:只有
// TierBilling && billing_unit=request 才判为固定价(跳过 OtherRatios 应用)。
func TestTaskIsTierRequestBilling(t *testing.T) {
	assert.False(t, taskIsTierRequestBilling(hosttypes.PriceData{}),
		"普通任务不得被判为按次档位")
	assert.False(t, taskIsTierRequestBilling(hosttypes.PriceData{TierBilling: true}),
		"单位缺失不得误判")
	assert.False(t, taskIsTierRequestBilling(hosttypes.PriceData{
		TierBilling: true, TierBillingUnit: hosttypes.BillingUnitSecond,
	}), "按秒档位必须走 OtherRatios 应用路径")
	assert.True(t, taskIsTierRequestBilling(hosttypes.PriceData{
		TierBilling: true, TierBillingUnit: hosttypes.BillingUnitRequest,
	}), "按次档位必须跳过 OtherRatios 应用")
}

// TestTierDimensionRatioKeysFiltering 档位计费必须剔除分辨率维度倍率键
// （size/resolution），保留 seconds 与 video_input 等正交维度。
func TestTierDimensionRatioKeysFiltering(t *testing.T) {
	priceData := hosttypes.PriceData{TierBilling: true}
	priceData.AddOtherRatio("seconds", 10)
	priceData.AddOtherRatio("size", 1.666667)
	priceData.AddOtherRatio("resolution", 2.333)
	priceData.AddOtherRatio("video_input", 1.5)

	for _, key := range hosttypes.TierDimensionRatioKeys {
		priceData.RemoveOtherRatio(key)
	}

	ratios := priceData.OtherRatios()
	require.NotNil(t, ratios)
	require.NotContains(t, ratios, "size", "分辨率维度键必须被剔除")
	require.NotContains(t, ratios, "resolution", "分辨率维度键必须被剔除")
	require.Contains(t, ratios, "seconds", "计费时长必须保留")
	require.Contains(t, ratios, "video_input", "正交成本维度必须保留")
}

// TestTaskIsPerCallFixedBilling 锁定按次固定价的判定:管理员配置 model_price
// 全包价(UsePrice)且未启用档位/按秒时,适配器注入的 seconds 倍率不得应用 ——
// 否则全包价被乘成「单价×时长」(0.9 元/次 × 8 秒)且 PerCallBilling 跳过
// 结算、超扣永不纠偏。
func TestTaskIsPerCallFixedBilling(t *testing.T) {
	assert.True(t, taskIsPerCallFixedBilling(hosttypes.PriceData{UsePrice: true}),
		"按次全包价必须跳过倍率应用")
	assert.False(t, taskIsPerCallFixedBilling(hosttypes.PriceData{
		UsePrice: true, VideoSecondPrice: 0.7,
	}), "按秒计费不吃这个豁免(时长是计费的必要组成)")
	assert.False(t, taskIsPerCallFixedBilling(hosttypes.PriceData{
		UsePrice: true, TierBilling: true,
	}), "档位计费有自己的维度护栏,不走按次豁免")
	assert.False(t, taskIsPerCallFixedBilling(hosttypes.PriceData{}),
		"倍率计费(ratio)保持既有行为")
}

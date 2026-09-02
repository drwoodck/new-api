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

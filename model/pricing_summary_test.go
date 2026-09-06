package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// restoreRatioSettings 快照全部参与换算的全局定价表,测试结束后恢复,
// 避免用例间互相污染(ratio/price/completion/second/tiers/material/group ratio)。
func restoreRatioSettings(t *testing.T) {
	t.Helper()
	savedModelRatio := ratio_setting.ModelRatio2JSONString()
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	savedCompletionRatio := ratio_setting.CompletionRatio2JSONString()
	savedVideoSecond := ratio_setting.VideoSecondPrice2JSONString()
	savedTiers := ratio_setting.VideoPriceTiers2JSONString()
	savedMaterials := ratio_setting.InputMaterialPrices2JSONString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(savedCompletionRatio))
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedVideoSecond))
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(savedTiers))
		require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(savedMaterials))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})
}

// TestGeneratePricingSummaryPreloaded 覆盖 GeneratePricingSummaryPreloaded 全部分支:
// 显式 0 价免费(标量/倍率/素材)、token 倍率、按次固定价、统一/行内秒价、档表
// (全局与行内)、素材价追加、分别定价行缺失。全部用全局 option 表 + 内存行驱动,
// 不依赖 DB。
func TestGeneratePricingSummaryPreloaded(t *testing.T) {
	tests := []struct {
		name                string
		setup               func(t *testing.T)
		row                 *ModelGroupPrice
		groupPricingEnabled bool
		want                string
	}{
		{
			name: "统一模式ratio显式0免费",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sum-model":0}`))
			},
			// ratio 0 是显式配置的免费,与「未配置」区分。
			want: "免费",
		},
		{
			name: "token倍率",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sum-model":2}`))
				require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"sum-model":2}`))
			},
			// 输入价 = ratio × 2 / 1e6 × 1000 = 2 × 0.002 = $0.004/1K;
			// 输出价 = 输入价 × completionRatio = $0.008/1K。
			want: "$0.004/1K 输入 · $0.008/1K 输出",
		},
		{
			name: "按次固定价",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sum-model":0.02}`))
			},
			want: "$0.02/次",
		},
		{
			name: "按次固定价0免费",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sum-model":0}`))
			},
			// 显式 0 固定价 = 免费,与素材段 0 价口径一致。
			want: "免费",
		},
		{
			name: "统一秒价",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"sum-model":0.1}`))
			},
			want: "$0.1/秒",
		},
		{
			name:                "分别定价行内秒价",
			row:                 &ModelGroupPrice{VideoSecondPrice: float64Ptr(0.2)},
			groupPricingEnabled: true,
			want:                "$0.2/秒",
		},
		{
			name: "档表",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(`{"sum-model":[
					{"label":"5秒","tier_type":"request","key":"5s","billing_unit":"second","price":0.05},
					{"label":"10秒","tier_type":"request","key":"10s","billing_unit":"second","price":0.08}]}`))
			},
			want: "档表:5秒 $0.05/秒 · 10秒 $0.08/秒",
		},
		{
			name: "分别定价行内档表",
			row: &ModelGroupPrice{PriceTiers: &types.PriceTierList{
				{Label: "5秒", TierType: types.TierTypeRequest, Key: "5s", BillingUnit: types.BillingUnitSecond, Price: 0.05},
				{Label: "10秒", TierType: types.TierTypeRequest, Key: "10s", BillingUnit: types.BillingUnitSecond, Price: 0.08},
			}},
			groupPricingEnabled: true,
			// 行内档表就是终价,直接复用调用方行,不查 DB、不叠乘倍率。
			want: "档表:5秒 $0.05/秒 · 10秒 $0.08/秒",
		},
		{
			name: "免费+素材价",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sum-model":0}`))
				require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(`{"sum-model":[
					{"material_type":"image","price_per_unit":0.01}]}`))
			},
			// 显式 0 倍率 → 「免费」,素材段照常追加。
			want: "免费 · 输入图 $0.01/张",
		},
		{
			name: "按次+素材价",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sum-model":0.02}`))
				require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(`{"sum-model":[
					{"material_type":"video","price_per_second":0.1},
					{"material_type":"audio","price_per_second":0.002}]}`))
			},
			want: "$0.02/次 · 输入视频 $0.1/秒 · 输入音频 $0.002/秒",
		},
		{
			name: "素材价0为显式免费",
			setup: func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sum-model":0.02}`))
				require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(`{"sum-model":[
					{"material_type":"image","price_per_unit":0}]}`))
			},
			want: "$0.02/次 · 输入图 免费",
		},
		{
			name:                "分别定价行缺失未定价",
			groupPricingEnabled: true,
			want:                "未定价",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreRatioSettings(t)
			if tt.setup != nil {
				tt.setup(t)
			}
			got := GeneratePricingSummaryPreloaded("sum-model", "default", tt.groupPricingEnabled, tt.row)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGeneratePricingSummarySmoke 用真实 DB 行走一遍 GeneratePricingSummary 本体
// (IsGroupPricingEnabled 缓存 + GetModelGroupPrice 查行 + Preloaded 换算),证明
// 两条入口共享同一套换算,不会算出不同的文案。
func TestGeneratePricingSummarySmoke(t *testing.T) {
	restoreRatioSettings(t)
	ptierSetupDB(t)
	insertTierPricedModel(t, "sum-gp-row")

	require.NoError(t, ReplaceModelGroupPrices("sum-gp-row", []ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.02)},
	}))

	got := GeneratePricingSummary("sum-gp-row", "vip")
	assert.Equal(t, "$0.02/次", got)
}

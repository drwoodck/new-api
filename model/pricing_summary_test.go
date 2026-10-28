package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/setting/config"
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
			// 输入价 = ratio × 2 = $4/M(1 ratio = $2/1M);
			// 输出价 = 输入价 × completionRatio = $8/M。输入在前、单位 /M。
			want: "输入$4/M · 输出$8/M",
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
			want: "5秒（$0.05/秒）；10秒（$0.08/秒）",
		},
		{
			name: "分别定价行内档表",
			row: &ModelGroupPrice{PriceTiers: &types.PriceTierList{
				{Label: "5秒", TierType: types.TierTypeRequest, Key: "5s", BillingUnit: types.BillingUnitSecond, Price: 0.05},
				{Label: "10秒", TierType: types.TierTypeRequest, Key: "10s", BillingUnit: types.BillingUnitSecond, Price: 0.08},
			}},
			groupPricingEnabled: true,
			// 行内档表就是终价,直接复用调用方行,不查 DB、不叠乘倍率。
			want: "5秒（$0.05/秒）；10秒（$0.08/秒）",
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

// loadBillingExprConfig 把「模型 → 计费模式/表达式」写进全局配置,测试结束恢复。
// 用 SaveToDB/LoadFromDB 一对,避免用例之间互相污染。
func loadBillingExprConfig(t *testing.T, modes, exprs map[string]string) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })

	modeJSON, err := json.Marshal(modes)
	require.NoError(t, err)
	exprJSON, err := json.Marshal(exprs)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeJSON),
		"billing_setting.billing_expr": string(exprJSON),
	}))
}

// TestGeneratePricingSummaryTieredExprIsNotUnpriced 阶梯表达式计费的模型必须给出
// 人话文案,不能落成「未定价」—— 画布把「未定价」当「没有价格信息」,会把整条
// 模型从列表里藏掉(gpt-6.1-sol / grok-4.7 在 2026-10-26 就是这么消失的)。
// 而且必须**带价**:只写一句「阶梯表达式计费」,用户既比不了价也判断不出贵不贵。
func TestGeneratePricingSummaryTieredExprIsNotUnpriced(t *testing.T) {
	restoreRatioSettings(t)
	loadBillingExprConfig(t,
		map[string]string{"expr-multi": "tiered_expr"},
		map[string]string{"expr-multi": `len <= 272000 ? tier("short", p * 0.9625 + c * 4.8125) : tier("long", p * 1.54 + c * 5.775)`},
	)

	got := GeneratePricingSummaryPreloaded("expr-multi", "default", false, nil)
	assert.Equal(t,
		"上下文≤272K（输入$0.9625/M 输出$4.8125/M）；其余（输入$1.54/M 输出$5.775/M）",
		got,
		"阶梯表达式要按「上下文≤x（输入/输出）」展开,不能只给一句『阶梯表达式计费』")
	assert.NotEqual(t, pricingUnavailable, got, "阶梯表达式不能落成未定价,否则画布隐藏整条模型")
}

// TestDescribeBillingExpr 覆盖表达式 → 人话的三种形态:
// 多档上下文阶梯、单档固定、以及「随请求变价」这种没有单一价的情况。
func TestDescribeBillingExpr(t *testing.T) {
	restoreRatioSettings(t)
	loadBillingExprConfig(t,
		map[string]string{
			"expr-cache":  "tiered_expr",
			"expr-image":  "tiered_expr",
			"expr-header": "tiered_expr",
			"ratio-model": "ratio",
		},
		map[string]string{
			// Claude 风格:缓存读/写单独计价,且分短/长上下文两档。
			"expr-cache": `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75) : tier("long", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5)`,
			// 图片模型:文本 + 图片输入/图片输出,单一档。(pkg/billingexpr 还没支持
			// 缓存图片 img_cr,所以用例里不引用它 —— 引用了会编译失败,只能退回
			// 「阶梯表达式计费」,那是另一条兜底路径。)
			"expr-image": `tier("base", p * 4 + cr * 1 + img * 6.4 + img_o * 24)`,
			// 按请求头变价:同一档里价格随请求变化,没有单一价可列。
			"expr-header": `(tier("base", p * 5 + c * 25)) * (header("anthropic-beta") == "fast-mode" ? 6 : 1)`,
		},
	)

	cache, ok := DescribeBillingExpr("expr-cache")
	assert.True(t, ok)
	assert.Equal(t,
		"上下文≤200K（输入$3/M 输出$15/M 缓存命中$0.3/M 缓存写入$3.75/M）；"+
			"其余（输入$6/M 输出$22.5/M 缓存命中$0.6/M 缓存写入$7.5/M）",
		cache)

	image, ok := DescribeBillingExpr("expr-image")
	assert.True(t, ok, "单档表达式同样要给价")
	assert.Equal(t, "输入$4/M 缓存命中$1/M 图片输入$6.4/M 图片输出$24/M", image)

	// 按请求头变价:基础价照列,但必须提示「部分请求另有倍率」——否则用户按
	// 基础价估算,遇到 fast 模式(×6)会以为被多扣钱。
	header, ok := DescribeBillingExpr("expr-header")
	assert.True(t, ok)
	assert.Equal(t, "输入$5/M 输出$25/M（部分请求另有倍率）", header)

	_, ok = DescribeBillingExpr("ratio-model")
	assert.False(t, ok, "按倍率计费的模型不走表达式口径")
}

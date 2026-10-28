package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

// loadBillingDisplaySettings 把计费设置临时替换成给定值，用例结束后还原。
// billing_setting 与 ratio_setting 都是进程级全局状态，不还原会污染同包其他用例。
func loadBillingDisplaySettings(t *testing.T, values map[string]string) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(values))
}

func mustJSONMap(t *testing.T, m map[string]string) string {
	t.Helper()
	b, err := common.Marshal(m)
	require.NoError(t, err)
	return string(b)
}

// 元信息页价格列：表达式计费模型没有任何 price 字段，必须补上 billing_mode，
// 否则前端四条价格来源全空、只能显示成「-」。
func TestFillModelBillingDisplayTieredExpr(t *testing.T) {
	const modelName = "toapis-size-test-official"
	loadBillingDisplaySettings(t, map[string]string{
		"billing_setting.billing_mode":    mustJSONMap(t, map[string]string{modelName: "tiered_expr"}),
		"billing_setting.billing_expr":    mustJSONMap(t, map[string]string{modelName: `tier("base", p * 4 + img_o * 24)`}),
		"group_ratio_setting.group_ratio": `{"default":1}`,
	})

	m := &model.Model{ModelName: modelName}
	fillModelBillingDisplay(m)

	require.Equal(t, "tiered_expr", m.BillingMode)
	require.Contains(t, m.BillingExpr, "img_o")
	// 表达式模型的价格不在倍率/固定价里，不应再折算每百万 token 价。
	require.Zero(t, m.InputPricePerM)
}

// 按 token 倍率计费：把 ratio 折算成「每百万 token 美元价」，
// 让管理端看到金额而不是裸倍率（折算口径与计费一致：usd = tokens × ratio / QuotaPerUnit）。
func TestFillModelBillingDisplayRatios(t *testing.T) {
	loadBillingDisplaySettings(t, map[string]string{
		"billing_setting.billing_mode":    `{}`,
		"billing_setting.billing_expr":    `{}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	})

	require.Positive(t, common.QuotaPerUnit, "折算依赖 QuotaPerUnit，不能为 0")

	m := &model.Model{ModelName: "ratio-test-model", ModelRatio: 4, CompletionRatio: 6}
	fillModelBillingDisplay(m)

	require.Empty(t, m.BillingMode)
	require.InDelta(t, 4*1_000_000/common.QuotaPerUnit, m.InputPricePerM, 1e-9)
	require.InDelta(t, 6*4*1_000_000/common.QuotaPerUnit, m.OutputPricePerM, 1e-9)
}

// 固定价模型走自己的金额口径，不该被折算成每百万 token 价。
func TestFillModelBillingDisplayKeepsFixedPrice(t *testing.T) {
	loadBillingDisplaySettings(t, map[string]string{
		"billing_setting.billing_mode":    `{}`,
		"billing_setting.billing_expr":    `{}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	})

	m := &model.Model{ModelName: "fixed-price-model", ModelPrice: 0.7, ModelRatio: 4}
	fillModelBillingDisplay(m)

	require.Zero(t, m.InputPricePerM, "有固定价时倍率折算没有意义")
	require.Empty(t, m.BillingMode)
}

// 目录总览的分组价格行：表达式计费既没有档表也没有分组价，必须带上
// billing_mode + 表达式原文，否则展示层只能落到「未配置」。
func TestBuildOverviewGroupPricesTieredExpr(t *testing.T) {
	const modelName = "toapis-overview-test"
	loadBillingDisplaySettings(t, map[string]string{
		"billing_setting.billing_mode": mustJSONMap(t, map[string]string{modelName: "tiered_expr"}),
		"billing_setting.billing_expr": mustJSONMap(t, map[string]string{modelName: `tier("base", p * 4)`}),
	})

	rows := buildOverviewGroupPrices(modelName, false, nil, []string{"内部特惠", "外部vip1"})
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "tiered_expr", row.BillingMode)
		require.Equal(t, `tier("base", p * 4)`, row.BillingExpr)
		require.Nil(t, row.Price, "表达式计费没有单值价格，不能编一个 0")
		require.Nil(t, row.PriceTiers)
	}
}

package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// setupModelGroupPriceTestDB 给每个测试一个独立的内存库,同 canvas_catalog_test.go
// 的 setupCanvasCatalogTestDB —— 不能用共享缓存 DSN,否则行会在测试间累积;
// 且必须在 Cleanup 里把包级 DB 换回原值,否则本包其它测试会撞上表不存在。
func setupModelGroupPriceTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	original := DB
	t.Cleanup(func() {
		DB = original
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	DB = db
	require.NoError(t, db.AutoMigrate(&ModelGroupPrice{}, &Model{}))
}

func float64Ptr(f float64) *float64 { return &f }

func TestReplaceModelGroupPricesInsertsThenReplaces(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(1.0)},
		{GroupName: "vip", ModelRatio: float64Ptr(0.5)},
	}))

	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// 再次调用必须是"整体替换",不是追加 —— 旧的 vip 行应该消失。
	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(2.0)},
	}))
	rows, err = GetModelGroupPrices("m1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "default", rows[0].GroupName)
	assert.Equal(t, 2.0, *rows[0].ModelRatio)
}

func TestReplaceModelGroupPricesWithEmptyRowsClears(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(1.0)},
	}))
	require.NoError(t, ReplaceModelGroupPrices("m1", nil))

	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestReplaceModelGroupPricesDoesNotTouchOtherModels(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(1.0)},
	}))
	require.NoError(t, ReplaceModelGroupPrices("m2", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(9.0)},
	}))

	require.NoError(t, ReplaceModelGroupPrices("m1", nil))

	m2Rows, err := GetModelGroupPrices("m2")
	require.NoError(t, err)
	require.Len(t, m2Rows, 1, "清空 m1 不应影响 m2 的行")
}

func TestGetModelGroupPriceReturnsNilNilWhenNotConfigured(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	row, err := GetModelGroupPrice("does-not-exist", "default")
	require.NoError(t, err, "未配置是正常状态,不是 error")
	assert.Nil(t, row)
}

func TestGetAllModelGroupPricesBucketsByModel(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(1.0)},
		{GroupName: "vip", ModelRatio: float64Ptr(0.5)},
	}))
	require.NoError(t, ReplaceModelGroupPrices("m2", []ModelGroupPrice{
		{GroupName: "default", ModelPrice: float64Ptr(0.2)},
	}))

	all, err := GetAllModelGroupPrices()
	require.NoError(t, err)
	require.Contains(t, all, "m1")
	require.Contains(t, all, "m2")
	assert.Len(t, all["m1"], 2)
	assert.Len(t, all["m2"], 1)
	assert.Equal(t, 0.2, *all["m2"]["default"].ModelPrice)
}

func TestDeleteModelGroupPricesByModel(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	require.NoError(t, ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(1.0)},
	}))
	require.NoError(t, DeleteModelGroupPricesByModel("m1"))

	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestReplaceModelGroupPricesRejectsTiersAndSecondPriceConflict(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	// 档表和行内秒价同时配置应该报错
	tiers := types.PriceTierList{
		{Label: "标清", TierType: "resolution", Key: "480p", BillingUnit: "request", Price: 0.01},
		{Label: "高清", TierType: "resolution", Key: "1080p", BillingUnit: "request", Price: 0.02},
	}
	secondPrice := 0.002

	err := ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{
			GroupName:        "default",
			PriceTiers:       &tiers,
			VideoSecondPrice: &secondPrice,
		},
	})

	require.Error(t, err, "档表和行内秒价同时配置应该报错")
	assert.Contains(t, err.Error(), "档表定价(price_tiers)和行内秒价(video_second_price)不能同时配置")
	assert.Contains(t, err.Error(), "default")

	// 验证没有保存任何数据
	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	assert.Empty(t, rows, "失败的配置不应该保存任何数据")
}

func TestReplaceModelGroupPricesAllowsTiersOnly(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	// 只配置档表应该成功
	tiers := types.PriceTierList{
		{Label: "标清", TierType: "resolution", Key: "480p", BillingUnit: "request", Price: 0.01},
		{Label: "高清", TierType: "resolution", Key: "1080p", BillingUnit: "request", Price: 0.02},
	}

	err := ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{
			GroupName:  "default",
			PriceTiers: &tiers,
		},
	})

	require.NoError(t, err)

	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.NotNil(t, rows[0].PriceTiers)
	assert.Nil(t, rows[0].VideoSecondPrice)
}

func TestReplaceModelGroupPricesAllowsSecondPriceOnly(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	// 只配置行内秒价应该成功
	secondPrice := 0.002

	err := ReplaceModelGroupPrices("m1", []ModelGroupPrice{
		{
			GroupName:        "default",
			VideoSecondPrice: &secondPrice,
		},
	})

	require.NoError(t, err)

	rows, err := GetModelGroupPrices("m1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].PriceTiers)
	assert.NotNil(t, rows[0].VideoSecondPrice)
	assert.Equal(t, 0.002, *rows[0].VideoSecondPrice)
}

// --- ResolveGroupPrice ---
//
// ResolveGroupPrice 的模式判断走 IsGroupPricingEnabled,那个函数读的是
// modelGroupPricingEnabled 这张包级缓存 map,平时只由 updatePricing() 刷新。
// updatePricing() 本身还要读 abilities/channels/vendors 一整套表,是它自己的
// 测试该覆盖的东西,不是这里的关注点。这里直接摆 map 内容(白盒,同包可访问)
// 并把 GetPricing() 的"缓存新鲜"判断喂饱,只验证 ResolveGroupPrice 按开关
// 走对分支,不重新验证 updatePricing 的刷新逻辑本身。
func setGroupPricingEnabledForTest(t *testing.T, modelName string, enabled bool) {
	t.Helper()

	savedMap := modelGroupPricingEnabled
	savedPricingMap := pricingMap
	savedTime := lastGetPricingTime
	t.Cleanup(func() {
		modelGroupPricingEnabled = savedMap
		pricingMap = savedPricingMap
		lastGetPricingTime = savedTime
	})

	next := make(map[string]bool, len(savedMap)+1)
	for k, v := range savedMap {
		next[k] = v
	}
	if enabled {
		next[modelName] = true
	} else {
		delete(next, modelName)
	}
	modelGroupPricingEnabled = next

	// GetPricing() 只在缓存为空或超过 1 分钟才会重新跑 updatePricing() ——
	// 保证非空 + 时间戳够新,让它直接返回,不覆盖上面刚设的 map。
	if len(pricingMap) == 0 {
		pricingMap = []Pricing{{ModelName: "___resolve_group_price_test_placeholder___"}}
	}
	lastGetPricingTime = time.Now()
}

func TestResolveGroupPriceUnifiedModeUsesGlobalPriceTimesGroupRatio(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	setGroupPricingEnabledForTest(t, "unified-model", false)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-model":0.2}`))

	result, err := ResolveGroupPrice("unified-model", "vip")
	require.NoError(t, err)
	assert.True(t, result.Available, "统一模式下永远可用,不存在'该分组不可用'")
	assert.Equal(t, 1, result.QuotaType)
	assert.Equal(t, 0.5, result.GroupRatioApplied)
	assert.InDelta(t, 0.1, result.ModelPrice, 1e-9, "0.2 全局价 × 0.5 分组倍率 = 0.1")
}

func TestResolveGroupPriceSeparateModeHitConfiguredGroup(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	setGroupPricingEnabledForTest(t, "separate-model", true)
	require.NoError(t, ReplaceModelGroupPrices("separate-model", []ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.05)},
	}))

	// 分别定价模式下,全局倍率无论是多少都不应该被叠乘进去 —— 这里故意设一个
	// 明显的倍率(10),如果实现有 bug 把它乘进去,断言会用 0.5 而不是 0.05 抓到。
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10}`))

	result, err := ResolveGroupPrice("separate-model", "vip")
	require.NoError(t, err)
	assert.True(t, result.Available)
	assert.Equal(t, 1, result.QuotaType)
	assert.Equal(t, 0.05, result.ModelPrice)
	assert.Equal(t, float64(1), result.GroupRatioApplied, "分别定价模式下 GroupRatioApplied 恒为 1,不叠乘全局倍率")
}

func TestResolveGroupPriceSeparateModeUnconfiguredGroupIsUnavailable(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	setGroupPricingEnabledForTest(t, "separate-model-2", true)
	require.NoError(t, ReplaceModelGroupPrices("separate-model-2", []ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.05)},
	}))

	result, err := ResolveGroupPrice("separate-model-2", "default")
	require.NoError(t, err)
	assert.False(t, result.Available, "分别定价模式下没配置的分组必须判为不可用")
}

func TestResolveGroupPriceSeparateModeRatioBasedModel(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	setGroupPricingEnabledForTest(t, "separate-ratio-model", true)
	require.NoError(t, ReplaceModelGroupPrices("separate-ratio-model", []ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(3.0), CompletionRatio: float64Ptr(2.0)},
	}))

	result, err := ResolveGroupPrice("separate-ratio-model", "default")
	require.NoError(t, err)
	assert.True(t, result.Available)
	assert.Equal(t, 0, result.QuotaType, "配了 ModelRatio 而非 ModelPrice 时按 token 计费")
	assert.Equal(t, 3.0, result.ModelRatio)
	assert.Equal(t, 2.0, result.CompletionRatio)
	assert.Equal(t, float64(1), result.GroupRatioApplied)
}

// --- ResolveGroupPriceFromPreloaded ---
//
// 零 DB 查询版本,行为必须与 ResolveGroupPrice 完全一致——它们共享
// resolveFromGroupPriceRow/resolveFromGlobalPrice,这里只验证"喂进去的
// groupPricingEnabled/groupPrices 决定走哪条分支",不重复验证换算公式本身
// (公式细节已由上面 ResolveGroupPrice 的测试覆盖)。

func TestResolveGroupPriceFromPreloadedUnifiedMode(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"preload-unified":0.2}`))

	result := ResolveGroupPriceFromPreloaded("preload-unified", "vip", false, nil)
	assert.True(t, result.Available)
	assert.InDelta(t, 0.1, result.ModelPrice, 1e-9)
}

func TestResolveGroupPriceFromPreloadedSeparateModeHit(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	prices := map[string]ModelGroupPrice{
		"vip": {GroupName: "vip", ModelPrice: float64Ptr(0.07)},
	}
	result := ResolveGroupPriceFromPreloaded("preload-separate", "vip", true, prices)
	assert.True(t, result.Available)
	assert.Equal(t, 0.07, result.ModelPrice)
	assert.Equal(t, float64(1), result.GroupRatioApplied)
}

func TestResolveGroupPriceFromPreloadedSeparateModeMiss(t *testing.T) {
	setupModelGroupPriceTestDB(t)

	result := ResolveGroupPriceFromPreloaded("preload-separate-2", "default", true, nil)
	assert.False(t, result.Available, "nil/缺失的分组价格 map 必须判为不可用,不能 panic 或悄悄当统一模式处理")
}

func TestResolveGroupPriceModeSwitchDoesNotLeakAcrossModels(t *testing.T) {
	setupModelGroupPriceTestDB(t)
	setGroupPricingEnabledForTest(t, "separate-b", true)
	// unified-a 有意不出现在 modelGroupPricingEnabled 里(setGroupPricingEnabledForTest
	// 只往里加一个名字),验证的正是"另一个模型开了分别定价,不影响本模型走统一模式"。
	require.NoError(t, ReplaceModelGroupPrices("separate-b", []ModelGroupPrice{
		{GroupName: "default", ModelPrice: float64Ptr(0.3)},
	}))

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-a":0.1}`))

	unified, err := ResolveGroupPrice("unified-a", "default")
	require.NoError(t, err)
	assert.Equal(t, 0.1, unified.ModelPrice, "统一模式模型不受另一个模型开了分别定价影响")

	separate, err := ResolveGroupPrice("separate-b", "default")
	require.NoError(t, err)
	assert.Equal(t, 0.3, separate.ModelPrice, "分别定价模型不受全局 ModelPrice 影响")
}

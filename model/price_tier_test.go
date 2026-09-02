package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// ptierSetupDB 为分别定价用例建独立内存库。不能用 setupModelGroupPriceTestDB
// （只迁移 2 张表）—— insertTierPricedModel 里的 RefreshPricing() 会查
// abilities/channels 表,必须与 relay/helper 的 setupGroupPricingTestDB 一样
// 迁移全套相关表。
func ptierSetupDB(t *testing.T) {
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
	require.NoError(t, db.AutoMigrate(
		&Model{}, &ModelGroupPrice{}, &Ability{}, &Channel{}, &Vendor{},
	))
}

// setTiers 写入全局档表并注册清理,避免污染其它测试的全局 RWMap。
func setTiers(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(jsonStr))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString("{}")) })
}

// setGroupRatio 覆盖某分组倍率并注册清理。
func setGroupRatio(t *testing.T, jsonStr string) {
	t.Helper()
	saved := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(jsonStr))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(saved)) })
}

// insertTierPricedModel 插入开启分别定价的模型行并刷新 IsGroupPricingEnabled 缓存。
func insertTierPricedModel(t *testing.T, name string) {
	t.Helper()
	m := &Model{ModelName: name, Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	RefreshPricing()
}

func resolutionTiers() types.PriceTierList {
	return types.PriceTierList{
		{Label: "480P", TierType: types.TierTypeResolution, Key: "480p", BillingUnit: types.BillingUnitSecond, Price: 0.45},
		{Label: "720P", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 0.75},
		{Label: "1080P", TierType: types.TierTypeResolution, Key: "1080p", BillingUnit: types.BillingUnitSecond, Price: 1.55},
	}
}

// TestResolveTierPriceUnifiedModeKeepsBasePriceAndReportsRatio 统一模式：
// 返回档位原价 + GroupRatioApplied，调用方乘一次（避免双重乘）。
func TestResolveTierPriceUnifiedModeKeepsBasePriceAndReportsRatio(t *testing.T) {
	setTiers(t, `{"ptier-unified-model":[
		{"label":"480P","tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45},
		{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)
	setGroupRatio(t, `{"vip":2}`)

	resolved, err := ResolveTierPrice("ptier-unified-model", "vip", "vip", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionResolved, resolved.Status)
	assert.Equal(t, "720p", resolved.TierKey)
	assert.InDelta(t, 0.75, resolved.Price, 1e-9, "Price 必须是未乘倍率的档位原价")
	assert.Equal(t, types.BillingUnitSecond, resolved.BillingUnit)
	assert.Equal(t, float64(2), resolved.GroupRatioApplied, "倍率由 GroupRatioApplied 单独报告")
	assert.NotNil(t, resolved.TierSnapshot, "预扣必须携带档表快照")
	assert.Len(t, *resolved.TierSnapshot, 2)

	// 快照价格必须保持原价（结算按 原价 × GroupRatio 重算）
	assert.InDelta(t, 0.75, (*resolved.TierSnapshot)[1].Price, 1e-9)
}

// TestResolveTierPriceRequestEmptyKeyFallback request 档：时长键未命中时回退空 key 固定价档。
func TestResolveTierPriceRequestEmptyKeyFallback(t *testing.T) {
	setTiers(t, `{"ptier-req-model":[
		{"label":"10秒","tier_type":"request","key":"10s","billing_unit":"request","price":0.9},
		{"label":"固定按次","tier_type":"request","billing_unit":"request","price":2.3}]}`)

	// 精确命中时长键
	resolved, err := ResolveTierPrice("ptier-req-model", "default", "default", types.TierInput{DurationSeconds: 10})
	require.NoError(t, err)
	require.Equal(t, TierResolutionResolved, resolved.Status)
	assert.InDelta(t, 0.9, resolved.Price, 1e-9)

	// 未命中时长键 → 回退空 key 档（任意请求固定价，对齐 paipu 渠道1 语义）
	resolved, err = ResolveTierPrice("ptier-req-model", "default", "default", types.TierInput{DurationSeconds: 15})
	require.NoError(t, err)
	require.Equal(t, TierResolutionResolved, resolved.Status)
	assert.Equal(t, "", resolved.TierKey)
	assert.InDelta(t, 2.3, resolved.Price, 1e-9)

	// 缺时长输入 → Unavailable
	resolved, err = ResolveTierPrice("ptier-req-model", "default", "default", types.TierInput{})
	require.NoError(t, err)
	require.Equal(t, TierResolutionUnavailable, resolved.Status)
}

// TestResolveTierPriceGroupPricingRowTiersWin 分别定价：行内档表是终价，不叠乘 GroupRatio。
func TestResolveTierPriceGroupPricingRowTiersWin(t *testing.T) {
	ptierSetupDB(t)
	insertTierPricedModel(t, "ptier-gp-model")
	setGroupRatio(t, `{"vip":10}`) // 故意设显眼的倍率，抓"误叠乘"的 bug

	rowTiers := resolutionTiers()
	require.NoError(t, ReplaceModelGroupPrices("ptier-gp-model", []ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &rowTiers},
	}))

	resolved, err := ResolveTierPrice("ptier-gp-model", "vip", "vip", types.TierInput{Resolution: "1080p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionResolved, resolved.Status)
	assert.InDelta(t, 1.55, resolved.Price, 1e-9, "分别定价行内档表必须是不叠乘的终价")
	assert.Equal(t, float64(1), resolved.GroupRatioApplied)
}

// TestResolveTierPriceGroupPricingMissingRowIsNoTable 分别定价 + 全局档表存在时，
// 行缺失 → NoTable（不回退全局档表 × 倍率 —— 未覆盖即不可用的闸门语义）。
func TestResolveTierPriceGroupPricingMissingRowIsNoTable(t *testing.T) {
	ptierSetupDB(t)
	insertTierPricedModel(t, "ptier-gp-miss")
	setTiers(t, `{"ptier-gp-miss":[
		{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)

	// 该分组没有行 → 必须 NoTable，让调用方落回既有"分组不可用"错误
	resolved, err := ResolveTierPrice("ptier-gp-miss", "vip", "vip", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionNoTable, resolved.Status)

	// 行存在但行内无档表（只有旧标量）→ 同样 NoTable，走既有 3 标量语义
	require.NoError(t, ReplaceModelGroupPrices("ptier-gp-miss", []ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(2.3)},
	}))
	resolved, err = ResolveTierPrice("ptier-gp-miss", "vip", "vip", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionNoTable, resolved.Status)
}

// TestResolveTierPriceUnavailableIsHardError 档表存在但请求档缺失 → Unavailable。
func TestResolveTierPriceUnavailableIsHardError(t *testing.T) {
	setTiers(t, `{"ptier-miss-tier":[
		{"tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45},
		{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)

	resolved, err := ResolveTierPrice("ptier-miss-tier", "default", "default", types.TierInput{Resolution: "1080p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionUnavailable, resolved.Status)
	assert.Equal(t, "1080p", resolved.TierKey)

	// 输入维度为空同样 Unavailable（调用方不该拿空档去撞表）
	resolved, err = ResolveTierPrice("ptier-miss-tier", "default", "default", types.TierInput{})
	require.NoError(t, err)
	require.Equal(t, TierResolutionUnavailable, resolved.Status)
}

// TestResolveTierPriceNoTableFallback 无全局档表也无行内档表 → NoTable。
func TestResolveTierPriceNoTableFallback(t *testing.T) {
	resolved, err := ResolveTierPrice("ptier-no-config-model", "default", "default", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionNoTable, resolved.Status)
}

// TestResolveGroupTierTableUnifiedModeScalesCopyNotSource 目录下发：统一模式
// 返回 ×GroupRatio 的终价副本（目录语义 = 已按该分组算好的最终数字，画布不
// 再乘倍率），且缩放不得污染全局档表。
func TestResolveGroupTierTableUnifiedModeScalesCopyNotSource(t *testing.T) {
	setTiers(t, `{"ptier-catalog-model":[
		{"label":"480P","tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45},
		{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)
	setGroupRatio(t, `{"vip":2}`)

	table, err := ResolveGroupTierTable("ptier-catalog-model", "vip")
	require.NoError(t, err)
	require.NotNil(t, table)
	require.NotNil(t, table.PriceTiers)
	assert.InDelta(t, 0.9, (*table.PriceTiers)[0].Price, 1e-9, "目录下发档表必须是 ×GroupRatio 的终价")
	assert.InDelta(t, 1.5, (*table.PriceTiers)[1].Price, 1e-9)
	assert.Equal(t, float64(2), table.GroupRatioApplied)

	// 全局档表原值不得被缩放（副本）
	src, ok := ratio_setting.GetVideoPriceTiers("ptier-catalog-model")
	require.True(t, ok)
	assert.InDelta(t, 0.45, src[0].Price, 1e-9, "全局档表必须保持原价")
}

// TestResolveGroupTierTableGroupPricingRowReturnsRowAsIs 分别定价：返回行内档表原样，
// GroupRatioApplied 恒 1。
func TestResolveGroupTierTableGroupPricingRowReturnsRowAsIs(t *testing.T) {
	ptierSetupDB(t)
	insertTierPricedModel(t, "ptier-catalog-gp")
	setGroupRatio(t, `{"vip":2}`) // 分别模式忽略全局倍率

	rowTiers := types.PriceTierList{
		{Label: "720P", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 0.75},
	}
	require.NoError(t, ReplaceModelGroupPrices("ptier-catalog-gp", []ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &rowTiers},
	}))

	table, err := ResolveGroupTierTable("ptier-catalog-gp", "vip")
	require.NoError(t, err)
	require.NotNil(t, table)
	require.NotNil(t, table.PriceTiers)
	assert.InDelta(t, 0.75, (*table.PriceTiers)[0].Price, 1e-9, "行内档表必须原样下发")
	assert.Equal(t, float64(1), table.GroupRatioApplied)
}

// TestResolveGroupTierTableNoTable 无档表 → nil（目录接口据此不下发档表字段）。
func TestResolveGroupTierTableNoTable(t *testing.T) {
	table, err := ResolveGroupTierTable("ptier-no-table", "default")
	require.NoError(t, err)
	require.Nil(t, table)
}

// TestSelectTierFromSnapshot 结算重选：分辨率升档/降档命中新档，缺档与空输入保持原档。
func TestSelectTierFromSnapshot(t *testing.T) {
	tiers := resolutionTiers()

	// 升档：请求 720p，实际 1080p
	tier, ok := SelectTierFromSnapshot(&tiers, types.TierTypeResolution, "1080P")
	require.True(t, ok)
	assert.InDelta(t, 1.55, tier.Price, 1e-9, "大小写必须归一化后命中")

	// 降档：实际 480p
	tier, ok = SelectTierFromSnapshot(&tiers, types.TierTypeResolution, "480p")
	require.True(t, ok)
	assert.InDelta(t, 0.45, tier.Price, 1e-9)

	// 快照缺该档 → false（保持预扣档，不回退全局）
	_, ok = SelectTierFromSnapshot(&tiers, types.TierTypeResolution, "768p")
	require.False(t, ok)

	// 空实际分辨率 → false
	_, ok = SelectTierFromSnapshot(&tiers, types.TierTypeResolution, "")
	require.False(t, ok)

	// 非 resolution 维度不重选
	_, ok = SelectTierFromSnapshot(&tiers, types.TierTypeRequest, "1080p")
	require.False(t, ok)

	// nil 快照 → false（不 panic，回退 SecondPrice 旧路径）
	_, ok = SelectTierFromSnapshot(nil, types.TierTypeResolution, "1080p")
	require.False(t, ok)
}

// TestResolveTierPriceUnifiedAppliesSpecialGroupRatio 统一模式必须应用用户组
// 特殊倍率（group_group_ratio）—— 与旧按秒/标量路径的 HandleGroupRatio 口径
// 一致；缺失时回退裸分组倍率（审查 C2 回归锁定）。
func TestResolveTierPriceUnifiedAppliesSpecialGroupRatio(t *testing.T) {
	setTiers(t, `{"ptier-special-model":[
		{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`)
	setGroupRatio(t, `{"vip":2}`)
	savedSpecial := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(savedSpecial)) })
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"reseller":{"vip":0.5}}`))

	resolved, err := ResolveTierPrice("ptier-special-model", "reseller", "vip", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	require.Equal(t, TierResolutionResolved, resolved.Status)
	assert.InDelta(t, 0.5, resolved.GroupRatioApplied, 1e-9,
		"命中特殊倍率的用户组必须用特殊倍率,而不是裸分组倍率 2")

	// 未命中特殊倍率的用户组回退裸分组倍率
	resolved, err = ResolveTierPrice("ptier-special-model", "vip", "vip", types.TierInput{Resolution: "720p"})
	require.NoError(t, err)
	assert.InDelta(t, 2.0, resolved.GroupRatioApplied, 1e-9)
}

// TestReplaceModelGroupPricesValidatesRowTiers 行内档表落库前必须经
// NormalizePriceTierList 校验(审查 H2 回归锁定):非法档表整体拒绝。
func TestReplaceModelGroupPricesValidatesRowTiers(t *testing.T) {
	ptierSetupDB(t)

	badTiers := types.PriceTierList{
		{TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: -1},
	}
	err := ReplaceModelGroupPrices("ptier-row-validate", []ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &badTiers},
	})
	require.Error(t, err, "负价档表必须被拒绝")

	// 合法档表落库时键会被归一化(大小写 -> 小写)
	rawTiers := types.PriceTierList{
		{TierType: types.TierTypeResolution, Key: "720P", BillingUnit: types.BillingUnitSecond, Price: 0.75},
	}
	require.NoError(t, ReplaceModelGroupPrices("ptier-row-validate", []ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &rawTiers},
	}))
	row, err := GetModelGroupPrice("ptier-row-validate", "vip")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.NotNil(t, row.PriceTiers)
	assert.Equal(t, "720p", (*row.PriceTiers)[0].Key, "落库档表键必须已归一化")
}

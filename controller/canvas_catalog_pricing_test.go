package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// restoreCatalogPricingRatioSettings 快照全部参与定价文案换算的全局表,测试结束
// 恢复,避免用例间互相污染 —— 与 model/pricing_summary_test.go 的
// restoreRatioSettings 同一思路(controller 包没有现成 helper,补齐)。
func restoreCatalogPricingRatioSettings(t *testing.T) {
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

// setupCatalogPricingTestDB 给 GetCanvasCatalog 一个独立内存库,照抄
// canvas_catalog_wire_test.go 的 fixture 方式(独立 sqlite、model.DB 替换、
// t.Cleanup 关库)。比 wire fixture 多迁 Ability/Channel/Vendor/ModelGroupPrice:
// 「分别定价未定价」分支需要 model.RefreshPricing() 走通 updatePricing
// (查 abilities/channels/vendors 后重建 modelGroupPricingEnabled 缓存)才能让
// IsGroupPricingEnabled 返回 true;纯统一模式用例不受多迁影响。
func setupCatalogPricingTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:canvas_pricing_%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.CanvasCatalogModel{}, &model.Ability{}, &model.Model{}, &model.ModelGroupPrice{},
		&model.Channel{}, &model.Vendor{},
	))

	router := gin.New()
	router.GET("/api/canvas/catalog", GetCanvasCatalog)
	return router
}

// TestCanvasCatalogPricingAutoWhenPerCallPrice 用例 1:目录行 Pricing 为空 + 模型
// 有按次价 → wire.Pricing 自动生成为 "$0.02/次",且 PricingSource="auto"。
// 锁住「目录宣传 = 实际计费」:文案不是手填的,是后端按计费真源现算的。
func TestCanvasCatalogPricingAutoWhenPerCallPrice(t *testing.T) {
	restoreCatalogPricingRatioSettings(t)
	router := setupCatalogPricingTestDB(t)

	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"pricing-auto-per-call":0.02}`))
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "pricing-auto-per-call", DisplayName: "Auto Per Call",
		Contract: "relay_video_async_v1",
		// Pricing 留空:自动文案分支
	}).Error)

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	m := resp.Models[0]
	require.NotNil(t, m.Pricing, "pricing 文案必须下发")
	assert.Equal(t, "$0.02/次", *m.Pricing)
	assert.Equal(t, "auto", m.PricingSource)
}

// TestCanvasCatalogPricingCustomPassthrough 用例 2:管理员手填非空 → 原样下发 +
// PricingSource="custom"。手填是管理员覆盖,后端不得改写(哪怕计费真源不同)。
func TestCanvasCatalogPricingCustomPassthrough(t *testing.T) {
	restoreCatalogPricingRatioSettings(t)
	router := setupCatalogPricingTestDB(t)

	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "pricing-custom", DisplayName: "Custom Copy",
		Contract: "relay_video_async_v1", Pricing: "2.8元/条",
	}).Error)

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	m := resp.Models[0]
	require.NotNil(t, m.Pricing)
	assert.Equal(t, "2.8元/条", *m.Pricing)
	assert.Equal(t, "custom", m.PricingSource)
}

// TestCanvasCatalogPricingUnpricedModel 用例 3:模型无任何价格 → Pricing="未定价"
// + source="auto"。分别定价模式(GroupPricingEnabled)下调用方分组没有任何价格
// 行 = 计费口径的「未定价」,文案逐字与 GeneratePricingSummary 一致,不留空。
func TestCanvasCatalogPricingUnpricedModel(t *testing.T) {
	restoreCatalogPricingRatioSettings(t)
	router := setupCatalogPricingTestDB(t)

	m := &model.Model{ModelName: "pricing-none", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "pricing-none", DisplayName: "Unpriced",
		Contract: "relay_video_async_v1",
	}).Error)

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	w := resp.Models[0]
	require.NotNil(t, w.Pricing)
	assert.Equal(t, "未定价", *w.Pricing)
	assert.Equal(t, "auto", w.PricingSource)
}

// TestCanvasCatalogPricingGeneratedWithoutGroup 用例 4:无分组信息(group 空串)时
// 仍按全局默认价生成文案 + source="auto"。与 group_price 的 fail-open(nil)不同:
// pricing 文案宁可多给也不能缺席 —— 「目录宣传 = 实际计费」的兜底口径。
func TestCanvasCatalogPricingGeneratedWithoutGroup(t *testing.T) {
	restoreCatalogPricingRatioSettings(t)
	router := setupCatalogPricingTestDB(t)

	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"pricing-no-group":0.02}`))
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "pricing-no-group", DisplayName: "No Group",
		Contract: "relay_video_async_v1",
	}).Error)

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	m := resp.Models[0]
	require.NotNil(t, m.Pricing, "无分组信息也必须生成 pricing 文案")
	assert.Equal(t, "$0.02/次", *m.Pricing)
	assert.Equal(t, "auto", m.PricingSource)
}

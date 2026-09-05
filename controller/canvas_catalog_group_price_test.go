package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func float64Ptr(f float64) *float64 { return &f }

// setupCatalogGroupPriceTestDB 同 canvas_catalog_group_filter_test.go 的
// setupCatalogGroupFilterTestDB,额外迁移 Model/ModelGroupPrice(分组分别定价
// 存储)。initModelListColumnNames 仍然必须调 —— GetGroupEnabledModels 用的
// commonGroupCol 只由它初始化,这个包的测试之间不共享这个初始化状态。
func setupCatalogGroupPriceTestDB(t *testing.T, effectiveGroup string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	initModelListColumnNames(t)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
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
	router.Use(func(c *gin.Context) {
		if effectiveGroup != "" {
			common.SetContextKey(c, constant.ContextKeyUsingGroup, effectiveGroup)
		}
		c.Next()
	})
	router.GET("/api/canvas/catalog", GetCanvasCatalog)
	return router
}

func fetchCanvasCatalog(t *testing.T, router *gin.Engine) catalogResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestGetCanvasCatalogUnifiedModeGroupPriceIsGlobalTimesGroupRatio 统一模式:
// group_price 是全局价格 × 调用者分组的 GroupRatio,不查 model_group_price 表。
func TestGetCanvasCatalogUnifiedModeGroupPriceIsGlobalTimesGroupRatio(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "vip")

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":0.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-catalog-model":0.4}`))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "unified-catalog-model", DisplayName: "Unified", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "vip", Model: "unified-catalog-model", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	m := resp.Models[0]
	require.NotNil(t, m.GroupPrice, "有分组信息时必须下发价格")
	assert.Equal(t, 1, m.GroupPrice.QuotaType)
	assert.InDelta(t, 0.2, m.GroupPrice.ModelPrice, 1e-9, "0.4 全局价 × 0.5 vip 倍率 = 0.2")
	assert.Equal(t, 0.5, m.GroupPrice.GroupRatioApplied)
}

// TestGetCanvasCatalogSeparateModeOnlyReturnsCallersOwnGroupPrice 分别定价模式下,
// 只下发调用者自己分组的那一个价格,不泄露其它分组的价格。
func TestGetCanvasCatalogSeparateModeOnlyReturnsCallersOwnGroupPrice(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "vip")

	m := &model.Model{ModelName: "separate-catalog-model", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
	require.NoError(t, model.ReplaceModelGroupPrices("separate-catalog-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.3)},
		{GroupName: "default", ModelPrice: float64Ptr(999)}, // 不该出现在响应里
	}))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "separate-catalog-model", DisplayName: "Separate", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "vip", Model: "separate-catalog-model", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price)
	assert.Equal(t, 0.3, price.ModelPrice, "只能拿到 vip 自己的价格")
	assert.NotEqual(t, float64(999), price.ModelPrice, "default 分组的 999 绝不能泄露给 vip 调用者")
	assert.Equal(t, float64(1), price.GroupRatioApplied, "分别定价模式下不叠乘倍率")
}

// TestGetCanvasCatalogSeparateModeUnconfiguredGroupHasNilPrice 分别定价模式下,
// 调用者的分组没有配置价格时,group_price 必须是 nil(不可用),不是 0。
func TestGetCanvasCatalogSeparateModeUnconfiguredGroupHasNilPrice(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "default")

	m := &model.Model{ModelName: "separate-catalog-model-2", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
	require.NoError(t, model.ReplaceModelGroupPrices("separate-catalog-model-2", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.3)},
	}))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "separate-catalog-model-2", DisplayName: "Separate 2", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "default", Model: "separate-catalog-model-2", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	assert.Nil(t, resp.Models[0].GroupPrice, "没配置价格的分组必须是 nil,不能是 0 或伪造一个价格")
}

// TestGetCanvasCatalogNoGroupContextGroupPriceIsNil 取不到有效分组时
// (fail-open,与 GroupVisible 同一原则)group_price 必须是 nil,不能是 0 ——
// 0 会被画布当"免费"处理并放过余额闸门。
func TestGetCanvasCatalogNoGroupContextGroupPriceIsNil(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "")

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "no-group-context-model", DisplayName: "No Group", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	assert.Nil(t, resp.Models[0].GroupPrice, "无分组信息时必须 fail-open 为 nil,不能是 0")
	assert.True(t, resp.Models[0].GroupVisible, "无分组信息时按可见处理(同一 fail-open 原则)")
}

// TestGetCanvasCatalogSeparateModeRowScalarOverGlobalSecondPrice 锁住分别定价
// 模式下行内无秒价列时目录不再下发全局秒价 —— 与计费路径(ModelPriceHelperPerCall)
// 同一规则:行是唯一价格权威,按行内标量 999 下发,否则目录会宣传一个计费根本
// 不收取的按秒价。(旧契约"全局秒价赢过分别定价"已由分组秒价行内列取代,
// 见 relay/helper/video_group_second_price_test.go。)
func TestGetCanvasCatalogSeparateModeRowScalarOverGlobalSecondPrice(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "default")

	m := &model.Model{ModelName: "video-conflict-catalog-model", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
	require.NoError(t, model.ReplaceModelGroupPrices("video-conflict-catalog-model", []model.ModelGroupPrice{
		{GroupName: "default", ModelPrice: float64Ptr(999)},
	}))

	savedVideoPrice := ratio_setting.VideoSecondPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedVideoPrice)) })
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"video-conflict-catalog-model":0.1}`))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "video-conflict-catalog-model", DisplayName: "Video Conflict", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price)
	require.Nil(t, price.VideoSecondPrice, "行内无秒价列不得下发全局秒价")
	assert.Equal(t, 999.0, price.ModelPrice, "必须按行内标量固定价下发")
	assert.Equal(t, 1, price.QuotaType)
	assert.Equal(t, float64(1), price.GroupRatioApplied, "分别定价模式下不叠乘倍率")
}

// TestGetCanvasCatalogSeparateModeRowSecondPriceDispatched 分别定价模式的行内
// 秒价按调用者分组下发:终价、不叠乘分组倍率(GroupRatioApplied=1),且优先于
// 全局秒价 —— 与 ModelPriceHelperPerCall 的行内秒价分支同序同价。
func TestGetCanvasCatalogSeparateModeRowSecondPriceDispatched(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "vip")

	m := &model.Model{ModelName: "gsp-catalog-model", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
	require.NoError(t, model.ReplaceModelGroupPrices("gsp-catalog-model", []model.ModelGroupPrice{
		{GroupName: "vip", VideoSecondPrice: float64Ptr(0.2)},
		{GroupName: "default", VideoSecondPrice: float64Ptr(9.99)}, // 不得泄露
	}))

	// 全局秒价与全局倍率都设成显眼值:行内秒价是终价,两者都不该出现在结果里。
	savedVideoPrice := ratio_setting.VideoSecondPrice2JSONString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedVideoPrice))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"gsp-catalog-model":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10}`))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "gsp-catalog-model", DisplayName: "Group Second", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "vip", Model: "gsp-catalog-model", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price)
	require.NotNil(t, price.VideoSecondPrice)
	assert.Equal(t, 0.2, *price.VideoSecondPrice, "必须下发调用者分组的行内秒价,不是全局 0.1 也不是其它分组的 9.99")
	assert.Equal(t, 0.2, price.ModelPrice)
	assert.Equal(t, 1, price.QuotaType)
	assert.Equal(t, float64(1), price.GroupRatioApplied, "行内秒价是终价,GroupRatioApplied 恒 1")
}

// TestGetCanvasCatalogTierTableBeatsVideoSecondPrice 档表模型目录下发：
// 档表优先于旧按秒单值，下发原价档表 + GroupRatioApplied。
func TestGetCanvasCatalogTierTableBeatsVideoSecondPrice(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "default")

	savedSecond := ratio_setting.VideoSecondPrice2JSONString()
	savedTiers := ratio_setting.VideoPriceTiers2JSONString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedSecond))
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(savedTiers))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"ptier-catalog":0.1}`))
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(`{"ptier-catalog":[
		{"label":"480P","tier_type":"resolution","key":"480p","billing_unit":"second","price":0.45},
		{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "ptier-catalog", DisplayName: "Tiered", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "default", Model: "ptier-catalog", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price, "档表模型必须下发价格")
	require.NotNil(t, price.PriceTiers, "必须下发档表而非旧字段")
	require.Nil(t, price.VideoSecondPrice, "档表路径必须让旧按秒字段缺席")
	require.Len(t, *price.PriceTiers, 2)
	assert.InDelta(t, 0.9, (*price.PriceTiers)[0].Price, 1e-9, "目录档表必须是 ×GroupRatio 的终价(0.45 × default 组倍率 2)")
	assert.InDelta(t, 1.5, (*price.PriceTiers)[1].Price, 1e-9)
	assert.Equal(t, 1, price.QuotaType)
	assert.Equal(t, float64(2), price.GroupRatioApplied)
}

// TestGetCanvasCatalogSeparateModeRowTiersOnlyForOwnGroup 分别定价 + 行内档表：
// 只下发调用者分组的行内档表（原价、GroupRatioApplied 恒 1）。
func TestGetCanvasCatalogSeparateModeRowTiersOnlyForOwnGroup(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "vip")

	m := &model.Model{ModelName: "ptier-catalog-gp", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()

	tiers := types.PriceTierList{
		{Label: "720P", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 0.75},
	}
	require.NoError(t, model.ReplaceModelGroupPrices("ptier-catalog-gp", []model.ModelGroupPrice{
		{GroupName: "vip", PriceTiers: &tiers},
	}))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "ptier-catalog-gp", DisplayName: "RowTiered", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "vip", Model: "ptier-catalog-gp", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price)
	require.NotNil(t, price.PriceTiers)
	require.Len(t, *price.PriceTiers, 1)
	assert.InDelta(t, 0.75, (*price.PriceTiers)[0].Price, 1e-9)
	assert.Equal(t, float64(1), price.GroupRatioApplied, "分别定价 GroupRatio 恒 1")
}

// TestGetCanvasCatalogNoTierTableKeepsLegacyFields 无档表模型目录下发原字段，
// 档表字段缺席（旧路径回归）。
func TestGetCanvasCatalogNoTierTableKeepsLegacyFields(t *testing.T) {
	router := setupCatalogGroupPriceTestDB(t, "default")

	savedSecond := ratio_setting.VideoSecondPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedSecond)) })
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"ptier-legacy-catalog":0.1}`))

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "ptier-legacy-catalog", DisplayName: "Legacy", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	model.DB.Create(&model.Ability{Group: "default", Model: "ptier-legacy-catalog", ChannelId: 1, Enabled: true})

	resp := fetchCanvasCatalog(t, router)
	require.Len(t, resp.Models, 1)
	price := resp.Models[0].GroupPrice
	require.NotNil(t, price)
	require.Nil(t, price.PriceTiers, "无档表模型不得下发档表字段")
	require.NotNil(t, price.VideoSecondPrice, "旧按秒字段必须照旧下发")
}

package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// setupCatalogOverviewTestDB 给 GetCanvasCatalogOverviewAdmin 一个独立内存库,
// 同 canvas_catalog_group_filter_test.go 的 setupCatalogGroupFilterTestDB ——
// 只是这个视图还要读 models 表(GetEnabledModels 只碰 abilities,不需要
// initModelListColumnNames 那套 commonGroupCol 初始化,GetGroupEnabledModels
// 才需要)。
func setupCatalogOverviewTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	original := model.DB
	t.Cleanup(func() {
		model.DB = original
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Ability{}, &model.Model{}, &model.CanvasCatalogModel{}, &model.ModelGroupPrice{},
		&model.Channel{},
	))
	// GetEnabledModels 是 abilities INNER JOIN channels,只统计 status=1 的渠道。
	// 没有 channels 表时那句 SQL 整条报错、已启用模型集合恒为空,所有依赖
	// abilities 的断言都会假性通过(或直接找不到行) —— 各用例里的
	// ChannelId:1 指的就是这条。
	require.NoError(t, db.Create(&model.Channel{Id: 1, Key: "test-key", Status: 1, Name: "test-channel"}).Error)

	router := gin.New()
	router.GET("/api/canvas/admin/catalog-overview", GetCanvasCatalogOverviewAdmin)
	router.GET("/api/canvas/catalog", GetCanvasCatalog)
	return router
}

func getOverview(t *testing.T, router *gin.Engine) []canvasCatalogOverviewRow {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/admin/catalog-overview", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp adminApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)

	var rows []canvasCatalogOverviewRow
	require.NoError(t, json.Unmarshal(resp.Data, &rows))
	return rows
}

// TestCatalogOverviewSplitsIntoConfiguredAndUnconfigured 锁住整个视图存在的
// 理由:一个已启用但从未建过目录条目的模型必须出现(落"未配置"),
// 一个已配置且契约可用的模型必须判 ready(落"已配置")。
func TestCatalogOverviewSplitsIntoConfiguredAndUnconfigured(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	model.DB.Create(&model.Ability{Group: "default", Model: "configured-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.Ability{Group: "default", Model: "unconfigured-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "configured-model", DisplayName: "Configured", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}

	require.Contains(t, byName, "configured-model")
	assert.True(t, byName["configured-model"].Ready)
	assert.Equal(t, "Configured", byName["configured-model"].DisplayName)

	require.Contains(t, byName, "unconfigured-model", "已启用但没建目录条目的模型必须出现在总览里(未配置页)")
	assert.False(t, byName["unconfigured-model"].Ready)
	assert.Empty(t, byName["unconfigured-model"].DisplayName)
	assert.Zero(t, byName["unconfigured-model"].CatalogID)
}

// TestCatalogOverviewCarriesModelMetaDisplayNameAndDescription 锁住元信息页
// 那两列在总览里的来源:display_name 来自 canvas_catalog_model 行(目录侧可改),
// meta_display_name / description 来自 models 行(元信息页维护,目录侧只读)。
// 未配置的模型只有后者 —— "配置画布参数"要靠它预填表单,少了就得重打一遍。
func TestCatalogOverviewCarriesModelMetaDisplayNameAndDescription(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	model.DB.Create(&model.Ability{Group: "default", Model: "configured-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.Ability{Group: "default", Model: "unconfigured-model", ChannelId: 1, Enabled: true})
	require.NoError(t, (&model.Model{
		ModelName: "configured-model", Status: 1,
		DisplayName: "元信息显示名", Description: "元信息说明",
	}).Insert())
	require.NoError(t, (&model.Model{
		ModelName: "unconfigured-model", Status: 1,
		DisplayName: "未配置的显示名", Description: "未配置的说明",
	}).Insert())
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "configured-model", DisplayName: "目录显示名", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}

	require.Contains(t, byName, "configured-model")
	assert.Equal(t, "目录显示名", byName["configured-model"].DisplayName, "DisplayName 仍是目录条目自己的值")
	assert.Equal(t, "元信息显示名", byName["configured-model"].MetaDisplayName)
	assert.Equal(t, "元信息说明", byName["configured-model"].Description)

	require.Contains(t, byName, "unconfigured-model")
	assert.Empty(t, byName["unconfigured-model"].DisplayName, "没有目录条目就没有目录显示名")
	assert.Equal(t, "未配置的显示名", byName["unconfigured-model"].MetaDisplayName)
	assert.Equal(t, "未配置的说明", byName["unconfigured-model"].Description)
}

// TestCatalogOverviewIncludesCatalogRowsMissingFromAbilities 锁住"取并集,
// 不是只取 abilities" —— 一个已经配过目录、但渠道暂时停用/删除导致不在
// abilities 里的模型,不该从总览里消失(否则运营方会看到自己配过的条目
// 凭空不见)。
func TestCatalogOverviewIncludesCatalogRowsMissingFromAbilities(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	// 故意不插 abilities 行
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "orphaned-catalog-entry", DisplayName: "Orphaned", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}
	require.Contains(t, byName, "orphaned-catalog-entry")
	assert.True(t, byName["orphaned-catalog-entry"].Ready)
}

// TestCatalogOverviewUnknownContractIsNotReady 契约不在画布支持清单内 ——
// 即便有目录行、有 display_name,也不能算 ready(画布会把这条整条跳过)。
func TestCatalogOverviewUnknownContractIsNotReady(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	model.DB.Create(&model.Ability{Group: "default", Model: "bad-contract-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "bad-contract-model", DisplayName: "Bad Contract", Capabilities: "audio_gen",
		Enabled: boolPtr(true), Contract: "relay_audio_async_v1", RequiresVocab: 1,
	})

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}
	require.Contains(t, byName, "bad-contract-model")
	assert.False(t, byName["bad-contract-model"].Ready)
}

// TestCatalogOverviewUnifiedModeGroupPrices 统一模式:每个分组都算出
// 全局价格 × 该分组倍率,不查 model_group_price 表。
func TestCatalogOverviewUnifiedModeGroupPrices(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":0.5}`))

	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-priced-model":0.2}`))

	m := &model.Model{ModelName: "unified-priced-model", Status: 1, GroupPricingEnabled: false}
	require.NoError(t, m.Insert())
	model.DB.Create(&model.Ability{Group: "default", Model: "unified-priced-model", ChannelId: 1, Enabled: true})

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}
	require.Contains(t, byName, "unified-priced-model")
	assert.False(t, byName["unified-priced-model"].GroupPricingEnabled)

	pricesByGroup := make(map[string]*float64, len(byName["unified-priced-model"].GroupPrices))
	for _, gp := range byName["unified-priced-model"].GroupPrices {
		pricesByGroup[gp.GroupName] = gp.Price
	}
	require.Contains(t, pricesByGroup, "default")
	require.Contains(t, pricesByGroup, "vip")
	require.NotNil(t, pricesByGroup["default"])
	require.NotNil(t, pricesByGroup["vip"])
	assert.InDelta(t, 0.2, *pricesByGroup["default"], 1e-9)
	assert.InDelta(t, 0.1, *pricesByGroup["vip"], 1e-9, "0.2 全局价 × 0.5 vip 倍率 = 0.1")
}

// TestCatalogOverviewSeparateModeMissingGroupShowsNilPrice 分别定价模式下,
// 一个模型没给某分组配价格时,该分组的 price 必须是 nil(不是 0、不是缺省
// 到全局价格)—— 这是"没配价"在总览里唯一可见的地方,不能静默留空或伪造。
func TestCatalogOverviewSeparateModeMissingGroupShowsNilPrice(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))

	m := &model.Model{ModelName: "separate-priced-model", Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.DB.Create(&model.Ability{Group: "default", Model: "separate-priced-model", ChannelId: 1, Enabled: true})

	price := 0.5
	require.NoError(t, model.ReplaceModelGroupPrices("separate-priced-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: &price},
	}))

	rows := getOverview(t, router)
	byName := make(map[string]canvasCatalogOverviewRow, len(rows))
	for _, r := range rows {
		byName[r.ModelName] = r
	}
	require.Contains(t, byName, "separate-priced-model")
	assert.True(t, byName["separate-priced-model"].GroupPricingEnabled)

	pricesByGroup := make(map[string]*float64, len(byName["separate-priced-model"].GroupPrices))
	for _, gp := range byName["separate-priced-model"].GroupPrices {
		pricesByGroup[gp.GroupName] = gp.Price
	}
	require.Contains(t, pricesByGroup, "default")
	require.Contains(t, pricesByGroup, "vip")
	assert.Nil(t, pricesByGroup["default"], "分别定价模式下未配置的分组必须是 nil,不能是 0 或全局价格")
	require.NotNil(t, pricesByGroup["vip"])
	assert.Equal(t, 0.5, *pricesByGroup["vip"])
}

// TestCatalogOverviewDoesNotAffectCanvasCatalogDispatch 回归断言:总览接口的
// 存在与调用,不改变 /api/canvas/catalog 的下发内容 —— 后者仍然只读
// canvas_catalog_model 表,未配置模型(只在总览视图里出现的那些)绝不能
// 出现在真正下发给画布客户端的响应里。
func TestCatalogOverviewDoesNotAffectCanvasCatalogDispatch(t *testing.T) {
	router := setupCatalogOverviewTestDB(t)

	model.DB.Create(&model.Ability{Group: "default", Model: "configured-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.Ability{Group: "default", Model: "unconfigured-model", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "configured-model", DisplayName: "Configured", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})

	// 先调一次总览(触发它自己的所有内存计算),再调真正的下发接口。
	overviewRows := getOverview(t, router)
	require.Len(t, overviewRows, 2, "总览应该看到两个模型(已配置 + 未配置)")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var catalogResp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalogResp))
	require.Len(t, catalogResp.Models, 1, "下发内容只应包含 canvas_catalog_model 表里的那一条,不受未配置模型影响")
	assert.Equal(t, "configured-model", catalogResp.Models[0].RemoteID)
}

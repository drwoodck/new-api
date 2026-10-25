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
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupCatalogGroupFilterTestDB 与 setupCatalogTestDB(canvas_catalog_test.go)同一建库
// 方式,额外迁移 abilities 与 model_group_prices 表 —— 目录下发现在同时按
// 「分组可用模型(abilities)」与「该分组配过价(model_group_prices)」收窄。
// 路由前插入一个中间件,把 constant.ContextKeyUsingGroup 写进 context ——
// 模拟 TokenAuthReadOnly 已经算好的有效分组,不重新走完整鉴权链路。
func setupCatalogGroupFilterTestDB(t *testing.T, effectiveGroup string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// model.GetGroupEnabledModels builds its WHERE clause with the unexported
	// commonGroupCol (backtick- or quote-escaped "group" column name), which is
	// only populated by model.InitDB() -> initCol(). Reuse the same
	// initModelListColumnNames helper model_list_test.go already uses for this
	// exact reason, rather than inventing new test infra.
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
		&model.CanvasCatalogModel{},
		&model.Ability{},
		&model.Channel{},
		&model.Model{},
		&model.ModelGroupPrice{},
		&model.Vendor{},
	))
	// GetGroupEnabledModels 是 abilities INNER JOIN channels 且只认 status=1;
	// 没有这条渠道行就一行都匹配不到 —— 分组过滤会退化成「只按价格判定」。
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Key: "group-filter-key", Status: common.ChannelStatusEnabled,
		Name: "group-filter-channel",
	}).Error)

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

func floatPtr(v float64) *float64 { return &v }

func createCatalogModel(t *testing.T, remoteID string) {
	t.Helper()
	createCatalogModelEnabled(t, remoteID, true)
}

func createCatalogModelEnabled(t *testing.T, remoteID string, enabled bool) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: remoteID, DisplayName: remoteID, Capabilities: "video_gen",
		Enabled: boolPtr(enabled), Contract: "c1", RequiresVocab: 1,
	}).Error)
}

// createGroupPricedModel 建一个「开了分组分别定价 + 该分组有价」的模型。
//
// 必须同时写 models.group_pricing_enabled=true:目录下发的"有没有价"复用了
// 计费侧的 hasBillingConfigForUser —— 只有开了分组定价的模型才看分组价格行,
// 否则走全局 ratio_setting(统一模式)。
func createGroupPricedModel(t *testing.T, remoteID, group string, price float64) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Model{
		ModelName: remoteID, Status: 1, GroupPricingEnabled: true,
	}).Error)
	model.RefreshPricing()
	require.NoError(t, model.DB.Create(&model.ModelGroupPrice{
		ModelName: remoteID, GroupName: group, ModelPrice: floatPtr(price),
	}).Error)
}

func fetchCatalog(t *testing.T, router *gin.Engine) catalogResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func catalogIDs(resp catalogResponse) []string {
	out := make([]string, 0, len(resp.Models))
	for _, m := range resp.Models {
		out = append(out, m.RemoteID)
	}
	return out
}

// TestGetCanvasCatalogOnlyDeliversGroupModelsWithGroupPrice 锁住目录下发的两条收窄规则:
//
//  1. 只下发调用者所在分组可用的模型(abilities 里有该分组的启用行);
//  2. 只下发该分组配过价的模型 —— 没配价的模型即便分组可见也不下发,
//     否则画布会把一个自己分组根本计不了价的模型同步进本地模型表。
//
// 分组打不到的模型必须**整条不下发**:画布拿不到条目就不会把它同步进来,
// 这正是「用户更新模型时不能多出别组的模型」的要求。
func TestGetCanvasCatalogOnlyDeliversGroupModelsWithGroupPrice(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "vip")

	createCatalogModel(t, "vip-priced")                   // 分组可用 + 有价 → 下发
	createCatalogModel(t, "vip-unpriced")                 // 分组可用但没价 → 不下发
	createCatalogModel(t, "other-group")                  // 分组不可用、有价 → 不下发
	createCatalogModelEnabled(t, "vip-priced-old", false) // 分组可用 + 有价,但运营方已停用 → 仍下发(enabled=false 表达下线)

	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "vip-priced", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "vip-unpriced", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "vip-priced-old", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "other-group", ChannelId: 1, Enabled: true}).Error)
	createGroupPricedModel(t, "vip-priced", "vip", 1.5)
	createGroupPricedModel(t, "other-group", "vip", 2)
	createGroupPricedModel(t, "vip-priced-old", "vip", 0.8)

	resp := fetchCatalog(t, router)
	assert.ElementsMatch(t, []string{"vip-priced", "vip-priced-old"}, catalogIDs(resp),
		"只下发「本分组可用 + 本分组有价」的模型")
}

// TestGetCanvasCatalogPriceTableDefinesGroupModelsWhenAbilitiesEmpty 覆盖
// 「分组在 abilities 里没有行」的形态(例如 内部特惠2:只配了价格表,没有能力行):
// 这时分组模型集合由价格表本身表达 —— 有价的下发,没价的不下发。
//
// 这条不是 fail-open:如果按 abilities 判空就把整份目录发下去,用户会同步到
// 一堆自己分组根本计不了价的模型,正是本次要禁止的行为。
func TestGetCanvasCatalogPriceTableDefinesGroupModelsWhenAbilitiesEmpty(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "group-with-only-prices")

	createCatalogModel(t, "priced-only")
	createCatalogModel(t, "unpriced")

	// 统一模式 + 全局有价,但该分组一行价格都没有 —— 没有任何"这个分组有它"的
	// 信号,不能出现在这个分组的目录里(否则内部特惠2 会凭空多出对话模型)。
	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-elsewhere":0.4}`))
	createCatalogModel(t, "unified-elsewhere")

	createGroupPricedModel(t, "priced-only", "group-with-only-prices", 3)

	resp := fetchCatalog(t, router)
	assert.Equal(t, []string{"priced-only"}, catalogIDs(resp))
	assert.True(t, resp.Models[0].GroupVisible, "价格表表达的分组模型视为可见")
}

// TestGetCanvasCatalogUnifiedModeModelStaysDeliverable 统一模式(未开分组定价、
// 价格配在全局 ratio_setting)的模型没有分组价格行,但它对一个分组是真实可计费的
// —— 必须照常下发。只查分组价格表会把这些模型(线上就是 7 个对话模型)整批
// 判成"没价"、从画布目录里消失,而直接调用又是通的。
func TestGetCanvasCatalogUnifiedModeModelStaysDeliverable(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "vip")

	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-priced":0.4}`))

	createCatalogModel(t, "unified-priced")   // 统一模式 + 全局有价 → 下发
	createCatalogModel(t, "unified-unpriced") // 统一模式 + 全局也没价 → 不下发
	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "unified-priced", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "unified-unpriced", ChannelId: 1, Enabled: true}).Error)

	resp := fetchCatalog(t, router)
	assert.Equal(t, []string{"unified-priced"}, catalogIDs(resp),
		"统一模式模型靠全局价可计费,不能因为缺少分组价格行被滤掉")
}

// TestGetCanvasCatalogNoGroupContextDeliversAll 取不到有效分组(context key 缺省)时
// 不做收窄:读不到分组就不该按某个分组去砍目录,那是比漏过滤严重得多的故障。
func TestGetCanvasCatalogNoGroupContextDeliversAll(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "")

	createCatalogModel(t, "any-model")
	createCatalogModel(t, "another-model")

	resp := fetchCatalog(t, router)
	assert.ElementsMatch(t, []string{"any-model", "another-model"}, catalogIDs(resp))
}

// TestGetCanvasCatalogSkipsPriceFilterWhenPriceTableUnreadable 价格表读不出来时
// 只跳过价格收窄(仍按 abilities 收窄),不能让整份目录变空 —— 把一次 DB 故障
// 判成「所有模型都没配价」会让所有客户端的模型列表一起消失。
func TestGetCanvasCatalogSkipsPriceFilterWhenPriceTableUnreadable(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "vip")

	createCatalogModel(t, "vip-model")
	require.NoError(t, model.DB.Create(&model.Ability{Group: "vip", Model: "vip-model", ChannelId: 1, Enabled: true}).Error)

	// 制造价格表读取失败:迁移后立刻删表,GetAllModelGroupPrices 必然报错。
	require.NoError(t, model.DB.Migrator().DropTable(&model.ModelGroupPrice{}))

	resp := fetchCatalog(t, router)
	assert.Equal(t, []string{"vip-model"}, catalogIDs(resp), "价格表故障时不能把分组内模型全部砍掉")
}

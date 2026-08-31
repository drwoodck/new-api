package controller

import (
	"strings"
	"fmt"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupCatalogGroupFilterTestDB 与 setupCatalogTestDB(canvas_catalog_test.go)同一建库
// 方式,额外迁移 abilities 表(GetGroupEnabledModels 读的表)。路由前插入一个
// 中间件,把 constant.ContextKeyUsingGroup 写进 context —— 模拟 TokenAuthReadOnly
// 已经算好的有效分组,不重新走完整鉴权链路(那需要真实 token/user 记录)。
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
	require.NoError(t, db.AutoMigrate(&model.CanvasCatalogModel{}, &model.Ability{}))

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

// TestGetCanvasCatalogMarksGroupVisibilityWithoutTouchingEnabled 端到端锁住
// 分组过滤的三条不变量,每条都对应一个曾经写错或差点写错的实现:
//
//  1. **不删行** —— 分组打不到的模型必须仍在响应里。abilities 只含已启用能力,
//     所以任何被分组排除的模型若直接消失,客户端就分不清「这个分组用不了」
//     与「这个模型被删了」。
//  2. **不改 enabled** —— 它必须如实反映库里存的值。画布把 enabled 写进本地
//     models.enabled,而那一列同时是用户自己的勾选开关且同步时无条件覆写;
//     从目录侧写它会静默清掉用户的选择。
//  3. **可见性走独立的 group_visible 字段** —— 这才是表达「这个分组能不能用」
//     的地方。
func TestGetCanvasCatalogMarksGroupVisibilityWithoutTouchingEnabled(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "vip")

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "vip-only", DisplayName: "VIP Only", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1, SortOrder: 1,
	})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "default-only", DisplayName: "Default Only", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1, SortOrder: 2,
	})
	// 运营方自己停用的条目 —— 用来验证 enabled 与 group_visible 是两个独立维度
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "vip-retired", DisplayName: "VIP Retired", Capabilities: "video_gen",
		Enabled: boolPtr(false), Contract: "c1", RequiresVocab: 1, SortOrder: 3,
	})
	// abilities: vip 分组能用 vip-only 与 vip-retired
	model.DB.Create(&model.Ability{Group: "vip", Model: "vip-only", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.Ability{Group: "vip", Model: "vip-retired", ChannelId: 1, Enabled: true})
	model.DB.Create(&model.Ability{Group: "default", Model: "default-only", ChannelId: 1, Enabled: true})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// (1) 三行都在
	require.Len(t, resp.Models, 3, "分组过滤绝不删行")

	byID := make(map[string]canvasCatalogWireModel, len(resp.Models))
	for _, m := range resp.Models {
		byID[m.RemoteID] = m
	}
	require.Contains(t, byID, "vip-only")
	require.Contains(t, byID, "default-only")
	require.Contains(t, byID, "vip-retired")

	// (2) enabled 一律是库里存的值
	assert.True(t, byID["vip-only"].Enabled, "库里存的是 true")
	assert.True(t, byID["default-only"].Enabled,
		"vip 用不了它,但库里存的是 true —— 绝不能因为分组而改写 enabled")
	assert.False(t, byID["vip-retired"].Enabled, "运营方停用的,库里存的是 false")

	// (3) group_visible 才是分组维度
	assert.True(t, byID["vip-only"].GroupVisible, "在 vip 的可用集里")
	assert.False(t, byID["default-only"].GroupVisible, "不在 vip 的可用集里")
	assert.True(t, byID["vip-retired"].GroupVisible,
		"在 vip 的可用集里 —— 已停用与分组可见是两个独立维度")
}

// TestGetCanvasCatalogEmptyAbilitiesTreatsAllVisible 锁住 fail-open:
// 分组在 abilities 里查不到任何启用模型时,一律按可见下发。
//
// 为什么这条重要:查询失败与「该分组确实一个模型都不能用」都得到空结果,
// 但含义相反。若判成「全部不可见」,画布会把每个条目当成已下线、直接从
// 模型下拉里剔掉 —— 一次瞬时 DB 故障或一处漏配的分组,就让所有客户端的
// 模型列表整体变空。错误方向应当是「看得到、点了被计费层拦住并给出明确
// 报错」,不是「模型凭空消失」。
func TestGetCanvasCatalogEmptyAbilitiesTreatsAllVisible(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "group-with-no-abilities")

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "some-model", DisplayName: "Some Model", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	// 故意不插任何 abilities 行 —— 模拟分组漏配 / 查询拿到空结果

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Models, 1)
	assert.True(t, resp.Models[0].GroupVisible,
		"分组查不到任何启用模型时必须 fail-open,否则客户端模型列表会整体变空")
}

// TestGetCanvasCatalogNoGroupContextTreatsAllVisible 取不到有效分组时(context key
// 缺省)一律按可见下发。宁可多给也不要因为读不到分组就把整份目录判成不可见 ——
// 那会让所有客户端的模型列表整体变空,是比漏过滤严重得多的故障。
func TestGetCanvasCatalogNoGroupContextTreatsAllVisible(t *testing.T) {
	router := setupCatalogGroupFilterTestDB(t, "")

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "any-model", DisplayName: "Any", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1,
	})
	// 故意不插任何 abilities 行:即便如此也不能把条目判成不可见
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Models, 1)
	assert.True(t, resp.Models[0].Enabled)
	assert.True(t, resp.Models[0].GroupVisible, "无分组信息时按可见处理")
}

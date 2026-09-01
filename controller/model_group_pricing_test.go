package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupModelMetaTestDB 给 model_meta 系列 handler 一个独立内存库。表清单取自
// model_list_test.go 的 setupModelListControllerTestDB(它已确认 RefreshPricing()
// 需要 User/Channel/Ability/Model/Vendor 才能跑通),外加本次新增的 ModelGroupPrice。
func setupModelMetaTestDB(t *testing.T) *gin.Engine {
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
		&model.User{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{},
		&model.ModelGroupPrice{},
	))

	router := gin.New()
	router.GET("/api/models/:id", GetModelMeta)
	router.POST("/api/models/", CreateModelMeta)
	router.PUT("/api/models/", UpdateModelMeta)
	return router
}

func doModelMetaJSON(t *testing.T, router *gin.Engine, method, path string, body any) (*httptest.ResponseRecorder, adminApiResponse) {
	t.Helper()
	return doJSON(t, router, method, path, body)
}

func TestCreateModelMetaPersistsGroupPrices(t *testing.T) {
	router := setupModelMetaTestDB(t)

	ratio := 1.5
	w, resp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gm-1",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "vip", "model_ratio": ratio},
		},
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, resp.Success, resp.Message)

	rows, err := model.GetModelGroupPrices("gm-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "vip", rows[0].GroupName)
	require.NotNil(t, rows[0].ModelRatio)
	assert.Equal(t, ratio, *rows[0].ModelRatio)
}

func TestCreateModelMetaRejectsGroupPricingOnNonExactNameRule(t *testing.T) {
	router := setupModelMetaTestDB(t)

	_, resp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gpt-4-",
		"name_rule":             model.NameRulePrefix,
		"group_pricing_enabled": true,
	})
	assert.False(t, resp.Success, "非精确匹配模型不应允许开启分组分别定价")

	rows, err := model.GetModelGroupPrices("gpt-4-")
	require.NoError(t, err)
	assert.Empty(t, rows, "被拒绝的请求不应留下任何分组价格行")
}

func TestUpdateModelMetaTogglingOffClearsGroupPrices(t *testing.T) {
	router := setupModelMetaTestDB(t)

	_, createResp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gm-2",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "default", "model_price": 0.1},
		},
	})
	require.True(t, createResp.Success, createResp.Message)
	var created model.Model
	require.NoError(t, json.Unmarshal(createResp.Data, &created))

	rows, err := model.GetModelGroupPrices("gm-2")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// 关闭开关,不带 group_prices 字段 —— saveModelGroupPrices 必须无条件清空,
	// 不能因为请求体没显式传空数组就把旧行留着。
	w, updateResp := doModelMetaJSON(t, router, "PUT", "/api/models/", map[string]any{
		"id":                    created.Id,
		"model_name":            "gm-2",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": false,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, updateResp.Success, updateResp.Message)

	rows, err = model.GetModelGroupPrices("gm-2")
	require.NoError(t, err)
	assert.Empty(t, rows, "关闭分别定价模式必须清空该模型的分组价格行")
}

func TestUpdateModelMetaStatusOnlyDoesNotTouchGroupPrices(t *testing.T) {
	router := setupModelMetaTestDB(t)

	_, createResp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gm-3",
		"name_rule":             model.NameRuleExact,
		"status":                1,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "default", "model_price": 0.2},
		},
	})
	require.True(t, createResp.Success, createResp.Message)
	var created model.Model
	require.NoError(t, json.Unmarshal(createResp.Data, &created))

	// status_only 请求体现实里只会带 {id, status} —— 不显式传 group_pricing_enabled,
	// 绑定后是 false 零值。这条路径必须完全跳过分组价格的读写,否则每次切换
	// 模型启停都会把配置好的分组价格清空。
	w, updateResp := doModelMetaJSON(t, router, "PUT", "/api/models/?status_only=true", map[string]any{
		"id":     created.Id,
		"status": 0,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, updateResp.Success, updateResp.Message)

	rows, err := model.GetModelGroupPrices("gm-3")
	require.NoError(t, err)
	require.Len(t, rows, 1, "status_only 更新不应清空分组价格")
	assert.Equal(t, "default", rows[0].GroupName)
}

func TestUpdateModelMetaRenameMigratesGroupPrices(t *testing.T) {
	router := setupModelMetaTestDB(t)

	_, createResp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gm-old-name",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "default", "model_price": 0.3},
		},
	})
	require.True(t, createResp.Success, createResp.Message)
	var created model.Model
	require.NoError(t, json.Unmarshal(createResp.Data, &created))

	w, updateResp := doModelMetaJSON(t, router, "PUT", "/api/models/", map[string]any{
		"id":                    created.Id,
		"model_name":            "gm-new-name",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "default", "model_price": 0.3},
		},
	})
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, updateResp.Success, updateResp.Message)

	oldRows, err := model.GetModelGroupPrices("gm-old-name")
	require.NoError(t, err)
	assert.Empty(t, oldRows, "改名后旧名字下不应残留孤儿分组价格")

	newRows, err := model.GetModelGroupPrices("gm-new-name")
	require.NoError(t, err)
	require.Len(t, newRows, 1)
}

func TestGetModelMetaEnrichesGroupPrices(t *testing.T) {
	router := setupModelMetaTestDB(t)

	_, createResp := doModelMetaJSON(t, router, "POST", "/api/models/", map[string]any{
		"model_name":            "gm-4",
		"name_rule":             model.NameRuleExact,
		"group_pricing_enabled": true,
		"group_prices": []map[string]any{
			{"group_name": "vip", "model_price": 0.4},
		},
	})
	require.True(t, createResp.Success, createResp.Message)
	var created model.Model
	require.NoError(t, json.Unmarshal(createResp.Data, &created))

	w, getResp := doModelMetaJSON(t, router, "GET", fmt.Sprintf("/api/models/%d", created.Id), nil)
	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, getResp.Success, getResp.Message)

	var fetched model.Model
	require.NoError(t, json.Unmarshal(getResp.Data, &fetched))
	require.Len(t, fetched.GroupPrices, 1)
	assert.Equal(t, "vip", fetched.GroupPrices[0].GroupName)
}

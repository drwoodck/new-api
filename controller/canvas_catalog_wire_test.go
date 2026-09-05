package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// canvasCatalogWireResponse 对应 GetCanvasCatalog 的裸 JSON 响应
// (非 ApiSuccess 信封,见 controller/canvas_catalog.go 的 c.Data)。
type canvasCatalogWireResponse struct {
	CatalogVersion int64 `json:"catalog_version"`
	Models         []struct {
		RemoteID    string  `json:"remote_id"`
		Description *string `json:"description"`
	} `json:"models"`
}

func setupCatalogWireTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := "file:canvas_wire_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CanvasCatalogModel{}, &model.Model{}))

	router := gin.New()
	router.GET("/api/canvas/catalog", GetCanvasCatalog)
	return router
}

func doGetCatalog(t *testing.T, router *gin.Engine) canvasCatalogWireResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "/api/canvas/catalog", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp canvasCatalogWireResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestCanvasCatalogWirePrefersModelMetaDescription 说明统一(2026-09-04 spec 3.7):
// models 表有说明时下发 models 的;没有时回退目录存量文字。
func TestCanvasCatalogWirePrefersModelMetaDescription(t *testing.T) {
	router := setupCatalogWireTestDB(t)

	require.NoError(t, model.DB.Create(&model.Model{
		ModelName: "meta-wins", Description: "来自模型管理页的说明", Status: 1,
	}).Error)
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "meta-wins", DisplayName: "Meta Wins",
		Contract: "relay_video_async_v1", Description: "目录存量说明",
	}).Error)
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "stored-only", DisplayName: "Stored Only",
		Contract: "relay_video_async_v1", Description: "存量回退",
	}).Error)

	resp := doGetCatalog(t, router)
	byRemote := map[string]*string{}
	for _, m := range resp.Models {
		byRemote[m.RemoteID] = m.Description
	}
	require.Contains(t, byRemote, "meta-wins")
	require.NotNil(t, byRemote["meta-wins"])
	assert.Equal(t, "来自模型管理页的说明", *byRemote["meta-wins"])
	require.NotNil(t, byRemote["stored-only"])
	assert.Equal(t, "存量回退", *byRemote["stored-only"])
}

// TestCanvasCatalogWireFallbackAfterMetaRemoved models 行说明清空后回退存量,
// 且整条目录不因 meta 查询失败/缺行而丢条目。
func TestCanvasCatalogWireFallbackAfterMetaRemoved(t *testing.T) {
	router := setupCatalogWireTestDB(t)

	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "fallback", DisplayName: "Fallback",
		Contract: "relay_video_async_v1", Description: "存量",
	}).Error)

	resp := doGetCatalog(t, router)
	require.Len(t, resp.Models, 1)
	require.NotNil(t, resp.Models[0].Description)
	assert.Equal(t, "存量", *resp.Models[0].Description)
}

// TestCanvasCatalogWireFallsBackWhenMetaDescriptionEmpty 锁住空说明不入 map 的
// 回退分支:models 行存在但 Description 为空串时,GetModelMetaDescriptionMap
// 不收录它,下发应回退目录存量说明,而不是把空串当有效说明顶掉存量。
func TestCanvasCatalogWireFallsBackWhenMetaDescriptionEmpty(t *testing.T) {
	router := setupCatalogWireTestDB(t)

	require.NoError(t, model.DB.Create(&model.Model{
		ModelName: "empty-meta", Description: "", Status: 1,
	}).Error)
	require.NoError(t, model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "empty-meta", DisplayName: "Empty Meta",
		Contract: "relay_video_async_v1", Description: "存量说明",
	}).Error)

	resp := doGetCatalog(t, router)
	require.Len(t, resp.Models, 1)
	require.NotNil(t, resp.Models[0].Description)
	assert.Equal(t, "存量说明", *resp.Models[0].Description)
}

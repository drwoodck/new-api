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

// catalogResponse 对应画布客户端的契约形状(Rust CatalogResponse):
// models 必须是 wire DTO(canvasCatalogWireModel),而不是存储层的
// CanvasCatalogModel —— 客户端要求 capabilities 是数组、param_schema 是对象。
type catalogResponse struct {
	CatalogVersion int                       `json:"catalog_version"`
	MinClient      string                    `json:"min_client"`
	SchemaVocab    int                       `json:"schema_vocab"`
	Provider       map[string]interface{}    `json:"provider"`
	Models         []canvasCatalogWireModel  `json:"models"`
}

func setupCatalogTestDB(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CanvasCatalogModel{}))

	router := gin.New()
	router.GET("/api/canvas/catalog", GetCanvasCatalog)
	return router
}

func TestGetCatalogEmpty(t *testing.T) {
	router := setupCatalogTestDB(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.CatalogVersion)
	assert.Equal(t, "0.1.17", resp.MinClient)
	assert.Equal(t, 1, resp.SchemaVocab)
	assert.Empty(t, resp.Models)
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
}

func TestGetCatalogETag304(t *testing.T) {
	router := setupCatalogTestDB(t)

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)
	etag := w1.Header().Get("ETag")
	require.NotEmpty(t, etag)

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req2.Header.Set("If-None-Match", etag)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusNotModified, w2.Code)
	assert.Empty(t, w2.Body.Bytes())
	// 304 也应携带协商缓存头
	assert.NotEmpty(t, w2.Header().Get("ETag"))
	assert.Equal(t, "private, no-store", w2.Header().Get("Cache-Control"))

	// 内容变化后,同一客户端必须拿到 200 + 新 ETag,而不是被旧 ETag 钉死在 304
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "fresh-model", DisplayName: "Fresh", Capabilities: "video_gen",
		Enabled: true, Contract: "relay_video_async_v1", RequiresVocab: 1,
	})
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req3.Header.Set("If-None-Match", etag)
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
	assert.NotEqual(t, etag, w3.Header().Get("ETag"))
}

func TestGetCatalogWithModels(t *testing.T) {
	router := setupCatalogTestDB(t)

	// Insert a test model
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID:    "test-model-1",
		DisplayName: "Test Model 1",
		Capabilities: "text,chat",
		Enabled:     true,
		Contract:    "standard",
		SortOrder:   0,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.CatalogVersion)
	assert.Equal(t, "0.1.17", resp.MinClient)
	assert.Equal(t, 1, len(resp.Models))
	assert.Equal(t, "test-model-1", resp.Models[0].RemoteID)
	assert.Equal(t, "Test Model 1", resp.Models[0].DisplayName)
}

// 锁住本次修复的核心:存储层是文本框字符串,线上必须是契约形状
// (capabilities 数组 / param_schema 对象),否则客户端整份目录解析失败。
// 同时锁住:停用条目必须出现在响应里(客户端软下线依赖它)。
func TestGetCatalogWireFormatContract(t *testing.T) {
	router := setupCatalogTestDB(t)

	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "video-a", DisplayName: "Video A",
		Capabilities: "video_gen,image_gen",
		Enabled:      true,
		Contract:     "relay_video_async_v1",
		ParamSchema:  `{"prompt":{"type":"string"},"duration":{"type":"integer"}}`,
		RequiresVocab: 1,
	})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "video-b", DisplayName: "Video B",
		Capabilities: `["video_gen"]`, // JSON 数组写法也要兼容
		Enabled:      false,           // 停用条目必须仍出现在响应中
		Contract:     "relay_video_async_v1",
		SchemaOverride: `{"endpoint_path":"/v1/videos"}`,
		ParamSchema:    `not-valid-json`, // 非法 JSON → null,不能弄垮响应
		RequiresVocab:  1,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp catalogResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Models, 2)

	a := resp.Models[0]
	assert.Equal(t, []string{"video_gen", "image_gen"}, a.Capabilities)
	assert.Equal(t, "video-a", a.RemoteID)
	assert.True(t, a.Enabled)
	var paramSchema map[string]interface{}
	require.NoError(t, json.Unmarshal(a.ParamSchema, &paramSchema))
	assert.Contains(t, paramSchema, "prompt")
	assert.Nil(t, a.SchemaOverride)

	b := resp.Models[1]
	assert.Equal(t, []string{"video_gen"}, b.Capabilities)
	assert.False(t, b.Enabled, "disabled entries must stay in the catalog (soft-offline contract)")
	assert.Nil(t, b.ParamSchema, "invalid param_schema JSON must degrade to null")
	require.NotNil(t, b.SchemaOverride)
	assert.JSONEq(t, `{"endpoint_path":"/v1/videos"}`, *b.SchemaOverride)
}

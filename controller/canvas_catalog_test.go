package controller

import (
	"strings"
	"fmt"
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
// boolPtr 是 CanvasCatalogModel.Enabled 变成 *bool 之后的测试辅助。
// 指针是必需的:该字段带 gorm default:true,bool 零值会被 GORM 当成
// 「未设置」而写入默认值,导致 enabled=false 根本插不进去。
func boolPtr(b bool) *bool { return &b }

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
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
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
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req3.Header.Set("If-None-Match", etag)
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
	assert.NotEqual(t, etag, w3.Header().Get("ETag"))
}

// TestGetCatalogETagWeakComparison 回归:If-None-Match 必须按 RFC 9110 §13.1.2
// 做**弱比较**,而不是字节强比较。
//
// 为什么要这条:本服务挂在 Cloudflare 后,边缘节点对响应做压缩/改写时会依 RFC 把
// 强 ETag 降级为弱 ETag(W/"...")再下发,客户端原样存下、原样回传。字节强比较下
// W/"x" != "x" 恒成立,协商缓存彻底失效 —— 每次目录请求都返回 200,而画布把 200
// 当作「内容变了」(见 src-tauri/src/sync/catalog.rs 的 check_catalog_updates),
// 于是反复判定「有更新可用」,即使点过应用更新,下一轮轮询又会弹回来。
func TestGetCatalogETagWeakComparison(t *testing.T) {
	router := setupCatalogTestDB(t)

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)
	etag := w1.Header().Get("ETag")
	require.NotEmpty(t, etag)
	// 源站产出的是强 ETag;W/ 是中间层加的
	require.NotContains(t, etag, "W/")

	// 1. 中间层加了 W/ 前缀后原样回传 —— 必须仍判命中
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req2.Header.Set("If-None-Match", "W/"+etag)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusNotModified, w2.Code)

	// 2. 逗号分隔的多值列表里命中任一个即可
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req3.Header.Set("If-None-Match", `"stale-etag", W/`+etag)
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusNotModified, w3.Code)

	// 3. 通配符 * 表示「只要资源存在就命中」
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req4.Header.Set("If-None-Match", "*")
	router.ServeHTTP(w4, req4)
	assert.Equal(t, http.StatusNotModified, w4.Code)

	// 4. 内容真的变了,即使是弱 ETag 也不能再判命中
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "weak-cmp-model", DisplayName: "Weak", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	})
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest("GET", "/api/canvas/catalog", nil)
	req5.Header.Set("If-None-Match", "W/"+etag)
	router.ServeHTTP(w5, req5)
	assert.Equal(t, http.StatusOK, w5.Code)
}

func TestGetCatalogWithModels(t *testing.T) {
	router := setupCatalogTestDB(t)

	// Insert a test model
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID:    "test-model-1",
		DisplayName: "Test Model 1",
		Capabilities: "text,chat",
		Enabled:     boolPtr(true),
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
		Enabled:      boolPtr(true),
		Contract:     "relay_video_async_v1",
		ParamSchema:  `{"prompt":{"type":"string"},"duration":{"type":"integer"}}`,
		RequiresVocab: 1,
	})
	model.DB.Create(&model.CanvasCatalogModel{
		RemoteID: "video-b", DisplayName: "Video B",
		Capabilities: `["video_gen"]`, // JSON 数组写法也要兼容
		Enabled:      boolPtr(false),           // 停用条目必须仍出现在响应中
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

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

type catalogResponse struct {
	CatalogVersion int                        `json:"catalog_version"`
	MinClient      string                    `json:"min_client"`
	SchemaVocab    int                        `json:"schema_vocab"`
	Provider       map[string]interface{}    `json:"provider"`
	Models         []model.CanvasCatalogModel `json:"models"`
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

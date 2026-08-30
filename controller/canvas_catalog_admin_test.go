package controller

import (
	"strings"
	"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type adminApiResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func setupCatalogAdminTestDB(t *testing.T) *gin.Engine {
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
	router.GET("/api/canvas/admin/models", GetAllCanvasCatalogModelsAdmin)
	router.GET("/api/canvas/admin/models/:id", GetCanvasCatalogModelAdmin)
	router.POST("/api/canvas/admin/models", CreateCanvasCatalogModelAdmin)
	router.PUT("/api/canvas/admin/models", UpdateCanvasCatalogModelAdmin)
	router.DELETE("/api/canvas/admin/models/:id", DeleteCanvasCatalogModelAdmin)
	return router
}

func doJSON(t *testing.T, router *gin.Engine, method, path string, body any) (*httptest.ResponseRecorder, adminApiResponse) {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(b)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp adminApiResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return w, resp
}

func TestCreateCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	w, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:    "kungai-seedance-2",
		DisplayName: "seedance-2.0",
		Capabilities: "video_gen",
		Contract:    "relay_video_async_v1",
		Enabled:     boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	var created model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &created))
	assert.NotZero(t, created.Id)
	assert.Equal(t, "kungai-seedance-2", created.RemoteID)
}

func TestCreateCanvasCatalogModelAdminRejectsMissingFields(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	_, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		DisplayName: "no remote id",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestCreateCanvasCatalogModelAdminRejectsDuplicateRemoteID(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	first := model.CanvasCatalogModel{RemoteID: "dup-id", DisplayName: "First", Contract: "relay_video_async_v1"}
	require.NoError(t, first.Insert())

	_, resp := doJSON(t, router, "POST", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:    "dup-id",
		DisplayName: "Second",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestUpdateCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{RemoteID: "m1", DisplayName: "Old Name", Contract: "relay_video_async_v1"}
	require.NoError(t, existing.Insert())

	w, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", model.CanvasCatalogModel{
		Id:          existing.Id,
		RemoteID:    "m1",
		DisplayName: "New Name",
		Contract:    "relay_video_async_v1",
		Enabled:     boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	updated, err := model.GetCanvasCatalogModelByID(existing.Id)
	require.NoError(t, err)
	assert.Equal(t, "New Name", updated.DisplayName)
}

func TestUpdateCanvasCatalogModelAdminRejectsMissingID(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	_, resp := doJSON(t, router, "PUT", "/api/canvas/admin/models", model.CanvasCatalogModel{
		RemoteID:    "m1",
		DisplayName: "No ID",
		Contract:    "relay_video_async_v1",
	})
	assert.False(t, resp.Success)
}

func TestDeleteCanvasCatalogModelAdmin(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	existing := model.CanvasCatalogModel{RemoteID: "to-delete", DisplayName: "Delete Me", Contract: "relay_video_async_v1"}
	require.NoError(t, existing.Insert())

	w, resp := doJSON(t, router, "DELETE", "/api/canvas/admin/models/"+strconv.Itoa(existing.Id), nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	_, err := model.GetCanvasCatalogModelByID(existing.Id)
	assert.Error(t, err)
}

func TestGetAllCanvasCatalogModelsAdminIncludesDisabled(t *testing.T) {
	router := setupCatalogAdminTestDB(t)

	enabled := model.CanvasCatalogModel{RemoteID: "enabled-1", DisplayName: "Enabled", Contract: "c", Enabled: boolPtr(true)}
	disabled := model.CanvasCatalogModel{RemoteID: "disabled-1", DisplayName: "Disabled", Contract: "c", Enabled: boolPtr(false)}
	require.NoError(t, enabled.Insert())
	require.NoError(t, disabled.Insert())

	w, resp := doJSON(t, router, "GET", "/api/canvas/admin/models", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, resp.Success)

	var all []model.CanvasCatalogModel
	require.NoError(t, json.Unmarshal(resp.Data, &all))
	assert.Len(t, all, 2)
}

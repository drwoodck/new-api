package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCanvasCatalogMetaAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/canvas/admin/meta", GetCanvasCatalogMetaAdmin)

	req, _ := http.NewRequest(http.MethodGet, "/api/canvas/admin/meta", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			SupportedContracts   []string          `json:"supported_contracts"`
			CapabilityToContract map[string]string `json:"capability_to_contract"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Contains(t, resp.Data.SupportedContracts, "relay_video_async_v1")
	assert.Contains(t, resp.Data.SupportedContracts, "relay_image_async_v1")
	assert.Equal(t, "relay_video_async_v1", resp.Data.CapabilityToContract["video_gen"])
	assert.Equal(t, "relay_image_async_v1", resp.Data.CapabilityToContract["image_gen"])
}

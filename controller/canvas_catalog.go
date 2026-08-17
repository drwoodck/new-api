package controller

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetCanvasCatalog(c *gin.Context) {
	models, version, err := model.GetCanvasCatalog(nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	baseURL := os.Getenv("RELAY_BASE_URL")
	if baseURL == "" {
		baseURL = "https://your-relay.com"
	}

	response := gin.H{
		"catalog_version": version,
		"min_client":      "0.1.17",
		"schema_vocab":    1,
		"provider": gin.H{
			"base_url": baseURL,
			"kind":     "openai_compatible",
		},
		"models": models,
	}

	bodyBytes, err := json.Marshal(response)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to encode catalog: " + err.Error()})
		return
	}
	etag := fmt.Sprintf(`"%x"`, md5.Sum(bodyBytes))

	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, response)
}

package controller

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// canvasCatalogWireModel 是画布客户端契约(docs/superpowers/specs
// 2026-08-17-remote-model-catalog-design.md 第 1 节)的线上格式,与
// model.CanvasCatalogModel(管理端存储格式)刻意分开:存储层 capabilities /
// param_schema / schema_override 是后台文本框的原样字符串,而客户端要求
// capabilities 是数组、param_schema 是 JSON 对象。直接把 GORM 模型 marshal
// 出去,客户端整份目录都会解析失败(reqwest "error decoding response body"),
// 必须在这里做一次存储格式 → 契约格式的转换。
type canvasCatalogWireModel struct {
	RemoteID       string          `json:"remote_id"`
	DisplayName    string          `json:"display_name"`
	Capabilities   []string        `json:"capabilities"`
	Enabled        bool            `json:"enabled"`
	Description    *string         `json:"description"`
	Pricing        *string         `json:"pricing"`
	Limitations    *string         `json:"limitations"`
	Contract       string          `json:"contract"`
	ParamSchema    json.RawMessage `json:"param_schema"`
	SchemaOverride *string         `json:"schema_override"`
	RequiresVocab  int             `json:"requires_vocab"`
}

// parseCapabilities 兼容管理员在文本框里的几种写法:JSON 数组
// `["video_gen","image_gen"]`、逗号分隔 `video_gen,image_gen`,以及顿号 /
// 分号 / 空白分隔。解析结果为空时返回空数组而非 null,保持契约形状稳定。
func parseCapabilities(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && arr != nil {
			return arr
		}
	}
	out := make([]string, 0, 4)
	for _, p := range strings.FieldsFunc(trimmed, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '\n', '\t', ' ':
			return true
		}
		return false
	}) {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func toWireModel(m *model.CanvasCatalogModel) canvasCatalogWireModel {
	w := canvasCatalogWireModel{
		RemoteID:      m.RemoteID,
		DisplayName:   m.DisplayName,
		Capabilities:  parseCapabilities(m.Capabilities),
		Enabled:       m.Enabled,
		Contract:      m.Contract,
		RequiresVocab: m.RequiresVocab,
	}
	if m.Description != "" {
		w.Description = &m.Description
	}
	if m.Pricing != "" {
		w.Pricing = &m.Pricing
	}
	if m.Limitations != "" {
		w.Limitations = &m.Limitations
	}
	if m.SchemaOverride != "" {
		// 契约即 JSON 文本(客户端拿到后原样做 ResolvedProfile 反序列化),
		// 存储格式一致,原样透传。
		w.SchemaOverride = &m.SchemaOverride
	}
	if s := strings.TrimSpace(m.ParamSchema); s != "" && json.Valid([]byte(s)) {
		// 作为 JSON 对象内联下发;非法 JSON 保持 null —— param_schema 仅供
		// UI 渲染表单,不能因为它弄垮整份目录。
		w.ParamSchema = json.RawMessage(s)
	}
	return w
}

func GetCanvasCatalog(c *gin.Context) {
	rows, version, err := model.GetCanvasCatalog(nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	baseURL := os.Getenv("RELAY_BASE_URL")
	if baseURL == "" {
		baseURL = "https://your-relay.com"
	}

	models := make([]canvasCatalogWireModel, 0, len(rows))
	for i := range rows {
		models = append(models, toWireModel(&rows[i]))
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

	// ETag / Cache-Control 对 304 与 200 一并给出:304 按规范也应携带 ETag,
	// 便于客户端与中间层正确处理协商缓存。
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-store")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	// 写入的就是参与 etag 计算的那份 body,保证协商缓存与响应内容严格一致
	c.Data(http.StatusOK, "application/json; charset=utf-8", bodyBytes)
}

package controller

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

	// GroupVisible 表示该条目是否在调用者分组的可用模型集里(派生自 abilities,
	// 不是目录自己的列)。
	//
	// 为什么是独立字段、而不是复用 Enabled:画布把 Enabled 直接写进本地
	// models.enabled 列(sync/catalog.rs 的 apply_entry),而那一列**同时**是
	// 用户自己的模型勾选开关(ProviderDetailPanel 的启用/停用),且冲突时
	// 无条件覆写(model_repo.rs 的 `enabled = excluded.enabled`)。从目录侧
	// 写 Enabled 表达「分组不可见」,会在每次同步静默清掉用户的选择。
	//
	// 也不能用「从响应里删掉行」来表达:客户端靠「条目还在但 enabled=false」
	// 区分「已下线」与「已删除」,删行会让这个区分消失。
	//
	// 画布当前还不读这个字段 —— serde 忽略未知字段,所以下发它是向后兼容的,
	// 消费留给后续任务。
	GroupVisible bool `json:"group_visible"`
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

// toWireModel 把存储格式转成客户端契约格式。
//
// groupModels 是调用者分组的可用模型集(nil 表示「不做分组判定」——
// 取不到有效分组时一律按可见处理,宁可多给也不要把整份目录判成不可见)。
func toWireModel(m *model.CanvasCatalogModel, groupModels map[string]struct{}) canvasCatalogWireModel {
	w := canvasCatalogWireModel{
		RemoteID:      m.RemoteID,
		DisplayName:   m.DisplayName,
		Capabilities:  parseCapabilities(m.Capabilities),
		Enabled:       m.IsEnabled(),
		Contract:      m.Contract,
		RequiresVocab: m.RequiresVocab,
		// nil 集合 = 无分组信息 = 不降级任何条目
		GroupVisible: groupModels == nil,
	}
	if groupModels != nil {
		_, w.GroupVisible = groupModels[m.RemoteID]
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
	// TokenAuthReadOnly 已经算好并写入了有效分组(token.Group 覆盖 userCache.Group,
	// 与完整 TokenAuth 同一优先级),这里直接读,不重新查库。
	effectiveGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

	// groupModels 为 nil 表示「没有分组信息」—— 一律按可见下发。
	//
	// 三种情况都必须落到 nil(fail-open),而不是空集合:
	//   1. 取不到有效分组(context key 缺省)
	//   2. abilities 查询失败 —— **这一条是关键**。查询失败与「该分组确实
	//      一个模型都不能用」都会得到空结果,但含义相反。判成空集合会让
	//      每个条目 group_visible=false,而画布把它当「已下线」直接从模型
	//      下拉里剔掉 —— 一次瞬时 DB 故障就让所有客户端的模型列表变空。
	//      宁可多给(用户点了在计费层被拦)也不要整体变空。
	//
	// 只有查询**成功且返回了非空集合**时才收窄可见性。
	var groupModels map[string]struct{}
	if effectiveGroup != "" {
		// 无缓存的直接 DB 查询,但 abilities 复合主键以 Group 为首列,
		// distinct 模型集合很小,目录端点当前调用量级下可接受。
		enabled, err := model.GetGroupEnabledModels(effectiveGroup)
		switch {
		case err != nil:
			common.SysError(fmt.Sprintf(
				"读取分组 %s 的可用模型失败,本次目录按全部可见下发: %v", effectiveGroup, err))
		case len(enabled) == 0:
			// 空结果在查询成功的前提下是真实状态,但同样按可见处理 ——
			// 分组配置漏了会让用户什么都看不到,而错误方向应当是「看得到、
			// 点了被计费层拦住并给出明确报错」,不是「模型凭空消失」。
			common.SysLog(fmt.Sprintf(
				"分组 %s 在 abilities 里没有任何启用模型,本次目录按全部可见下发", effectiveGroup))
		default:
			groupModels = make(map[string]struct{}, len(enabled))
			for _, name := range enabled {
				groupModels[name] = struct{}{}
			}
		}
	}

	// 模型层不参与分组过滤 —— 可见性在下面的 wire 转换里作为独立字段附加。
	rows, version, err := model.GetCanvasCatalog()
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
		models = append(models, toWireModel(&rows[i], groupModels))
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

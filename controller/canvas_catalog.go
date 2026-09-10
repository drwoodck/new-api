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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

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
	RemoteID     string   `json:"remote_id"`
	DisplayName  string   `json:"display_name"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Description  *string  `json:"description"`
	Pricing      *string  `json:"pricing"`
	// PricingSource 标记 pricing 文案来源:auto = 按计费真源自动生成,
	// custom = 管理员手填覆盖。空 = 旧响应(本字段引入前的缓存)不下发,
	// 客户端 serde 忽略未知字段,向后兼容。
	PricingSource  string          `json:"pricing_source,omitempty"`
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

	// GroupPrice 是已经按调用者分组算好的价格 —— 只下发调用者自己那一个分组的
	// 数字,不下发其它分组的价格(用户的分组只能看到自己分组的价)。
	// nil 表示"没有价格信息":取不到有效分组(fail-open,与 GroupVisible 同一
	// 处理原则)、或分别定价模式下这个分组没有配置价格。**刻意不用 0 表示
	// 不可用**——0 会被画布当"免费"处理并放过余额闸门,而"没有价格信息"与
	// "免费"是两件完全不同的事。
	GroupPrice *canvasGroupPrice `json:"group_price"`
}

// canvasGroupPrice 字段命名与形状对齐画布已有的 relay/pricing.rs 的
// PricingItem,让画布侧能直接复用已有的预估公式(src/lib/costEstimate.ts),
// 不需要为这个新字段单独写一套。
type canvasGroupPrice struct {
	// QuotaType: 0 = 按 token 倍率计费,1 = 按次/按量固定价。
	QuotaType        int      `json:"quota_type"`
	ModelPrice       float64  `json:"model_price"`
	ModelRatio       float64  `json:"model_ratio"`
	CompletionRatio  float64  `json:"completion_ratio"`
	VideoSecondPrice *float64 `json:"video_second_price,omitempty"`
	// PriceTiers 档位计费的档表(统一模式为已 ×分组倍率的终价、分别定价为
	// 行内终价 —— 与 ModelPrice 同语义:目录下发"已按调用者分组算好的数字",
	// 画布侧不再叠乘)。非 nil 时画布以档表为准选档计价,
	// ModelPrice/VideoSecondPrice 等旧字段不再参与。
	PriceTiers *types.PriceTierList `json:"price_tiers,omitempty"`
	// GroupRatioApplied:统一模式下是该分组的 GroupRatio;分别定价模式下恒为 1
	// (分别定价的数字本身就是最终价,不再叠乘倍率——ResolveGroupPrice 的既有约定)。
	GroupRatioApplied float64 `json:"group_ratio_applied"`
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

// resolveCanvasGroupPrice 算出某个 remote_id(= 计费用的字面模型名,两者
// 本就是同一个字符串,已核实)在给定分组下的价格,供目录下发使用。
// group 为空表示"没有分组信息"(取不到有效分组),此时 fail-open 返回 nil ——
// 与 GroupVisible 同一处理原则:宁可不下发价格,也不能下发一个错误的 0。
// groupPrices 是调用方已按模型预载好的分组价格行(本模型各分组 → 行,可能为空
// map)——目录下发循环零逐条点查 model_group_prices(见 GetCanvasCatalog)。
func resolveCanvasGroupPrice(remoteID, group string, groupPrices map[string]model.ModelGroupPrice) *canvasGroupPrice {
	if group == "" {
		return nil
	}
	groupPricingEnabled := model.IsGroupPricingEnabled(remoteID)
	// 档位计费最优先：模型配了档表(全局或行内)时下发档表(原价)。
	// 与 ModelPriceHelperPerCall 的档位分支同序 —— 优先级矩阵见
	// model/model_group_price.go 的 ResolveTierPrice 注释。
	// 用预载行版:分别定价模式只认预载的行内档表,统一模式走全局档表(内存缓存),
	// 都不再查库。
	if tierTable := model.ResolveGroupTierTableFromPreloaded(remoteID, group, groupPricingEnabled, groupPrices); tierTable != nil {
		return &canvasGroupPrice{
			QuotaType:         1,
			PriceTiers:        tierTable.PriceTiers,
			GroupRatioApplied: tierTable.GroupRatioApplied,
		}
	}
	// 分别定价模式的行内秒价:行是唯一价格权威,与 ModelPriceHelperPerCall
	// 的秒价分支同序 —— 行内 VideoSecondPrice(nil/≤0 视为未启用)是最终价,
	// 不叠乘分组倍率(GroupRatioApplied=1);行内没有秒价列时也不回退全局秒价
	// (计费侧同样不回退,直接落到行内标量),否则目录会下发一个计费根本
	// 不会收取的按秒价。
	if groupPricingEnabled {
		row, ok := groupPrices[group]
		if !ok {
			// 该分组没有价格行 = 分组不可用(与计费侧同判),不下发价格。
			return nil
		}
		if secondPrice, ok := model.ResolveVideoSecondPriceForGroup(remoteID, group, true, &row); ok {
			return &canvasGroupPrice{
				QuotaType:         1,
				ModelPrice:        secondPrice,
				VideoSecondPrice:  row.VideoSecondPrice,
				GroupRatioApplied: 1,
			}
		}
		resolved := model.ResolveGroupPriceFromRow(&row)
		if !resolved.Available {
			return nil
		}
		return &canvasGroupPrice{
			QuotaType:         resolved.QuotaType,
			ModelPrice:        resolved.ModelPrice,
			ModelRatio:        resolved.ModelRatio,
			CompletionRatio:   resolved.CompletionRatio,
			GroupRatioApplied: resolved.GroupRatioApplied,
		}
	}
	// 统一模式的视频按秒计费。口径修复(2026-09-04 spec 3.3):与档表/按次
	// 同口径,下发**已乘分组倍率的终价**——旧版下发原价+分离倍率,固定
	// ratio=1 的客户端会低估(0.1×3 分组实收 0.3,客户端按 0.1 估算)。
	if secondPrice, ok := ratio_setting.GetVideoSecondPrice(remoteID); ok {
		groupRatio := ratio_setting.GetGroupRatio(group)
		finalPrice := secondPrice * groupRatio
		return &canvasGroupPrice{
			QuotaType:         1,
			ModelPrice:        finalPrice,
			VideoSecondPrice:  &finalPrice,
			GroupRatioApplied: groupRatio,
		}
	}
	resolved, err := model.ResolveGroupPrice(remoteID, group)
	if err != nil {
		// 与 abilities 查询失败同一处理:查询失败不等于"这个分组真的不可用",
		// 判成不可用会让画布把它当成免费/不可用来源,宁可不下发。
		common.SysError(fmt.Sprintf("解析模型 %s 在分组 %s 下的价格失败,本次目录不下发该条目的价格: %v", remoteID, group, err))
		return nil
	}
	if !resolved.Available {
		return nil
	}
	return &canvasGroupPrice{
		QuotaType:         resolved.QuotaType,
		ModelPrice:        resolved.ModelPrice,
		ModelRatio:        resolved.ModelRatio,
		CompletionRatio:   resolved.CompletionRatio,
		GroupRatioApplied: resolved.GroupRatioApplied,
	}
}

// toWireModel 把存储格式转成客户端契约格式。
//
// groupModels 是调用者分组的可用模型集(nil 表示「不做分组判定」——
// 取不到有效分组时一律按可见处理,宁可多给也不要把整份目录判成不可见)。
// group 是调用者的有效分组字符串(空串表示"没有分组信息"),用于算
// GroupPrice —— 与 groupModels 表达的是同一次"有没有分组信息"判断,
// 两个参数分开传是因为 GroupVisible 只需要集合、GroupPrice 的计算需要
// 分组名字符串本身。
// groupPrices 是调用方按模型预载的分组价格行(本模型的 group → 行,nil 表示
// 该模型没有预载到任何行)——目录下发循环零逐条点查。
func toWireModel(m *model.CanvasCatalogModel, groupModels map[string]struct{}, group string, metaDescriptions map[string]string, metaDisplayNames map[string]string, metadataMap map[string]*model.ModelMetadata, groupPrices map[string]model.ModelGroupPrice) canvasCatalogWireModel {
	w := canvasCatalogWireModel{
		RemoteID:      m.RemoteID,
		DisplayName:   m.DisplayName,
		Capabilities:  parseCapabilities(m.Capabilities),
		Enabled:       m.IsEnabled(),
		Contract:      m.Contract,
		RequiresVocab: m.RequiresVocab,
		// nil 集合 = 无分组信息 = 不降级任何条目
		GroupVisible: groupModels == nil,
		GroupPrice:   resolveCanvasGroupPrice(m.RemoteID, group, groupPrices),
	}
	if groupModels != nil {
		_, w.GroupVisible = groupModels[m.RemoteID]
	}
	// 显示名称统一真源：优先 models 表的 display_name，没有该行或显示名称为空时
	// 回退目录的 display_name（存量兼容）
	if displayName := metaDisplayNames[m.RemoteID]; displayName != "" {
		w.DisplayName = displayName
	}
	// 说明统一真源(2026-09-04 spec 3.7):优先 models 表的说明,没有该行或
	// 说明为空时回退目录存量文字(列已冻结,仅旧数据兜底)。
	if desc := metaDescriptions[m.RemoteID]; desc != "" {
		w.Description = &desc
	} else if m.Description != "" {
		w.Description = &m.Description
	}
	if m.Pricing != "" {
		w.Pricing = &m.Pricing
		w.PricingSource = "custom"
	} else {
		// 自动文案(2026-09-04 spec 3.3):与计费同一套解析,管理端未手填时
		// 由后端生成,保证「目录宣传 = 实际计费」。走 Preloaded 版,复用
		// GetCanvasCatalog 预载的分组价格行,循环内零逐条点查。
		row, ok := groupPrices[group]
		var rowPtr *model.ModelGroupPrice
		if ok {
			rowPtr = &row // map 值是值类型,取本地拷贝的指针,不指向 map 内部
		}
		if summary := model.GeneratePricingSummaryPreloaded(m.RemoteID, group, model.IsGroupPricingEnabled(m.RemoteID), rowPtr); summary != "" {
			w.Pricing = &summary
			w.PricingSource = "auto"
		}
	}
	if m.Limitations != "" {
		w.Limitations = &m.Limitations
	}
	if m.SchemaOverride != "" {
		// 契约即 JSON 文本(客户端拿到后原样做 ResolvedProfile 反序列化),
		// 存储格式一致,原样透传。
		w.SchemaOverride = &m.SchemaOverride
	}
	// 填充 param_schema: 优先从 model_metadata 表,回退到目录存量列
	if meta, ok := metadataMap[m.RemoteID]; ok && meta.ParamSchema != nil {
		// model_metadata 表的 param_schema 已是 JSON 字符串,直接解析为对象
		if json.Valid([]byte(*meta.ParamSchema)) {
			w.ParamSchema = json.RawMessage(*meta.ParamSchema)
		} else {
			common.SysLog(fmt.Sprintf("Failed to parse param_schema for %s: invalid JSON", m.RemoteID))
		}
	} else if s := strings.TrimSpace(m.ParamSchema); s != "" && json.Valid([]byte(s)) {
		// 回退到目录存量列(兼容旧数据)
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

	// 批量取 models 表说明和显示名称;查询失败按 fail-open 处理(回退目录存量文字),
	// 不让一次 meta 查询故障弄垮整份目录 —— 与分组可见性的 fail-open 同原则。
	remoteIDs := make([]string, 0, len(rows))
	for i := range rows {
		remoteIDs = append(remoteIDs, rows[i].RemoteID)
	}
	metaDescriptions, err := model.GetModelMetaDescriptionMap(remoteIDs)
	if err != nil {
		common.SysError(fmt.Sprintf("读取模型说明失败,目录说明回退存量文字: %v", err))
		metaDescriptions = map[string]string{}
	}
	metaDisplayNames, err := model.GetModelMetaDisplayNameMap(remoteIDs)
	if err != nil {
		common.SysError(fmt.Sprintf("读取模型显示名称失败,目录显示名称回退 remote_id: %v", err))
		metaDisplayNames = map[string]string{}
	}

	// 分组过滤后的可见 ID 列表（只查询该分组可见模型的元数据）
	visibleRemoteIDs := filterVisibleRemoteIDs(remoteIDs, groupModels)

	// 预载元数据
	metadataMap, err := model.GetModelMetadataMap(visibleRemoteIDs)
	if err != nil {
		// Fail-open: 查询失败按空 map 处理，不影响目录下发
		common.SysLog(fmt.Sprintf("GetModelMetadataMap failed: %v", err))
		metadataMap = make(map[string]*model.ModelMetadata)
	}

	// 预载全部分组价格行(分别定价模式的逐分组价),目录下发循环零逐条点查 ——
	// 与 metaDescriptions 同一 fail-open 约定:查询失败按空处理(SysError),
	// 让下游自然回落「无行 = 分组不可用 / 未定价」,不让一次查询故障弄垮整份目录。
	allGroupPrices, err := model.GetAllModelGroupPrices()
	if err != nil {
		common.SysError(fmt.Sprintf("读取分组价格失败,目录价格按未配置处理: %v", err))
		allGroupPrices = map[string]map[string]model.ModelGroupPrice{}
	}

	baseURL := os.Getenv("RELAY_BASE_URL")
	if baseURL == "" {
		baseURL = "https://your-relay.com"
	}

	models := make([]canvasCatalogWireModel, 0, len(rows))
	for i := range rows {
		models = append(models, toWireModel(&rows[i], groupModels, effectiveGroup, metaDescriptions, metaDisplayNames, metadataMap, allGroupPrices[rows[i].RemoteID]))
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

// filterVisibleRemoteIDs 返回该分组可见的 remote_id 子集
// groupModels 为 nil 时表示无分组信息 → Fail-open，全部可见
func filterVisibleRemoteIDs(allIDs []string, groupModels map[string]struct{}) []string {
	if groupModels == nil {
		return allIDs
	}
	visible := make([]string, 0, len(allIDs))
	for _, id := range allIDs {
		if _, ok := groupModels[id]; ok {
			visible = append(visible, id)
		}
	}
	return visible
}

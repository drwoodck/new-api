package controller

import (
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// deriveContractIfEmpty 按 capabilities 的第一个已知 capability 推出 contract,
// 但只在管理员没填时才推 —— 手填永远优先(逃生舱,新契约/新 capability 上线前
// 靠人先兜底)。取不到已知映射就原样留空,交给既有的「contract 不能为空」校验
// 报错,而不是猜一个可能错的值。
func deriveContractIfEmpty(m *model.CanvasCatalogModel) {
	if m.Contract != "" {
		return
	}
	for _, cap := range parseCapabilities(m.Capabilities) {
		if contract, ok := constant.ContractForCapability(cap); ok {
			m.Contract = contract
			return
		}
	}
}

// GetAllCanvasCatalogModelsAdmin 管理端获取全部目录条目(含禁用),不分页。
func GetAllCanvasCatalogModelsAdmin(c *gin.Context) {
	models, err := model.GetAllCanvasCatalogModelsAdmin()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, models)
}

// GetCanvasCatalogModelAdmin 按 ID 获取单条目录条目。
func GetCanvasCatalogModelAdmin(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	m, err := model.GetCanvasCatalogModelByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 填充 models 表的说明和显示名称（2026-09-09）
	metaDescriptions, err := model.GetModelMetaDescriptionMap([]string{m.RemoteID})
	if err == nil && len(metaDescriptions) > 0 {
		if desc, ok := metaDescriptions[m.RemoteID]; ok && desc != "" {
			m.Description = desc
		}
	}
	metaDisplayNames, err := model.GetModelMetaDisplayNameMap([]string{m.RemoteID})
	if err == nil && len(metaDisplayNames) > 0 {
		if displayName, ok := metaDisplayNames[m.RemoteID]; ok && displayName != "" {
			m.DisplayName = displayName
		}
	}

	common.ApiSuccess(c, m)
}

// CreateCanvasCatalogModelAdmin 新建目录条目。
func CreateCanvasCatalogModelAdmin(c *gin.Context) {
	var m model.CanvasCatalogModel
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.RemoteID == "" {
		common.ApiErrorMsg(c, "remote_id 不能为空")
		return
	}
	if m.DisplayName == "" {
		common.ApiErrorMsg(c, "display_name 不能为空")
		return
	}
	// 说明真源在 models 表(2026-09-04 spec 3.7):目录侧不接受 description 入参,
	// 旧客户端带来的值一律丢弃,防止绕过前端重新制造第二份说明。
	m.Description = ""
	deriveContractIfEmpty(&m)
	if m.Contract == "" {
		common.ApiErrorMsg(c, "contract 不能为空,且无法从 capabilities 推导(未知 capability),请手填")
		return
	}
	if dup, err := model.IsCanvasCatalogRemoteIDDuplicated(0, m.RemoteID); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "remote_id 已存在")
		return
	}

	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &m)
}

// UpdateCanvasCatalogModelAdmin 更新目录条目。
func UpdateCanvasCatalogModelAdmin(c *gin.Context) {
	var m model.CanvasCatalogModel
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorMsg(c, "缺少目录条目 ID")
		return
	}
	if m.RemoteID == "" {
		common.ApiErrorMsg(c, "remote_id 不能为空")
		return
	}
	if m.DisplayName == "" {
		common.ApiErrorMsg(c, "display_name 不能为空")
		return
	}
	deriveContractIfEmpty(&m)
	if m.Contract == "" {
		common.ApiErrorMsg(c, "contract 不能为空")
		return
	}
	if dup, err := model.IsCanvasCatalogRemoteIDDuplicated(m.Id, m.RemoteID); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "remote_id 已存在")
		return
	}

	if err := m.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &m)
}

// GetModelMappingInconsistenciesAdmin 列出同一对外模型名在不同启用渠道上
// 映射到不同上游目标的情况,供管理后台展示告警。不阻断任何保存操作 ——
// 见 model.CheckModelMappingConsistency 的注释:多渠道映射不同是合法的
// 负载均衡场景,这里只是给运营方一个"看起来像手误"的提示。
func GetModelMappingInconsistenciesAdmin(c *gin.Context) {
	inconsistencies, err := model.CheckModelMappingConsistency()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, inconsistencies)
}

// DeleteCanvasCatalogModelAdmin 删除(软删除)目录条目。
func DeleteCanvasCatalogModelAdmin(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteCanvasCatalogModel(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// canvasCatalogOverviewRow 是目录总览视图的一行:一个中转站已启用模型,
// 关联它(可能没有的)models 行与(可能没有的)canvas_catalog_model 行。
type canvasCatalogOverviewRow struct {
	ModelName           string                            `json:"model_name"`
	ModelID             int                               `json:"model_id"`
	CatalogID           int                               `json:"catalog_id"`
	DisplayName         string                            `json:"display_name"`
	Contract            string                            `json:"contract"`
	Capabilities        string                            `json:"capabilities"`
	CatalogEnabled      bool                              `json:"catalog_enabled"`
	ModelStatus         int                               `json:"model_status"`
	Ready               bool                              `json:"ready"`
	GroupPricingEnabled bool                              `json:"group_pricing_enabled"`
	GroupPrices         []canvasCatalogOverviewGroupPrice `json:"group_prices"`
}

type canvasCatalogOverviewGroupPrice struct {
	GroupName string `json:"group_name"`
	QuotaType int    `json:"quota_type"`
	Price     *float64 `json:"price"`
	// PriceTiers 档位计费的档表（原价）。非 nil 时该分组的计费以档表为准，
	// 展示层据此显示"档表 ×N"徽章而非单值价格。
	PriceTiers *types.PriceTierList `json:"price_tiers,omitempty"`
}

// buildOverviewGroupPrices 对一个模型算出它在**全部**已知分组上的价格。
// 全局倍率/价格与分组价格表都已由调用方一次性拉好(见 GetCanvasCatalogOverviewAdmin),
// 这里只做纯内存换算 —— 不要在这个函数里查库,否则 N 个模型 × M 个分组就是
// N×M 次查询。
func buildOverviewGroupPrices(
	modelName string,
	groupPricingEnabled bool,
	groupPrices map[string]model.ModelGroupPrice,
	groupNames []string,
) []canvasCatalogOverviewGroupPrice {
	out := make([]canvasCatalogOverviewGroupPrice, 0, len(groupNames))
	for _, groupName := range groupNames {
		row := canvasCatalogOverviewGroupPrice{GroupName: groupName}
		// 档位计费优先：模型(对该分组)配了档表时，总览以档表为准展示。
		if tierTable := model.ResolveGroupTierTableFromPreloaded(modelName, groupName, groupPricingEnabled, groupPrices); tierTable != nil {
			row.QuotaType = 1
			row.PriceTiers = tierTable.PriceTiers
			out = append(out, row)
			continue
		}
		resolved := model.ResolveGroupPriceFromPreloaded(modelName, groupName, groupPricingEnabled, groupPrices)
		row.QuotaType = resolved.QuotaType
		if resolved.Available {
			price := resolved.ModelPrice
			if resolved.QuotaType == 0 {
				// 按 token 倍率计费没有一个"每次调用多少钱"的单一数字 ——
				// 展示 ModelRatio(与「分组与模型定价」页面展示的口径一致),
				// 而不是编一个虚假的按次价格。
				price = resolved.ModelRatio
			}
			row.Price = &price
		}
		out = append(out, row)
	}
	return out
}

// GetCanvasCatalogOverviewAdmin 返回中转站全部已启用模型与画布目录配置状态的
// 关联视图,供管理端"已配置完成 / 未配置"两页分类使用。
//
// 与 GetAllCanvasCatalogModelsAdmin(现有的目录行 CRUD 列表)刻意分开 ——
// 后者的响应形状被编辑表单直接消费,改它的形状会牵动那个表单;这个总览
// 视图是新的独立读接口,不复用也不改造现有接口。
//
// /api/canvas/catalog 的下发内容不受这个接口影响 —— 它仍然只读
// canvas_catalog_model 表,未配置模型只存在于这个管理端视图里。
func GetCanvasCatalogOverviewAdmin(c *gin.Context) {
	enabledNames := model.GetEnabledModels()

	var modelRows []model.Model
	if err := model.DB.Find(&modelRows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	modelByName := make(map[string]model.Model, len(modelRows))
	for _, m := range modelRows {
		modelByName[m.ModelName] = m
	}

	catalogRows, err := model.GetAllCanvasCatalogModelsAdmin()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	catalogByRemoteID := make(map[string]model.CanvasCatalogModel, len(catalogRows))
	for _, cr := range catalogRows {
		catalogByRemoteID[cr.RemoteID] = cr
	}

	allGroupPrices, err := model.GetAllModelGroupPrices()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	groupRatios := ratio_setting.GetGroupRatioCopy()
	groupNames := make([]string, 0, len(groupRatios))
	for name := range groupRatios {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	// 已启用模型名集合可能不完整覆盖"值得在总览里出现"的名字 —— 一个模型
	// 已经建了目录条目,但因渠道停用/删除暂时不在 abilities 里,不该从总览
	// 消失(否则运营方会看到一条自己配过的目录条目突然凭空不见)。取两者并集。
	names := make(map[string]struct{}, len(enabledNames)+len(catalogRows))
	for _, n := range enabledNames {
		names[n] = struct{}{}
	}
	for _, cr := range catalogRows {
		names[cr.RemoteID] = struct{}{}
	}

	rows := make([]canvasCatalogOverviewRow, 0, len(names))
	for name := range names {
		mm, hasModel := modelByName[name]
		cr, hasCatalog := catalogByRemoteID[name]

		row := canvasCatalogOverviewRow{ModelName: name}
		if hasModel {
			row.ModelID = mm.Id
			row.ModelStatus = mm.Status
		}
		if hasCatalog {
			row.CatalogID = cr.Id
			row.DisplayName = cr.DisplayName
			row.Contract = cr.Contract
			row.Capabilities = cr.Capabilities
			row.CatalogEnabled = cr.IsEnabled()
			row.Ready = model.IsCanvasReady(&cr)
		}

		groupPricingEnabled := hasModel && mm.GroupPricingEnabled
		row.GroupPricingEnabled = groupPricingEnabled
		row.GroupPrices = buildOverviewGroupPrices(name, groupPricingEnabled, allGroupPrices[name], groupNames)

		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].ModelName < rows[j].ModelName })

	common.ApiSuccess(c, rows)
}

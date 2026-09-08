package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAllModelsMeta 获取模型列表（分页）
func GetAllModelsMeta(c *gin.Context) {

	pageInfo := common.GetPageQuery(c)
	status := c.Query("status")
	syncOfficial := c.Query("sync_official")
	modelsMeta, total, err := model.SearchModels("", "", status, syncOfficial, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)

	// 统计供应商计数（全部数据，不受分页影响）
	vendorCounts, _ := model.GetVendorModelCounts()

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"items":         modelsMeta,
		"total":         total,
		"page":          pageInfo.GetPage(),
		"page_size":     pageInfo.GetPageSize(),
		"vendor_counts": vendorCounts,
	})
}

// SearchModelsMeta 搜索模型列表
func SearchModelsMeta(c *gin.Context) {

	keyword := c.Query("keyword")
	vendor := c.Query("vendor")
	status := c.Query("status")
	syncOfficial := c.Query("sync_official")
	pageInfo := common.GetPageQuery(c)

	modelsMeta, total, err := model.SearchModels(keyword, vendor, status, syncOfficial, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 批量填充附加字段，提升列表接口性能
	enrichModels(modelsMeta)
	vendorCounts, _ := model.GetVendorModelCounts()
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(modelsMeta)
	common.ApiSuccess(c, gin.H{
		"items":         modelsMeta,
		"total":         total,
		"page":          pageInfo.GetPage(),
		"page_size":     pageInfo.GetPageSize(),
		"vendor_counts": vendorCounts,
	})
}

// GetModelMeta 根据 ID 获取单条模型信息
func GetModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var m model.Model
	if err := model.DB.First(&m, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	enrichModels([]*model.Model{&m})
	common.ApiSuccess(c, &m)
}

// validateGroupPricingNameRule 分组分别定价只对精确匹配的模型行开放。
//
// 非精确匹配(前缀/后缀/包含)的一行代表多个实际计费用的上游模型名 ——
// ResolveGroupPrice 按调用时的字面模型名查 model_group_price(与 ratio_setting
// 现有的 ModelRatio/ModelPrice 完全同一套关联方式,和 NameRule 无关),而这一行
// 本身的 ModelName 只是前缀/后缀/子串,不是任何请求会真正带的字面名。允许在这类
// 行上开关会造出一个界面上能开、保存后却永远查不到任何分组价格的死配置 ——
// 每次调用都会命中"该分组不可用"。此处直接拒绝,而不是静默忽略开关状态。
func validateGroupPricingNameRule(m *model.Model) error {
	if m.GroupPricingEnabled && m.NameRule != model.NameRuleExact {
		return fmt.Errorf("分组分别定价仅支持精确匹配的模型;当前匹配规则不是精确匹配,请先改为精确匹配或关闭分组分别定价")
	}
	return nil
}

// saveModelGroupPrices 落库分组分别定价配置,create/update 共用。
//
// 关闭分别定价模式时**强制清空**该模型的 model_group_price 行,不管请求体里
// GroupPrices 是否为空 —— "未开启即无行"是硬不变量,不依赖前端老实清空数组。
// 否则关了再开会复活上一轮的旧价格,管理员看不到任何提示。
func saveModelGroupPrices(m *model.Model) error {
	if !m.GroupPricingEnabled {
		return model.ReplaceModelGroupPrices(m.ModelName, nil)
	}
	return model.ReplaceModelGroupPrices(m.ModelName, m.GroupPrices)
}

// CreateModelMeta 新建模型
func CreateModelMeta(c *gin.Context) {
	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.ModelName == "" {
		common.ApiErrorMsg(c, "模型名称不能为空")
		return
	}
	if err := validateGroupPricingNameRule(&m); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	// 名称冲突检查
	if dup, err := model.IsModelNameDuplicated(0, m.ModelName); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模型名称已存在")
		return
	}

	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := saveModelGroupPrices(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, &m)
}

// UpdateModelMeta 更新模型
func UpdateModelMeta(c *gin.Context) {
	statusOnly := c.Query("status_only") == "true"

	var m model.Model
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorMsg(c, "缺少模型 ID")
		return
	}
	if !statusOnly {
		if err := validateGroupPricingNameRule(&m); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}

	if statusOnly {
		// 只更新状态，防止误清空其他字段 —— 分组价格同理不能碰:请求体这时候
		// 大概率只带了 {id, status},m.GroupPricingEnabled 会绑成 false 零值,
		// 若在这条分支也调 saveModelGroupPrices 会把该模型的分组价格整表清空。
		if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Update("status", m.Status).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		model.RefreshPricing()
		common.ApiSuccess(c, &m)
		return
	}

	// 名称冲突检查
	if dup, err := model.IsModelNameDuplicated(m.Id, m.ModelName); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "模型名称已存在")
		return
	}

	// model_group_price 按模型名字符串关联(与 abilities/ratio_setting 的既有
	// 惯例一致,没有外键)。改名前先取旧名字,写完新行后把旧名字的行删掉 ——
	// 否则旧名字下的分组价格会变成孤儿配置,谁都查不到也删不掉。
	var oldName string
	if err := model.DB.Model(&model.Model{}).Where("id = ?", m.Id).Pluck("model_name", &oldName).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	if err := m.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	if oldName != "" && oldName != m.ModelName {
		if err := model.DeleteModelGroupPricesByModel(oldName); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if err := saveModelGroupPrices(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, &m)
}

// DeleteModelMeta 删除模型
func DeleteModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Delete(&model.Model{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.RefreshPricing()
	common.ApiSuccess(c, nil)
}

// enrichModels 批量填充附加信息：端点、渠道、分组、计费类型、价格，避免 N+1 查询
func enrichModels(models []*model.Model) {
	if len(models) == 0 {
		return
	}

	// 1) 拆分精确与规则匹配
	exactNames := make([]string, 0)
	exactIdx := make(map[string][]int) // modelName -> indices in models
	ruleIndices := make([]int, 0)
	for i, m := range models {
		if m == nil {
			continue
		}
		if m.NameRule == model.NameRuleExact {
			exactNames = append(exactNames, m.ModelName)
			exactIdx[m.ModelName] = append(exactIdx[m.ModelName], i)
		} else {
			ruleIndices = append(ruleIndices, i)
		}
	}

	// 2) 批量查询精确模型的绑定渠道
	channelsByModel, _ := model.GetBoundChannelsByModelsMap(exactNames)

	// 分组价格只可能出现在精确匹配模型上(见 validateGroupPricingNameRule),
	// 一次性拉全表按模型名分桶,避免对每个开了分别定价的模型单独查一次。
	groupPricesByModel, _ := model.GetAllModelGroupPrices()

	// 获取定价信息用于填充价格字段
	pricingMap := model.GetModelPricingMap()

	// 3) 精确模型：端点从缓存、渠道批量映射、分组/计费类型/价格从缓存
	for name, indices := range exactIdx {
		chs := channelsByModel[name]
		pricing := pricingMap[name]
		for _, idx := range indices {
			mm := models[idx]
			if mm.Endpoints == "" {
				eps := model.GetModelSupportEndpointTypes(mm.ModelName)
				if b, err := json.Marshal(eps); err == nil {
					mm.Endpoints = string(b)
				}
			}
			mm.BoundChannels = chs
			mm.EnableGroups = model.GetModelEnableGroups(mm.ModelName)
			mm.QuotaTypes = model.GetModelQuotaTypes(mm.ModelName)
			if pricing != nil {
				mm.ModelRatio = pricing.ModelRatio
				mm.ModelPrice = pricing.ModelPrice
				mm.CompletionRatio = pricing.CompletionRatio
			}
			if mm.GroupPricingEnabled {
				if rows, ok := groupPricesByModel[name]; ok {
					mm.GroupPrices = make([]model.ModelGroupPrice, 0, len(rows))
					for _, row := range rows {
						mm.GroupPrices = append(mm.GroupPrices, row)
					}
				}
			}
		}
	}

	if len(ruleIndices) == 0 {
		return
	}

	// 4) 一次性读取定价缓存，内存匹配所有规则模型
	pricings := model.GetPricing()

	// 为全部规则模型收集匹配名集合、端点并集、分组并集、配额集合
	matchedNamesByIdx := make(map[int][]string)
	endpointSetByIdx := make(map[int]map[constant.EndpointType]struct{})
	groupSetByIdx := make(map[int]map[string]struct{})
	quotaSetByIdx := make(map[int]map[int]struct{})

	for _, p := range pricings {
		for _, idx := range ruleIndices {
			mm := models[idx]
			var matched bool
			switch mm.NameRule {
			case model.NameRulePrefix:
				matched = strings.HasPrefix(p.ModelName, mm.ModelName)
			case model.NameRuleSuffix:
				matched = strings.HasSuffix(p.ModelName, mm.ModelName)
			case model.NameRuleContains:
				matched = strings.Contains(p.ModelName, mm.ModelName)
			}
			if !matched {
				continue
			}
			matchedNamesByIdx[idx] = append(matchedNamesByIdx[idx], p.ModelName)

			es := endpointSetByIdx[idx]
			if es == nil {
				es = make(map[constant.EndpointType]struct{})
				endpointSetByIdx[idx] = es
			}
			for _, et := range p.SupportedEndpointTypes {
				es[et] = struct{}{}
			}

			gs := groupSetByIdx[idx]
			if gs == nil {
				gs = make(map[string]struct{})
				groupSetByIdx[idx] = gs
			}
			for _, g := range p.EnableGroup {
				gs[g] = struct{}{}
			}

			qs := quotaSetByIdx[idx]
			if qs == nil {
				qs = make(map[int]struct{})
				quotaSetByIdx[idx] = qs
			}
			qs[p.QuotaType] = struct{}{}
		}
	}

	// 5) 汇总所有匹配到的模型名称，批量查询一次渠道
	allMatchedSet := make(map[string]struct{})
	for _, names := range matchedNamesByIdx {
		for _, n := range names {
			allMatchedSet[n] = struct{}{}
		}
	}
	allMatched := make([]string, 0, len(allMatchedSet))
	for n := range allMatchedSet {
		allMatched = append(allMatched, n)
	}
	matchedChannelsByModel, _ := model.GetBoundChannelsByModelsMap(allMatched)

	// 6) 回填每个规则模型的并集信息
	for _, idx := range ruleIndices {
		mm := models[idx]

		// 端点并集 -> 序列化
		if es, ok := endpointSetByIdx[idx]; ok && mm.Endpoints == "" {
			eps := make([]constant.EndpointType, 0, len(es))
			for et := range es {
				eps = append(eps, et)
			}
			if b, err := json.Marshal(eps); err == nil {
				mm.Endpoints = string(b)
			}
		}

		// 分组并集
		if gs, ok := groupSetByIdx[idx]; ok {
			groups := make([]string, 0, len(gs))
			for g := range gs {
				groups = append(groups, g)
			}
			mm.EnableGroups = groups
		}

		// 配额类型集合（保持去重并排序）
		if qs, ok := quotaSetByIdx[idx]; ok {
			arr := make([]int, 0, len(qs))
			for k := range qs {
				arr = append(arr, k)
			}
			sort.Ints(arr)
			mm.QuotaTypes = arr
		}

		// 渠道并集
		names := matchedNamesByIdx[idx]
		channelSet := make(map[string]model.BoundChannel)
		for _, n := range names {
			for _, ch := range matchedChannelsByModel[n] {
				key := ch.Name + "_" + strconv.Itoa(ch.Type)
				channelSet[key] = ch
			}
		}
		if len(channelSet) > 0 {
			chs := make([]model.BoundChannel, 0, len(channelSet))
			for _, ch := range channelSet {
				chs = append(chs, ch)
			}
			mm.BoundChannels = chs
		}

		// 匹配信息
		mm.MatchedModels = names
		mm.MatchedCount = len(names)
	}
}

package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

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

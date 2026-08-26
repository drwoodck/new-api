package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

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
	if m.Contract == "" {
		common.ApiErrorMsg(c, "contract 不能为空")
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

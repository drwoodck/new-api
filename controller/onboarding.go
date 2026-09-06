package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetOnboardingOverview 上新工作台总览:聚合待补元数据 / 待定价 / 待上线目录 /
// 已忽略四列。AdminAuth 由路由组统一提供。
func GetOnboardingOverview(c *gin.Context) {
	overview, err := service.GetOnboardingOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

type launchOnboardingRequest struct {
	ModelNames []string `json:"model_names"`
	Force      bool     `json:"force"`
}

// LaunchOnboardingModels 批量开闸:对每个名字,未定价且未 force 的收进 unpriced
// 不启动;其余启动(models 行 status=1,无行则建;目录条目 enabled=true,无条目
// 只起 meta)。响应 {launched, unpriced}。
func LaunchOnboardingModels(c *gin.Context) {
	var req launchOnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	hasConfig := service.HasAnyBillingConfig
	if req.Force {
		// force=true:跳过定价校验,全部放行。
		hasConfig = func(string) bool { return true }
	}
	launchable, unpriced := service.FilterLaunchable(req.ModelNames, hasConfig)

	launched := make([]string, 0, len(launchable))
	for _, name := range launchable {
		if err := launchOnboardingModel(name); err != nil {
			common.SysError(fmt.Sprintf("开闸模型 %s 失败: %v", name, err))
			continue
		}
		launched = append(launched, name)
	}

	common.ApiSuccess(c, gin.H{
		"launched": launched,
		"unpriced": unpriced,
	})
}

// launchOnboardingModel 启动单个模型:
//   - models 行存在(含软删除,先恢复) → status=1;不存在 → 新建 status=1;
//   - 目录条目(remote_id=模型名)存在 → enabled=true;不存在 → 只起 meta,跳过目录部分。
func launchOnboardingModel(name string) error {
	var m model.Model
	err := model.DB.Unscoped().Where("model_name = ?", name).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		meta := &model.Model{ModelName: name, Status: 1, SyncOfficial: 1}
		// 若该模型已有分组定价行,新建 meta 行必须继承 GroupPricingEnabled=true:
		// 分组行是唯一价时,开闸后 flag=false 会让分组行失效、计费回退全局倍率
		// (37.5 兜底)。与 service.HasAnyBillingConfig 把孤儿行也算已配价的
		// 过度计数配对,影响面在此闭合。
		if hasGroupPrice, gErr := model.HasAnyModelGroupPrice(name); gErr == nil && hasGroupPrice {
			meta.GroupPricingEnabled = true
		}
		if err := meta.Insert(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if m.DeletedAt.Valid {
		// 软删除行先恢复再置上线,否则普通 Update 会被 GORM 软删除 scope 吞掉。
		if err := model.DB.Unscoped().Model(&model.Model{}).Where("id = ?", m.Id).
			Updates(map[string]interface{}{"status": 1, "deleted_at": nil}).Error; err != nil {
			return err
		}
	} else {
		m.Status = 1
		if err := m.Update(); err != nil {
			return err
		}
	}

	entry, err := model.GetCanvasCatalogModelByRemoteID(name)
	if err != nil {
		return err
	}
	if entry != nil {
		enabled := true
		entry.Enabled = &enabled
		if err := entry.Update(); err != nil {
			return err
		}
	}
	return nil
}

type ignoreOnboardingRequest struct {
	ModelNames []string `json:"model_names"`
}

// IgnoreOnboardingModels 把模型名并入忽略名单(option 持久化),之后 overview
// 不再展示这些模型。响应返回合并后的完整名单。
func IgnoreOnboardingModels(c *gin.Context) {
	var req ignoreOnboardingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	merged, err := service.AppendIgnoredModelNames(req.ModelNames)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"ignored": merged})
}

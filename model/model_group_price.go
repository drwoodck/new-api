package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

// ModelGroupPrice 是「分别定价模式」下,单个模型对单个分组的价格覆盖。
// 只在 Model.GroupPricingEnabled 为 true 的模型上生效 —— 统一模式下这张表
// 不参与计费,价格仍由 GroupRatio(分组倍率)× 全局模型价格算出。
//
// 三个价格字段都用指针,理由与 CanvasCatalogModel.Enabled 相同(见该文件注释):
// GORM 把数值零值当"未设置",0(免费)会写不进去,而 nil/0 语义完全不同 ——
// nil = 该维度未配置(分别定价模式下即"这个分组不可用"),0 = 该分组免费。
//
// 只覆盖三个主维度:ModelRatio/CompletionRatio(按 token 计费用)、
// ModelPrice(按次/按量计费用)。cache_ratio/image_ratio/audio_ratio 等继续
// 全局统一,不随这张表变化 —— 这是已确认的范围,不是遗漏。
type ModelGroupPrice struct {
	Id              int      `json:"id"`
	ModelName       string   `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_group,priority:1"`
	GroupName       string   `json:"group_name" gorm:"size:64;not null;uniqueIndex:uk_model_group,priority:2"`
	ModelRatio      *float64 `json:"model_ratio"`
	CompletionRatio *float64 `json:"completion_ratio"`
	ModelPrice      *float64 `json:"model_price"`
	CreatedTime     int64    `json:"created_time" gorm:"bigint"`
	UpdatedTime     int64    `json:"updated_time" gorm:"bigint"`
}

// GetModelGroupPrices 返回某模型的全部分组价格行,管理端编辑表单加载用。
func GetModelGroupPrices(modelName string) ([]ModelGroupPrice, error) {
	var rows []ModelGroupPrice
	err := DB.Where("model_name = ?", modelName).Order("group_name ASC").Find(&rows).Error
	return rows, err
}

// GetModelGroupPrice 查单个模型对单个分组的价格行。未配置时返回 (nil, nil) ——
// 调用方(计费/目录)据此判定"该分组不可用",不是 error。
func GetModelGroupPrice(modelName, groupName string) (*ModelGroupPrice, error) {
	var row ModelGroupPrice
	err := DB.Where("model_name = ? AND group_name = ?", modelName, groupName).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// GetAllModelGroupPrices 一次性拉全表,按模型名分桶。供批量场景(目录总览、
// 目录下发遍历所有模型)使用,避免在循环里逐个查库。
func GetAllModelGroupPrices() (map[string]map[string]ModelGroupPrice, error) {
	var rows []ModelGroupPrice
	if err := DB.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]map[string]ModelGroupPrice, len(rows))
	for _, r := range rows {
		if result[r.ModelName] == nil {
			result[r.ModelName] = make(map[string]ModelGroupPrice)
		}
		result[r.ModelName][r.GroupName] = r
	}
	return result, nil
}

// ReplaceModelGroupPrices 用给定的行整体替换某模型的分组价格配置(事务内删旧插新)。
// rows 为空即清空该模型的全部分组价格 —— 用于"关闭分别定价模式"或"清空重配"。
func ReplaceModelGroupPrices(modelName string, rows []ModelGroupPrice) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("model_name = ?", modelName).Delete(&ModelGroupPrice{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		now := common.GetTimestamp()
		for i := range rows {
			rows[i].Id = 0
			rows[i].ModelName = modelName
			rows[i].CreatedTime = now
			rows[i].UpdatedTime = now
		}
		return tx.Create(&rows).Error
	})
}

// DeleteModelGroupPricesByModel 删除某模型的全部分组价格行。模型改名或删除时调用,
// 避免旧模型名残留孤儿配置(改名场景下,调用方在写入新名字的行之前先删旧名字的)。
func DeleteModelGroupPricesByModel(modelName string) error {
	return DB.Where("model_name = ?", modelName).Delete(&ModelGroupPrice{}).Error
}

// ResolvedGroupPrice 是"某模型对某分组"算出的最终价格 —— 分别定价模式下直接来自
// ModelGroupPrice,统一模式下来自全局价格 × GroupRatio。计费(relay/helper/price.go)
// 与目录下发(controller/canvas_catalog.go)、目录总览(controller/canvas_catalog_admin.go)
// 三处都要用同一个结果,因此抽成这一个函数 —— 不要在别处重新实现这段换算,
// 两份实现迟早会算出不同的数字。
type ResolvedGroupPrice struct {
	// Available 为 false 表示该分组不可用:分别定价模式下这个模型对这个分组
	// 没有配置任何价格行。调用方据此判定"该分组不可用"(计费报错、目录不下发)。
	Available bool
	// QuotaType: 0 = 按 token 倍率计费,1 = 按次/按量固定价。
	QuotaType       int
	ModelPrice      float64
	ModelRatio      float64
	CompletionRatio float64
	// GroupRatioApplied 是实际叠乘的分组倍率。统一模式下等于 GroupRatio[group];
	// 分别定价模式下恒为 1(分别定价的数字本身就是最终价,不再叠乘倍率)。
	GroupRatioApplied float64
}

// ResolveGroupPrice 计算「模型 × 分组」的最终价格。
//
// 全局价格与倍率查询全部走内存缓存(ratio_setting 包与 model.pricing.go 的
// modelPriceMap/modelRatioMap/modelGroupPricingEnabled),本函数本身除了
// GetModelGroupPrice 一次查询外不再打 DB —— 单个"一模型一分组"的场景(计费
// 热路径)用这个。批量场景(目录总览遍历 N 个模型 × M 个分组、目录下发遍历
// N 个模型 × 1 个调用者分组)请用下面的 ResolveGroupPriceFromPreloaded,
// 自己预先一次性拉表,不要循环调本函数——那样每个格子都是一次 DB 查询。
func ResolveGroupPrice(modelName, groupName string) (ResolvedGroupPrice, error) {
	if !IsGroupPricingEnabled(modelName) {
		return resolveFromGlobalPrice(modelName, groupName), nil
	}
	row, err := GetModelGroupPrice(modelName, groupName)
	if err != nil {
		return ResolvedGroupPrice{}, err
	}
	if row == nil {
		return ResolvedGroupPrice{Available: false}, nil
	}
	return resolveFromGroupPriceRow(row), nil
}

// ResolveGroupPriceFromPreloaded 是 ResolveGroupPrice 的零 DB 查询版本。
//
// 调用方必须自己先拿到 groupPricingEnabled(取自一次性查好的 Model 行,或
// IsGroupPricingEnabled 的缓存)与 groupPrices(取自一次 GetAllModelGroupPrices()
// 里该模型对应的那个 map,不存在就传 nil)——本函数本身纯内存计算,
// 与 ResolveGroupPrice 共享同一套换算逻辑(resolveFromGroupPriceRow /
// resolveFromGlobalPrice),不会算出不同的数字。
func ResolveGroupPriceFromPreloaded(
	modelName, groupName string,
	groupPricingEnabled bool,
	groupPrices map[string]ModelGroupPrice,
) ResolvedGroupPrice {
	if !groupPricingEnabled {
		return resolveFromGlobalPrice(modelName, groupName)
	}
	row, ok := groupPrices[groupName]
	if !ok {
		return ResolvedGroupPrice{Available: false}
	}
	return resolveFromGroupPriceRow(&row)
}

// resolveFromGroupPriceRow 把分别定价模式下查到的一行换算成最终价格。
// GroupRatioApplied 恒为 1 —— 已确认的决策:分别定价的数字就是最终价,不叠乘倍率。
func resolveFromGroupPriceRow(row *ModelGroupPrice) ResolvedGroupPrice {
	if row.ModelPrice != nil {
		return ResolvedGroupPrice{
			Available:         true,
			QuotaType:         1,
			ModelPrice:        *row.ModelPrice,
			GroupRatioApplied: 1,
		}
	}
	result := ResolvedGroupPrice{Available: true, QuotaType: 0, GroupRatioApplied: 1}
	if row.ModelRatio != nil {
		result.ModelRatio = *row.ModelRatio
	}
	if row.CompletionRatio != nil {
		result.CompletionRatio = *row.CompletionRatio
	}
	return result
}

// resolveFromGlobalPrice 是统一模式的路径:全局价格 × GroupRatio[分组]。
// 统一模式下"该分组不可用"这个概念不存在 —— GetGroupRatio 对未知分组回退 1
// 并记日志(不是报错),因此这条路径的 Available 恒为 true。
func resolveFromGlobalPrice(modelName, groupName string) ResolvedGroupPrice {
	groupRatio := ratio_setting.GetGroupRatio(groupName)
	if modelPrice, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return ResolvedGroupPrice{
			Available:         true,
			QuotaType:         1,
			ModelPrice:        modelPrice * groupRatio,
			GroupRatioApplied: groupRatio,
		}
	}
	modelRatio, _, _ := ratio_setting.GetModelRatio(modelName)
	return ResolvedGroupPrice{
		Available:         true,
		QuotaType:         0,
		ModelRatio:        modelRatio,
		CompletionRatio:   ratio_setting.GetCompletionRatio(modelName),
		GroupRatioApplied: groupRatio,
	}
}

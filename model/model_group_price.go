package model

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

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
//
// PriceTiers 是该分组对"档位计费模型"的价格覆盖(可空)。非 nil 时该分组
// 的价格权威 = 行内档表(每档可独立选计价单位 second/request,最终价不叠乘
// GroupRatio),ModelPrice/ModelRatio 三个标量退居为"无档表模型的单档退化";
// nil 表示该分组没配档表 —— 档位模型下即"该档不可用"(不回退全局档表 × 倍率,
// 见 ResolveTierPrice 注释)。
type ModelGroupPrice struct {
	Id              int                  `json:"id"`
	ModelName       string               `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_group,priority:1"`
	GroupName       string               `json:"group_name" gorm:"size:64;not null;uniqueIndex:uk_model_group,priority:2"`
	ModelRatio      *float64             `json:"model_ratio"`
	CompletionRatio *float64             `json:"completion_ratio"`
	ModelPrice      *float64             `json:"model_price"`
	PriceTiers      *types.PriceTierList `json:"price_tiers,omitempty" gorm:"type:text"`
	CreatedTime     int64                `json:"created_time" gorm:"bigint"`
	UpdatedTime     int64                `json:"updated_time" gorm:"bigint"`
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
//
// 每行的 PriceTiers 在落库前经 types.NormalizePriceTierList 校验归一化 ——
// 全局档表路径(option)有等价校验,行内档表也必须有一道:负价/0 价/混型/重复键
// 若不在此拦截,计费热路径按"已归一化"假设解析,会产生负预扣或全请求不可用。
// 任一行的档表非法即整体拒绝(不部分生效)。
func ReplaceModelGroupPrices(modelName string, rows []ModelGroupPrice) error {
	for i := range rows {
		if rows[i].PriceTiers == nil {
			continue
		}
		normalized, err := types.NormalizePriceTierList(*rows[i].PriceTiers)
		if err != nil {
			return fmt.Errorf("分组 %s 档表: %w", rows[i].GroupName, err)
		}
		normalizedList := types.PriceTierList(normalized)
		rows[i].PriceTiers = &normalizedList
	}
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

// ResolveGroupPriceFromRow 用调用方已查好的行做标量换算(热路径行复用)。
// row == nil 返回 Available=false(与 ResolveGroupPrice 行缺失语义一致)。
func ResolveGroupPriceFromRow(row *ModelGroupPrice) ResolvedGroupPrice {
	if row == nil {
		return ResolvedGroupPrice{Available: false}
	}
	return resolveFromGroupPriceRow(row)
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

// ---------------------------------------------------------------------------
// 档位计费（price_tiers）解析
// ---------------------------------------------------------------------------
//
// 与 ResolveGroupPrice 同一哲学：统一模式与分别定价两模式的"档位解析"
// 收敛在这一个函数里，计费热路径、目录下发、总览三处复用同一份换算，
// 不要在别处重写 —— 两份实现迟早会算出不同的数字。

// TierResolution 是 ResolveTierPrice 的解析状态。
type TierResolution int

const (
	// TierResolutionNoTable:模型(或该分组)没有配置档表 —— 调用方应回退
	// 既有按次/按秒/按 token 路径,旧模型行为完全不变。
	TierResolutionNoTable TierResolution = iota
	// TierResolutionUnavailable:模型配了档表,但请求档位在表内不存在
	// (或输入维度为空) —— 硬报错,不回退全局(未覆盖即不可用的闸门语义)。
	TierResolutionUnavailable
	// TierResolutionResolved:已按请求档位解析出价格。
	TierResolutionResolved
)

// ResolvedTierPrice 是"某模型对某分组 × 请求档位"算出的档位价格。
type ResolvedTierPrice struct {
	Status      TierResolution
	// TierType 档表维度（types.TierTypeResolution/Request/...）。
	TierType    string
	// TierKey 命中的归一化档位键（request 空 key 档命中时为 ""）。
	TierKey     string
	Label       string
	// Price 档位原价（美元，未乘分组倍率）—— 调用方按 GroupRatioApplied 乘一次，
	// 与既有 ModelPriceHelperPerCall 的"原价 × GroupRatio"口径保持一致，避免双重乘。
	Price       float64
	// BillingUnit 命中的计价单位（types.BillingUnitSecond / BillingUnitRequest）。
	BillingUnit string
	// GroupRatioApplied 统一模式=GroupRatio[group]；分别定价模式恒 1。
	GroupRatioApplied float64
	// TierSnapshot 预扣时的完整档表快照（来源档表的拷贝，价格保持原价）。
	// 结算阶段重选档位的权威依据 —— 提交与完成之间管理员改价不影响在途任务。
	TierSnapshot *types.PriceTierList
}

// ResolveTierPrice 解析「模型 × 分组 × 请求档位」的最终价格。
//
// 判定顺序（确定性收敛，优先级矩阵见 plan）：
//   - 分别定价模式（IsGroupPricingEnabled）下档位只可能来自行内档表：
//     行缺失或行内无档表 → NoTable（落回既有 ResolveGroupPrice 的
//     "分组不可用 / 3 标量"路径，不回落全局档表 —— 未覆盖即不可用）。
//   - 统一模式：全局档表（VideoPriceTiers option）选中档，倍率取
//     用户组特殊倍率（group_group_ratio，命中时）否则分组倍率 —— 与
//     HandleGroupRatio/旧按秒路径的倍率口径一致，不能只认裸 GroupRatio
//     （否则配置了特殊倍率的用户组在档表路径下静默少收/多收）。
//   - 都没有 → NoTable。
//
// 与 ResolveGroupPrice 一样,本函数本身除 GetModelGroupPrice 一次查询外
// 不再打 DB（全局档表与分组定价开关均走内存缓存）。
func ResolveTierPrice(modelName, userGroup, groupName string, tierInput types.TierInput) (ResolvedTierPrice, error) {
	// 分别定价模式下档表只认行内:先确认模式再查行,避免无谓的全局档表查询。
	if IsGroupPricingEnabled(modelName) {
		row, err := GetModelGroupPrice(modelName, groupName)
		if err != nil {
			return ResolvedTierPrice{}, err
		}
		return ResolveTierPriceFromRow(modelName, groupName, tierInput, row), nil
	}
	return ResolveTierPriceUnified(modelName, userGroup, groupName, tierInput), nil
}

// ResolveTierPriceFromRow 是分别定价模式的分支:消费调用方已查好的行。
// 计费热路径用它避免"档位检查 + 旧标量解析"各查一次 model_group_price。
func ResolveTierPriceFromRow(modelName, groupName string, tierInput types.TierInput, row *ModelGroupPrice) ResolvedTierPrice {
	if row == nil || row.PriceTiers == nil {
		return ResolvedTierPrice{Status: TierResolutionNoTable}
	}
	return resolveTierFromTiers(*row.PriceTiers, 1, tierInput)
}

// ResolveTierPriceUnified 是统一模式的分支:全局档表(无 DB 查询)。
// 倍率取用户组特殊倍率(命中时)否则分组倍率。
func ResolveTierPriceUnified(modelName, userGroup, groupName string, tierInput types.TierInput) ResolvedTierPrice {
	tiers, ok := ratio_setting.GetVideoPriceTiers(modelName)
	if !ok {
		return ResolvedTierPrice{Status: TierResolutionNoTable}
	}
	return resolveTierFromTiers(tiers, unifiedGroupRatio(userGroup, groupName), tierInput)
}

// unifiedGroupRatio 返回统一模式下的应用倍率:用户组特殊倍率优先,否则分组倍率。
func unifiedGroupRatio(userGroup, groupName string) float64 {
	if special, ok := ratio_setting.GetGroupGroupRatio(userGroup, groupName); ok {
		return special
	}
	return ratio_setting.GetGroupRatio(groupName)
}

// ResolveTierPriceFromPreloaded 是 ResolveTierPrice 的零 DB 查询版本(镜像
// ResolveGroupPriceFromPreloaded):调用方预先拿到 groupPricingEnabled 与
// groupPrices(缺就传 nil),本函数纯内存计算,与 ResolveTierPrice 共享
// resolveTierFromTiers,不会算出不同的数字。
func ResolveTierPriceFromPreloaded(
	modelName, userGroup, groupName string,
	tierInput types.TierInput,
	groupPricingEnabled bool,
	groupPrices map[string]ModelGroupPrice,
) ResolvedTierPrice {
	if groupPricingEnabled {
		row, ok := groupPrices[groupName]
		if !ok {
			return ResolvedTierPrice{Status: TierResolutionNoTable}
		}
		return ResolveTierPriceFromRow(modelName, groupName, tierInput, &row)
	}
	return ResolveTierPriceUnified(modelName, userGroup, groupName, tierInput)
}

// resolveTierFromTiers 在归一化档表内按 tierInput 匹配档位。档表由
// NormalizePriceTierList 保证单一 tier_type;request 档优先按时长键精确匹配,
// 未命中且表内有空 key 档时命中空 key 档(任意请求固定价,对齐 paipu 渠道1)。
func resolveTierFromTiers(tiers types.PriceTierList, groupRatio float64, tierInput types.TierInput) ResolvedTierPrice {
	if len(tiers) == 0 {
		return ResolvedTierPrice{Status: TierResolutionNoTable}
	}
	tierType := tiers[0].TierType

	var tierKey string
	switch tierType {
	case types.TierTypeResolution:
		tierKey = tierInput.Resolution
	case types.TierTypeImageSize:
		tierKey = tierInput.ImageSize
	case types.TierTypeMode:
		tierKey = tierInput.Mode
	case types.TierTypeRequest:
		if tierInput.DurationSeconds <= 0 {
			return ResolvedTierPrice{Status: TierResolutionUnavailable}
		}
		tierKey = strconv.Itoa(tierInput.DurationSeconds) + "s"
	}
	if tierKey == "" {
		return ResolvedTierPrice{Status: TierResolutionUnavailable}
	}

	tier, ok := types.SelectTierKey(tiers, tierKey)
	if !ok && tierType == types.TierTypeRequest {
		// 时长键未命中时回退空 key 档(任意请求固定价)。
		tier, ok = types.SelectTierKey(tiers, "")
	}
	if !ok {
		return ResolvedTierPrice{Status: TierResolutionUnavailable, TierKey: tierKey}
	}
	// Price 保持档位原价,不在此乘 groupRatio —— 调用方统一按
	// GroupRatioApplied 乘一次(与 ModelPriceHelperPerCall 既有口径一致)。
	snapshot := make(types.PriceTierList, len(tiers))
	copy(snapshot, tiers)
	return ResolvedTierPrice{
		Status:            TierResolutionResolved,
		TierType:          tierType,
		TierKey:           tier.Key,
		Label:             tier.Label,
		Price:             tier.Price,
		BillingUnit:       tier.BillingUnit,
		GroupRatioApplied: groupRatio,
		TierSnapshot:      &snapshot,
	}
}

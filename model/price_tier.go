package model

import (
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// ---------------------------------------------------------------------------
// 档表工具：目录下发与结算共用
// ---------------------------------------------------------------------------

// GroupTierTable 是目录下发用的"某模型对某分组实际生效的档表"。
type GroupTierTable struct {
	// PriceTiers 终价档表：统一模式为全局档表每档 × GroupRatio 后的全新副本；
	// 分别定价模式为行内档表原样（行内档价本就是终价）。目录下发的语义是
	// "已按调用者分组算好的最终数字"（画布 buildEstimateInput 把 groupRatio
	// 覆盖为 1，不再叠乘），与 canvasGroupPrice.ModelPrice 的既有缩放一致。
	// 无档表时为 nil。
	PriceTiers *types.PriceTierList
	// GroupRatioApplied 统一模式=GroupRatio[group]（对账/展示）；分别定价
	// 模式恒 1。
	GroupRatioApplied float64
}

// ResolveGroupTierTable 返回某模型对某分组实际生效的档表（目录下发用）。
//
// 语义与 ResolveTierPrice 完全一致：分别定价模式只认行内档表，统一模式用
// 全局档表。差别仅在于返回整张表而非按请求档位选中的一档。
func ResolveGroupTierTable(modelName, groupName string) (*GroupTierTable, error) {
	if IsGroupPricingEnabled(modelName) {
		row, err := GetModelGroupPrice(modelName, groupName)
		if err != nil {
			return nil, err
		}
		if row == nil || row.PriceTiers == nil {
			return nil, nil
		}
		return &GroupTierTable{PriceTiers: row.PriceTiers, GroupRatioApplied: 1}, nil
	}
	return resolveGroupTierTableFromGlobal(modelName, groupName), nil
}

// ResolveGroupTierTableFromPreloaded 是 ResolveGroupTierTable 的零 DB 查询版本
// （镜像 ResolveGroupPriceFromPreloaded）：目录总览遍历 N 个模型 × M 个分组时
// 用，调用方已一次性预载 groupPrices。与 ResolveGroupTierTable 共享同一换算。
func ResolveGroupTierTableFromPreloaded(
	modelName, groupName string,
	groupPricingEnabled bool,
	groupPrices map[string]ModelGroupPrice,
) *GroupTierTable {
	if groupPricingEnabled {
		row, ok := groupPrices[groupName]
		if !ok || row.PriceTiers == nil {
			return nil
		}
		return &GroupTierTable{PriceTiers: row.PriceTiers, GroupRatioApplied: 1}
	}
	return resolveGroupTierTableFromGlobal(modelName, groupName)
}

func resolveGroupTierTableFromGlobal(modelName, groupName string) *GroupTierTable {
	tiers, ok := ratio_setting.GetVideoPriceTiers(modelName)
	if !ok {
		return nil
	}
	groupRatio := ratio_setting.GetGroupRatio(groupName)
	if groupRatio == 1 {
		return &GroupTierTable{PriceTiers: &tiers, GroupRatioApplied: 1}
	}
	// 缩放副本：目录下发的语义是"已按该分组算好的终价"，不能直接引用
	// 全局档表（会把倍率漏给画布端）。副本避免污染全局档表。
	scaled := make(types.PriceTierList, len(tiers))
	for i, tier := range tiers {
		scaled[i] = tier
		scaled[i].Price = tier.Price * groupRatio
	}
	return &GroupTierTable{PriceTiers: &scaled, GroupRatioApplied: groupRatio}
}

// SelectTierFromSnapshot 是结算阶段的档位重选：在预扣时存档的档表快照里，
// 按任务实际结果重选一档。快照是权威 —— 绝不重查当前档表（管理员可能在
// 提交与完成之间改价/删档）。
//
// 语义：
//   - tier_type=resolution 且 actualResolution 非空：归一化后查快照；
//     命中新档 → 返回该档（true）；新档键在快照中缺失 → 返回 (零值, false)，
//     调用方保持预扣档并记 warn（缺档不回退全局，闸门语义延续到结算）。
//   - 其余维度（request/image_size/mode）或 actualResolution 为空：不重选，
//     返回 (零值, false)（沿用预扣档）。
func SelectTierFromSnapshot(snap *types.PriceTierList, tierType, actualResolution string) (types.PriceTier, bool) {
	if snap == nil || len(*snap) == 0 {
		return types.PriceTier{}, false
	}
	if tierType != types.TierTypeResolution {
		return types.PriceTier{}, false
	}
	key, ok := types.NormalizeTierKey(types.TierTypeResolution, actualResolution)
	if !ok {
		return types.PriceTier{}, false
	}
	tier, ok := types.SelectTierKey(*snap, key)
	if !ok {
		return types.PriceTier{}, false
	}
	return tier, true
}

package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/types"
)

// pricingUnavailable 是「该模型 × 分组没有任何可展示价格」时的统一文案。
// 显式输出而非留空,让目录/定价页明确区分「未定价」与展示缺陷。
const pricingUnavailable = "未定价"

// GeneratePricingSummary 生成「模型 × 分组」的人读定价文案 —— 目录 pricing
// 字段的自动文案真源,与用户端定价页同一套解析口径(计费怎么收,文案怎么写)。
//
// 优先级与计费侧 ModelPriceHelperPerCall 完全一致:档表 → 秒价 → 按次/倍率;
// 有素材价配置时在末尾追加素材段。未定价显式输出「未定价」,不留空。
//
// group 为空串时按"没有分组信息"处理:走全局默认价(不乘分组倍率),与
// resolveCanvasGroupPrice 的 fail-open 同一边界。
func GeneratePricingSummary(modelName, groupName string) string {
	groupPricingEnabled := IsGroupPricingEnabled(modelName)
	var row *ModelGroupPrice
	if groupPricingEnabled {
		r, err := GetModelGroupPrice(modelName, groupName)
		if err != nil {
			// 查行失败不猜价:返回未定价比编一个错价安全。
			return pricingUnavailable
		}
		row = r
	}
	return GeneratePricingSummaryPreloaded(modelName, groupName, groupPricingEnabled, row)
}

// GeneratePricingSummaryPreloaded 是零 DB 版:调用方已持有分组定价开关与行
// (计费热路径/批量场景复用)。与 GeneratePricingSummary 共享全部换算,
// 不会算出不同的文案。
func GeneratePricingSummaryPreloaded(modelName, groupName string, groupPricingEnabled bool, row *ModelGroupPrice) string {
	var parts []string

	// 1) 档表(终价,已含分组倍率):逐档列出。
	//    分别定价模式直接复用调用方已查好的行内档表(行内档价本就是终价,
	//    GroupRatioApplied=1,与 ResolveGroupTierTableFromPreloaded 行内分支
	//    语义逐字一致,零 DB 查询);行缺失或行内无档表 → nil,不回退全局档表
	//    (未覆盖即不可用的闸门语义)。统一模式走全局档表 ×GroupRatio 换算。
	var tierTable *GroupTierTable
	if groupPricingEnabled {
		if row != nil && row.PriceTiers != nil {
			tierTable = &GroupTierTable{PriceTiers: row.PriceTiers, GroupRatioApplied: 1}
		}
	} else if t, err := ResolveGroupTierTable(modelName, groupName); err == nil {
		tierTable = t
	}
	if tierTable != nil && tierTable.PriceTiers != nil {
		var tierParts []string
		for _, tier := range *tierTable.PriceTiers {
			unit := "/秒"
			if tier.BillingUnit == types.BillingUnitRequest {
				unit = "/次"
			}
			// 档位用「480p（$0.33/秒）；720p（$0.55/秒）」这种样式:与阶梯表达式的
			// 上下文档保持同一种写法,且不再把「档表:」这种后台术语摆在用户面前。
			tierParts = append(tierParts, fmt.Sprintf("%s（$%s%s）", tier.Label, formatPrice(tier.Price), unit))
		}
		parts = append(parts, strings.Join(tierParts, "；"))
	} else if secondPrice, ok := ResolveVideoSecondPriceForGroup(modelName, groupName, groupPricingEnabled, row); ok {
		// 2) 秒价(行内为终价;统一模式为原价 —— 统一模式文案不带倍率,
		//    与 /api/pricing 的 VideoSecondPrice 展示口径一致)。
		parts = append(parts, fmt.Sprintf("$%s/秒", formatPrice(secondPrice)))
	} else if text, ok := DescribeBillingExpr(modelName); ok {
		// 3) 阶梯表达式计费:价格由表达式按上下文长度/缓存/图片等维度现算,
		//    没有单一标价。把表达式拆成「上下文≤272K（输入$0.96/M 输出$4.81/M）；
		//    其余（…）」这种人话 —— 只写一句「阶梯表达式计费」等于没写价,
		//    用户既无法比较也判断不出贵不贵。
		parts = append(parts, text)
	} else if UsesTieredExprBilling(modelName) {
		// 表达式结构不是「上下文阶梯」(例如按请求头/时段变价):没有单一价可列,
		// 但必须给一句人话 —— 不能落到「未定价」,画布把未定价当「没有价格信息」,
		// 会把整条模型从列表里藏掉(2026-10-26:gpt-6.1-sol / grok-4.7 就是这么消失的)。
		parts = append(parts, "阶梯表达式计费")
	} else if groupPricingEnabled {
		// 4) 分别定价:行内标量(ResolveGroupPriceFromRow)。
		resolved := ResolveGroupPriceFromRow(row)
		if !resolved.Available {
			return pricingUnavailable
		}
		parts = append(parts, formatScalar(resolved))
	} else {
		// 4) 统一模式:全局按次价/倍率(不乘分组倍率 —— 文案描述的是模型
		//    标价,分组差异由「分组倍率」概念承载,与定价页一致)。
		resolved := resolveFromGlobalPrice(modelName, groupName)
		parts = append(parts, formatScalar(resolved))
	}

	if materialPart := formatMaterialPrices(modelName, groupName, groupPricingEnabled, row); materialPart != "" {
		parts = append(parts, materialPart)
	}
	return strings.Join(parts, " · ")
}

// formatPrice 去尾零输出美元数值(0.10 → "0.1")。
//
// 用 6 位有效数字而不是原样打印:倍率相乘会带出浮点尾巴
// (0.3×2×0.3 = 0.18000000000000002),那种数字摆给用户看只会让人怀疑计价坏了。
func formatPrice(price float64) string {
	return strconv.FormatFloat(price, 'g', 6, 64)
}

// formatScalar 输出统一模式/分别定价的标量价格段。
// QuotaType 1(按次/按量固定价)→ 显式 0 输出「免费」,否则 "$X/次";
// QuotaType 0(按 token 倍率)→ "输入$X/M · 输出$Y/M",
// 输入价 = ModelRatio × 2(1 ratio = $2/1M tokens),输出价 = 输入价 × CompletionRatio。
// 显式 0 倍率输出「免费」(0 是显式配置的免费,与「未配置」区分)。
//
// 展示口径从 /1K 改成 /M:用户对「每百万 token」有直观感受,/1K 还得自己乘 1000;
// 而且 /1K 会把 $0.6/M 写成 0.0006、把 $0.18/M 写成 0.00017999999999999998 ——
// 一串只有机器看得懂的浮点尾巴。输入/输出分列,与画布价格列同一读法。
func formatScalar(resolved ResolvedGroupPrice) string {
	if resolved.QuotaType == 1 {
		if resolved.ModelPrice == 0 {
			return "免费"
		}
		return fmt.Sprintf("$%s/次", formatPrice(resolved.ModelPrice))
	}
	inputPerM := resolved.ModelRatio * 2
	if inputPerM <= 0 {
		return "免费"
	}
	// completion_ratio 缺失或为 0 时按 1 处理(输出与输入同价),不能报成 0 元输出。
	completion := resolved.CompletionRatio
	if completion <= 0 {
		completion = 1
	}
	return fmt.Sprintf("输入$%s/M · 输出$%s/M", formatPrice(inputPerM), formatPrice(inputPerM*completion))
}

// formatMaterialPrices 输出素材价段:每类素材一段,段间「 · 」分隔。
// image → "输入图 $X/张";video → "输入视频 $X/秒";audio → "输入音频 $X/秒";
// 显式 0 价输出「免费」(与未配置由条目存在性区分)。无配置返回空串(不输出素材段)。
func formatMaterialPrices(modelName, groupName string, groupPricingEnabled bool, row *ModelGroupPrice) string {
	list, ok := ResolveMaterialPricesForGroup(modelName, groupName, groupPricingEnabled, row)
	if !ok || len(list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, p := range list {
		var label, priceStr string
		switch p.MaterialType {
		case types.MaterialTypeImage:
			label = "输入图"
			if p.PricePerUnit == 0 {
				priceStr = "免费"
			} else {
				priceStr = fmt.Sprintf("$%s/张", formatPrice(p.PricePerUnit))
			}
		case types.MaterialTypeVideo:
			label = "输入视频"
			if p.PricePerSecond == 0 {
				priceStr = "免费"
			} else {
				priceStr = fmt.Sprintf("$%s/秒", formatPrice(p.PricePerSecond))
			}
		case types.MaterialTypeAudio:
			label = "输入音频"
			if p.PricePerSecond == 0 {
				priceStr = "免费"
			} else {
				priceStr = fmt.Sprintf("$%s/秒", formatPrice(p.PricePerSecond))
			}
		default:
			// 素材类型经 NormalizeInputMaterialPriceList 校验,理论不可达。
			continue
		}
		parts = append(parts, label+" "+priceStr)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

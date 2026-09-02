package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 档位计费的维度与计价单位常量。与 paipu（Lec API）的 price_tiers 契约对齐：
// tier_type 决定"按什么维度分档"，billing_unit 决定"该档按什么计价"。
const (
	TierTypeRequest    = "request"
	TierTypeResolution = "resolution"
	TierTypeImageSize  = "image_size"
	TierTypeMode       = "mode"

	BillingUnitSecond  = "second"
	BillingUnitRequest = "request"
)

// 档价合法区间（美元/单位）：
//   - MinTierPrice：档价 × QuotaPerUnit(500000) 至少折算 1 quota —— 再低的
//     单价预扣恒为 0，"配了价 = 免费"与系统对 0 价的显式拒绝语义矛盾。
//   - MaxTierPrice：远超任何合理单价的上限护栏；再大的值经倍率与时长连乘
//     会溢出为 +Inf 让目录接口整体 500（计费侧虽有 Strict 兜底 fail-closed，
//     目录下发没有）。
const (
	MinTierPrice = 2e-6
	MaxTierPrice = 1e6
)

// PriceTier 是档位表中的一档：某个维度键值下的价格。
//
//	Label        展示名（如 "720P"、"5 秒"）
//	TierType     档位维度（request/resolution/image_size/mode）
//	Key          归一化后的档位键（"720p"、"1K"、"5s"、"frame"）
//	BillingUnit  计价单位（second 按秒 / request 按次）
//	Price        单价（美元/秒 或 美元/次，由 BillingUnit 决定语义）
type PriceTier struct {
	Label       string  `json:"label"`
	TierType    string  `json:"tier_type"`
	Key         string  `json:"key"`
	BillingUnit string  `json:"billing_unit"`
	Price       float64 `json:"price"`
}

// TierInput 是一次请求的档位选择输入，由 relay/common 按渠道归一化后构建。
// 档表是哪种 tier_type，就取哪个字段做匹配；未用到的维度留空即可。
type TierInput struct {
	Resolution      string // 归一化分辨率标签："480p"/"720p"/"1080p"/"4k"/"768p"
	ImageSize       string // 归一化图像尺寸："1K"/"2K"/"4K"
	DurationSeconds int    // 视频时长（秒）
	Mode            string // 模式键："frame" 等
}

// PriceTierList 是档表切片，实现 driver.Valuer/sql.Scanner 以便作为
// GORM JSON 文本列存储（ModelGroupPrice.PriceTiers）。与 model/prefill_group.go
// 的 JSONValue 模式同构：数据库 []byte/string 双格式读取，nil 存 NULL。
type PriceTierList []PriceTier

// Value 实现 driver.Valuer：空档表写 NULL，否则写 JSON 数组。
func (l PriceTierList) Value() (driver.Value, error) {
	if len(l) == 0 {
		return nil, nil
	}
	return json.Marshal(l)
}

// Scan 实现 sql.Scanner：兼容不同驱动返回的 []byte / string / nil。
func (l *PriceTierList) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*l = nil
		return nil
	case []byte:
		b := make([]byte, len(v))
		copy(b, v)
		return l.UnmarshalJSON(b)
	case string:
		return l.UnmarshalJSON([]byte(v))
	default:
		return fmt.Errorf("unsupported price_tiers column type %T", value)
	}
}

// UnmarshalJSON 接受 JSON 数组，同时容忍被字符串化存储的旧数据（"[\"...\"]"）。
func (l *PriceTierList) UnmarshalJSON(b []byte) error {
	var tiers []PriceTier
	if err := json.Unmarshal(b, &tiers); err == nil {
		*l = PriceTierList(tiers)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(s), &tiers); err != nil {
		return err
	}
	*l = PriceTierList(tiers)
	return nil
}

// NormalizeTierKey 把管理员输入的档位键归一化为档表存储的规范格式。
// 第二个返回值为 false 表示该键在对应维度下不合法（如空的 resolution）。
//
//	resolution：小写化（"720P"→"720p"、"4K"→"4k"）
//	image_size：大写化（"1k"→"1K"）
//	request：纯秒数归一化为 "Ns"（"5"→"5s"）；组合键（如 "720p-5s"）原样保留
//	mode：Trim 原样
func NormalizeTierKey(tierType, key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	switch tierType {
	case TierTypeResolution:
		return strings.ToLower(key), true
	case TierTypeImageSize:
		return strings.ToUpper(key), true
	case TierTypeRequest:
		return normalizeRequestTierKey(key)
	case TierTypeMode:
		return key, true
	default:
		return "", false
	}
}

// normalizeRequestTierKey 处理 request 档的键：纯数字（或带 s 后缀的秒数）
// 统一为 "Ns"；空键（任意请求固定价）由调用方特判。其余组合键（如
// "720p-5s"）一律拒绝 —— 选档逻辑只构造纯时长键，组合键是永远选不中的死档。
func normalizeRequestTierKey(key string) (string, bool) {
	trimmed := strings.TrimSuffix(strings.ToLower(key), "s")
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return strconv.Itoa(seconds) + "s", true
	}
	return "", false
}

// NormalizePriceTierList 校验并归一化一张档表：
//   - 空表返回 (nil, nil)（"未配置档表"语义，由存储层丢弃该条目）
//   - 同一张表内所有档的 tier_type 必须一致（对齐 paipu 单类型档表语义；
//     组合维度用 request 档的时长键表达，跨维度组合键一律拒绝）
//   - 同一张表内 billing_unit 必须一致：结算重选档按"每秒单价"重算，
//     混用单位会把 request 档价当秒价乘时长（多收可达封顶）——
//     分组间单位差异由各自的行内档表表达，不是同一张表内的混用
//   - billing_unit 必须是 second/request；price 有上下界；键归一化后不得重复
//
// 返回的是全新切片，不修改入参。
func NormalizePriceTierList(tiers []PriceTier) ([]PriceTier, error) {
	if len(tiers) == 0 {
		return nil, nil
	}

	normalized := make([]PriceTier, 0, len(tiers))
	seenKeys := make(map[string]bool, len(tiers))
	var tierType string
	var billingUnit string

	for i, tier := range tiers {
		if tier.TierType != TierTypeRequest &&
			tier.TierType != TierTypeResolution &&
			tier.TierType != TierTypeImageSize &&
			tier.TierType != TierTypeMode {
			return nil, fmt.Errorf("第 %d 档 tier_type 不合法: %q", i+1, tier.TierType)
		}
		if tierType == "" {
			tierType = tier.TierType
		} else if tier.TierType != tierType {
			return nil, fmt.Errorf("档位表内 tier_type 必须一致: %q 与 %q 混用", tierType, tier.TierType)
		}
		if tier.BillingUnit != BillingUnitSecond && tier.BillingUnit != BillingUnitRequest {
			return nil, fmt.Errorf("第 %d 档 billing_unit 不合法: %q", i+1, tier.BillingUnit)
		}
		if billingUnit == "" {
			billingUnit = tier.BillingUnit
		} else if tier.BillingUnit != billingUnit {
			return nil, fmt.Errorf("档位表内 billing_unit 必须一致: %q 与 %q 混用", billingUnit, tier.BillingUnit)
		}
		if tier.Price < MinTierPrice || tier.Price > MaxTierPrice {
			return nil, fmt.Errorf("第 %d 档 price 超出合法区间 [%g, %g]: %v",
				i+1, MinTierPrice, MaxTierPrice, tier.Price)
		}
		// request 档允许空 key：语义为"任意请求均按该档固定价"（对齐 paipu 的
		// 渠道1 Seedance 2.0 满血 933：按次 2.30、无 key，时长不影响价格）。
		// 其余维度空 key 一律拒绝。
		var key string
		if tier.TierType == TierTypeRequest && strings.TrimSpace(tier.Key) == "" {
			key = ""
		} else {
			var ok bool
			key, ok = NormalizeTierKey(tier.TierType, tier.Key)
			if !ok {
				return nil, fmt.Errorf("第 %d 档档位键不合法: %q", i+1, tier.Key)
			}
		}
		if seenKeys[key] {
			return nil, fmt.Errorf("档位键重复: %q", key)
		}
		seenKeys[key] = true
		tier.Key = key
		normalized = append(normalized, tier)
	}

	return normalized, nil
}

// SelectTierKey 在档表内按键选档。未命中返回 (PriceTier{}, false)。
// 档表需已归一化（键唯一、类型一致），否则行为未定义。
func SelectTierKey(tiers PriceTierList, key string) (PriceTier, bool) {
	for _, tier := range tiers {
		if tier.Key == key {
			return tier, true
		}
	}
	return PriceTier{}, false
}

// TierDimensionRatioKeys 是"分辨率维度"的 OtherRatios 键名集合：档价本身
// 已按分辨率分档，这些键再乘一次就是双计（sora 的 size=1.666、gemini/vertex
// 的 resolution=1.5/2.333）。档位计费任务在预扣与结算两侧都必须剔除它们；
// seconds（计费时长）与 video_input（正交成本维度）保留。
var TierDimensionRatioKeys = []string{"size", "resolution"}

// IsTierDimensionRatioKey 判断某 OtherRatios 键是否为分辨率维度倍率键。
func IsTierDimensionRatioKey(key string) bool {
	for _, k := range TierDimensionRatioKeys {
		if k == key {
			return true
		}
	}
	return false
}

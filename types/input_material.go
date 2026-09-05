package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// 输入素材类型。素材计费是"加法"维度:总费用 = 生成费(既有分档/秒价/按次)
// + 素材费(本文件),素材费不进 OtherRatios 乘法体系。
const (
	MaterialTypeImage = "image"
	MaterialTypeVideo = "video"
	MaterialTypeAudio = "audio"
)

// MaxTaskDurationSeconds 是用户可控时长的全局上限。真源在本包(ratio_setting
// 等低层包做校验时需要它,又不能反向 import relay/common);relay/common 以
// 别名再导出,既有引用不受影响。用作计费乘数的时长必须先钳制到该值。
const MaxTaskDurationSeconds = 3600

// InputMaterialPrice 是一个模型对一类输入素材的单价。
//
//	MaterialType   素材类型(image/video/audio)
//	PricePerUnit   图片单价(美元/张)
//	PricePerSecond 视频/音频单价(美元/秒)
//	DefaultSeconds 无真值时的估算秒数;0 = 用系统默认(视频 20s / 音频 60s)
type InputMaterialPrice struct {
	MaterialType   string  `json:"material_type"`
	PricePerUnit   float64 `json:"price_per_unit,omitempty"`
	PricePerSecond float64 `json:"price_per_second,omitempty"`
	DefaultSeconds int     `json:"default_seconds,omitempty"`
}

// InputMaterialPriceList 与 PriceTierList 同构:driver.Valuer/sql.Scanner,
// 作为 GORM JSON 文本列存储(model_group_price.input_material_prices 与
// 全局 option InputMaterialPrices),nil 存 NULL。
type InputMaterialPriceList []InputMaterialPrice

func (l InputMaterialPriceList) Value() (driver.Value, error) {
	if len(l) == 0 {
		return nil, nil
	}
	return json.Marshal(l)
}

func (l *InputMaterialPriceList) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*l = nil
		return nil
	case []byte:
		b := make([]byte, len(v))
		copy(b, v)
		return json.Unmarshal(b, l)
	case string:
		return json.Unmarshal([]byte(v), l)
	default:
		return fmt.Errorf("unsupported input_material_prices column type %T", value)
	}
}

// NormalizeInputMaterialPriceList 校验并归一化一张素材价表:
//   - 空表返回 (nil, nil)("未配置"语义,由存储层丢弃)
//   - material_type 必须在枚举内;每类型至多一条
//   - 该类型的计价字段(图片 PricePerUnit / 音视频 PricePerSecond)必须落在
//     [0, MaxTierPrice]:0 = 显式免费(与"未配置"由条目存在性区分),
//     上限复用 MaxTierPrice 护栏
//   - DefaultSeconds ∈ [0, maxDurationSeconds],超界拒绝由调用方钳制语义决定:
//     这里对 >maxDurationSeconds 的值钳到 maxDurationSeconds(与解析侧
//     "钳制后使用"同向),负值拒绝
//   - 不适用的计价字段清零(image 条目的 PricePerSecond、音视频条目的
//     PricePerUnit),固化规范形:落库与计费只认该类型的计价字段,另一字段
//     残留值不产生歧义
//
// 返回全新切片,不修改入参。
func NormalizeInputMaterialPriceList(list InputMaterialPriceList, maxDurationSeconds int) (InputMaterialPriceList, error) {
	if len(list) == 0 {
		return nil, nil
	}
	normalized := make(InputMaterialPriceList, 0, len(list))
	seen := make(map[string]bool, len(list))
	for i, p := range list {
		switch p.MaterialType {
		case MaterialTypeImage, MaterialTypeVideo, MaterialTypeAudio:
		default:
			return nil, fmt.Errorf("第 %d 条 material_type 不合法: %q", i+1, p.MaterialType)
		}
		if seen[p.MaterialType] {
			return nil, fmt.Errorf("素材类型 %q 重复配置", p.MaterialType)
		}
		seen[p.MaterialType] = true
		price := p.PricePerUnit
		if p.MaterialType != MaterialTypeImage {
			price = p.PricePerSecond
		}
		if price < 0 || price > MaxTierPrice {
			return nil, fmt.Errorf("第 %d 条(%s)价格超出合法区间 [0, %g]: %v",
				i+1, p.MaterialType, MaxTierPrice, price)
		}
		if p.DefaultSeconds < 0 {
			return nil, fmt.Errorf("第 %d 条(%s) default_seconds 不能为负: %d",
				i+1, p.MaterialType, p.DefaultSeconds)
		}
		if p.DefaultSeconds > maxDurationSeconds {
			p.DefaultSeconds = maxDurationSeconds
		}
		// 固化规范形:清掉该类型不适用的计价字段(p 是 range 拷贝,不动入参)。
		if p.MaterialType == MaterialTypeImage {
			p.PricePerSecond = 0
		} else {
			p.PricePerUnit = 0
		}
		normalized = append(normalized, p)
	}
	return normalized, nil
}

// ResolvedInputMaterial 是提交时定稿的一条素材计费快照(进 PriceData 与
// TaskBillingContext;结算修正以它为输入)。
//
//	MaterialType 素材类型
//	URL          素材地址;空 = 内联数据(data: URI 等)
//	Seconds      计费时长(视频/音频);图片恒 0
//	UnitPrice    单价快照(美元/张或美元/秒,原价未乘分组倍率)
//	Source       时长来源:explicit(请求显式)/inline_measured(内联实测)/
//	             url_probed(URL 探测)/estimated(估算)/settle_corrected(结算修正)
type ResolvedInputMaterial struct {
	MaterialType string  `json:"material_type"`
	URL          string  `json:"url,omitempty"`
	Seconds      float64 `json:"seconds,omitempty"`
	UnitPrice    float64 `json:"unit_price"`
	Source       string  `json:"source"`
}

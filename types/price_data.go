package types

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

type GroupRatioInfo struct {
	GroupRatio        float64
	GroupSpecialRatio float64
	HasSpecialRatio   bool
}

type PriceData struct {
	FreeModel            bool
	ModelPrice           float64
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
	// VideoSecondPrice 视频按秒计费的每秒单价（美元/秒）。大于 0 表示该请求
	// 走按秒计费，任务完成时需按上游返回的实际时长差额结算。
	VideoSecondPrice float64
	// 素材计费（加法维度，不进 OtherRatios 乘法体系）：
	// MaterialPrices 本次请求生效的素材价表（原价，未乘分组倍率；分别定价模式
	// 即行内表，统一模式即全局表）。
	MaterialPrices InputMaterialPriceList
	// Materials 提交时定稿的素材计费快照（含每条时长与来源）。
	Materials []ResolvedInputMaterial
	// MaterialQuota 素材费额度（已乘分组倍率）。预扣与结算均在此之上做加法。
	MaterialQuota int
	// TierBilling 表示本次请求走档位计费（PriceTier 命中）。档表模型下
	// VideoSecondPrice/ModelPrice 承载的是命中档的单价，配合以下字段在
	// 结算阶段按实际档位差额结算。
	TierBilling bool
	// TierType 命中的档位维度（types.TierTypeResolution 等）。
	TierType string
	// TierKey 预扣时命中的档位键（request 空 key 档命中时为 ""）。
	TierKey string
	// TierBillingUnit 命中档的计价单位（types.BillingUnitSecond/Request）。
	TierBillingUnit string
	// TierSnapshot 预扣时的完整档表快照 —— 结算重选档的权威依据，防止
	// 提交与完成之间管理员改价/删档影响在途任务。
	TierSnapshot      *PriceTierList
	otherRatios       map[string]float64
	UsePrice          bool
	Quota             int // 按次计费的最终额度（MJ / Task）
	QuotaToPreConsume int // 按量计费的预消耗额度
	GroupRatioInfo    GroupRatioInfo
}

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	if !isValidOtherRatio(ratio) {
		return
	}
	if p.otherRatios == nil {
		p.otherRatios = make(map[string]float64)
	}
	p.otherRatios[key] = ratio
}

// RemoveOtherRatio 删除一个附加倍率键。档位计费剔除分辨率维度倍率时使用
// （档价本身已按分辨率分档，size/resolution 键再乘一次就是双计）。
func (p *PriceData) RemoveOtherRatio(key string) {
	if p.otherRatios == nil {
		return
	}
	delete(p.otherRatios, key)
}

func (p *PriceData) ReplaceOtherRatios(ratios map[string]float64) bool {
	p.otherRatios = nil
	for key, ratio := range ratios {
		p.AddOtherRatio(key, ratio)
	}
	return len(p.otherRatios) > 0
}

func (p *PriceData) HasOtherRatio(key string) bool {
	ratio, ok := p.otherRatios[key]
	return ok && isValidOtherRatio(ratio)
}

func (p *PriceData) OtherRatios() map[string]float64 {
	if len(p.otherRatios) == 0 {
		return nil
	}
	ratios := make(map[string]float64, len(p.otherRatios))
	for key, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) {
			ratios[key] = ratio
		}
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

func (p *PriceData) OtherRatioMultiplier() float64 {
	multiplier := 1.0
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			multiplier *= ratio
		}
	}
	return multiplier
}

func (p *PriceData) ApplyOtherRatiosToFloat(value float64) float64 {
	return value * p.OtherRatioMultiplier()
}

func (p *PriceData) ApplyOtherRatiosToDecimal(value decimal.Decimal) decimal.Decimal {
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			value = value.Mul(decimal.NewFromFloat(ratio))
		}
	}
	return value
}

func (p *PriceData) RemoveOtherRatiosFromFloat(value float64) float64 {
	for _, ratio := range p.otherRatios {
		if isValidOtherRatio(ratio) && ratio != 1.0 {
			value /= ratio
		}
	}
	return value
}

func isValidOtherRatio(ratio float64) bool {
	return ratio > 0 && !math.IsInf(ratio, 1)
}

func (p *PriceData) ToSetting() string {
	return fmt.Sprintf("ModelPrice: %f, ModelRatio: %f, CompletionRatio: %f, CacheRatio: %f, GroupRatio: %f, UsePrice: %t, CacheCreationRatio: %f, CacheCreation5mRatio: %f, CacheCreation1hRatio: %f, QuotaToPreConsume: %d, ImageRatio: %f, AudioRatio: %f, AudioCompletionRatio: %f", p.ModelPrice, p.ModelRatio, p.CompletionRatio, p.CacheRatio, p.GroupRatioInfo.GroupRatio, p.UsePrice, p.CacheCreationRatio, p.CacheCreation5mRatio, p.CacheCreation1hRatio, p.QuotaToPreConsume, p.ImageRatio, p.AudioRatio, p.AudioCompletionRatio)
}

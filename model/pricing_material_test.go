package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// TestApplyMaterialPricingToPricing 覆盖 applyMaterialPricingToPricing 三态:
// 统一模式设置全局素材价 → 挂上原价表;未配置 → 不下发;分别定价模式 →
// 不下发(与 PriceTiers 同规则,价格随目录接口按调用者分组返回)。纯函数,
// 不依赖 DB(IsGroupPricingEnabled 读包级缓存,GetInputMaterialPrices 读 RWMap)。
func TestApplyMaterialPricingToPricing(t *testing.T) {
	t.Run("统一模式设置全局素材价后挂上", func(t *testing.T) {
		restoreRatioSettings(t)
		require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(`{"sum-material":[
			{"material_type":"image","price_per_unit":0.01}]}`))

		p := &Pricing{ModelName: "sum-material"}
		applyMaterialPricingToPricing(p, "sum-material")

		require.NotNil(t, p.InputMaterialPrices)
		assert.Equal(t, types.InputMaterialPriceList{
			{MaterialType: types.MaterialTypeImage, PricePerUnit: 0.01},
		}, *p.InputMaterialPrices)
	})

	t.Run("未配置不下发", func(t *testing.T) {
		restoreRatioSettings(t)

		p := &Pricing{ModelName: "no-material"}
		applyMaterialPricingToPricing(p, "no-material")

		assert.Nil(t, p.InputMaterialPrices)
	})

	t.Run("分别定价模式不下发", func(t *testing.T) {
		restoreRatioSettings(t)
		setGroupPricingEnabledForTest(t, "sep-material", true)
		require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(`{"sep-material":[
			{"material_type":"video","price_per_second":0.1}]}`))

		p := &Pricing{ModelName: "sep-material"}
		applyMaterialPricingToPricing(p, "sep-material")

		assert.Nil(t, p.InputMaterialPrices)
	})
}

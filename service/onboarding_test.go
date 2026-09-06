package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHasAnyBillingConfig 钉住三种状态:全局价配 / 仅分组行 / 全无。
// 分组定价开关本身不参与判定 —— 这里只关心"有没有配过价"。
func TestHasAnyBillingConfig(t *testing.T) {
	// 保存并恢复全局状态(五张价格表 + 自用模式开关),避免污染同包其它测试。
	prevPrice := ratio_setting.ModelPrice2JSONString()
	prevRatio := ratio_setting.ModelRatio2JSONString()
	prevVideoSecond := ratio_setting.VideoSecondPrice2JSONString()
	prevVideoTiers := ratio_setting.VideoPriceTiers2JSONString()
	prevInputMaterial := ratio_setting.InputMaterialPrices2JSONString()
	prevSelfUse := operation_setting.SelfUseModeEnabled
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prevPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(prevRatio))
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(prevVideoSecond))
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(prevVideoTiers))
		require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString(prevInputMaterial))
		operation_setting.SelfUseModeEnabled = prevSelfUse
	})

	// 清空全部全局价格、关闭自用模式:保证"未配置"判定不受 GetModelRatio 的
	// 自用模式回退影响。
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateInputMaterialPricesByJSONString("{}"))
	operation_setting.SelfUseModeEnabled = false
	truncate(t)

	t.Run("全无", func(t *testing.T) {
		assert.False(t, HasAnyBillingConfig("no-config-model"))
	})

	t.Run("全局价配", func(t *testing.T) {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"price-model": 0.1}`))
		assert.True(t, HasAnyBillingConfig("price-model"))
		// 只配了 price-model,其它名字仍是未配置。
		assert.False(t, HasAnyBillingConfig("still-no-config-model"))
	})

	t.Run("仅分组行", func(t *testing.T) {
		price := 1.0
		row := model.ModelGroupPrice{ModelName: "group-only-model", GroupName: "default", ModelPrice: &price}
		require.NoError(t, model.DB.Create(&row).Error)
		assert.True(t, HasAnyBillingConfig("group-only-model"))
		assert.False(t, HasAnyBillingConfig("no-config-model"))
	})
}

// TestFilterLaunchable 钉住纯函数拆分:已配价进 ok、未配价进 unpriced,空白名跳过;
// force 场景传入恒真 hasConfig 时全部放行。
func TestFilterLaunchable(t *testing.T) {
	hasConfig := func(name string) bool { return name == "priced-a" || name == "priced-b" }

	ok, unpriced := FilterLaunchable([]string{"priced-a", "unpriced-a", "priced-b"}, hasConfig)
	assert.Equal(t, []string{"priced-a", "priced-b"}, ok)
	assert.Equal(t, []string{"unpriced-a"}, unpriced)

	ok, unpriced = FilterLaunchable([]string{"", "  ", "priced-a"}, hasConfig)
	assert.Equal(t, []string{"priced-a"}, ok)
	assert.Empty(t, unpriced)

	ok, unpriced = FilterLaunchable([]string{"whatever-a", "whatever-b"}, func(string) bool { return true })
	assert.Equal(t, []string{"whatever-a", "whatever-b"}, ok)
	assert.Empty(t, unpriced)
}

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/types"
)

func TestResolveMaterialPricesForGroupGateSemantics(t *testing.T) {
	sp := 0.1
	list := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeVideo, PricePerSecond: 0.1}}
	row := &ModelGroupPrice{
		VideoSecondPrice:    &sp,
		InputMaterialPrices: &list,
	}

	// 分别定价模式:行内配置生效,倍率闸门由调用方处理
	got, ok := ResolveMaterialPricesForGroup("m", "vip", true, row)
	require.True(t, ok)
	assert.Equal(t, list, got)

	// 分别定价模式:行缺失 = 未配置,不回退全局
	_, ok = ResolveMaterialPricesForGroup("m", "vip", true, nil)
	assert.False(t, ok)

	// 统一模式:走全局表(unset 的全局表返回 false)
	_, ok = ResolveMaterialPricesForGroup("m", "vip", false, nil)
	assert.False(t, ok)
}

func TestResolveVideoSecondPriceForGroupGateSemantics(t *testing.T) {
	sp := 0.2
	row := &ModelGroupPrice{VideoSecondPrice: &sp}

	got, ok := ResolveVideoSecondPriceForGroup("m", "vip", true, row)
	require.True(t, ok)
	assert.Equal(t, 0.2, got)

	// nil 列 = 该分组不启用按秒计费,不回退全局
	_, ok = ResolveVideoSecondPriceForGroup("m", "vip", true, &ModelGroupPrice{})
	assert.False(t, ok)
	_, ok = ResolveVideoSecondPriceForGroup("m", "vip", true, nil)
	assert.False(t, ok)
}

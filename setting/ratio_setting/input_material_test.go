package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/types"
)

func TestUpdateInputMaterialPricesByJSONString(t *testing.T) {
	err := UpdateInputMaterialPricesByJSONString(`{"sora-2":[{"material_type":"video","price_per_second":0.1,"default_seconds":20}]}`)
	require.NoError(t, err)
	list, ok := GetInputMaterialPrices("sora-2")
	require.True(t, ok)
	require.Len(t, list, 1)
	assert.Equal(t, "video", list[0].MaterialType)
	assert.Equal(t, 0.1, list[0].PricePerSecond)

	// 未配置模型
	_, ok = GetInputMaterialPrices("other-model")
	assert.False(t, ok)

	// 非法价格整体拒绝,旧配置保持
	err = UpdateInputMaterialPricesByJSONString(`{"sora-2":[{"material_type":"video","price_per_second":-1}]}`)
	require.Error(t, err)
	list, ok = GetInputMaterialPrices("sora-2")
	require.True(t, ok)
	assert.Equal(t, 0.1, list[0].PricePerSecond)

	// 空表条目丢弃
	err = UpdateInputMaterialPricesByJSONString(`{"sora-2":[],"kling-v1":[{"material_type":"audio","price_per_second":0.001}]}`)
	require.NoError(t, err)
	_, ok = GetInputMaterialPrices("sora-2")
	assert.False(t, ok)
	list, ok = GetInputMaterialPrices("kling-v1")
	require.True(t, ok)
	assert.Equal(t, types.MaterialTypeAudio, list[0].MaterialType)
}

package service

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zeroExplicitSeconds 模拟提交链路:请求没有显式时长提示时,视频/音频素材
// 都拿不到 explicit 秒数。
func zeroExplicitSeconds(types.ResolvedInputMaterial) int { return 0 }

func fixedExplicitSeconds(n int) func(types.ResolvedInputMaterial) int {
	return func(types.ResolvedInputMaterial) int { return n }
}

func TestResolveInputMaterialsImageSum(t *testing.T) {
	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeImage, PricePerUnit: 0.01}}
	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeImage, URL: "https://x/1.png"},
		{MaterialType: types.MaterialTypeImage, URL: "https://x/2.png"},
	}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 2.0, nil)
	require.Nil(t, clamp)
	require.Len(t, resolved, 2)
	assert.Equal(t, 0.01, resolved[0].UnitPrice)
	assert.Equal(t, materialSourceExplicit, resolved[0].Source)
	assert.Equal(t, 0.0, resolved[0].Seconds)
	// 2 张 × $0.01 × 2.0 倍率 × 500000 QuotaPerUnit = 20000
	assert.Equal(t, 20000, quota)
}

// TestResolveInputMaterialsExplicitSeconds 钉住显式时长分支:闭包给值时直接
// 采信(标 explicit),钳制 MaxTaskDurationSeconds,单价快照为每秒价。
func TestResolveInputMaterialsExplicitSeconds(t *testing.T) {
	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeVideo, PricePerSecond: 0.002}}
	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeVideo, URL: "https://example.invalid/ref.mp4"},
	}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 2.0, fixedExplicitSeconds(120))
	require.Nil(t, clamp)
	require.Len(t, resolved, 1)
	assert.Equal(t, 120.0, resolved[0].Seconds)
	assert.Equal(t, 0.002, resolved[0].UnitPrice)
	assert.Equal(t, materialSourceExplicit, resolved[0].Source)
	// 120s × $0.002/s × 2.0 × 500000 = 240000
	assert.Equal(t, 240000, quota)

	// 超过 MaxTaskDurationSeconds 的显式值被钳制
	resolved, quota, clamp = ResolveInputMaterials(nil, materials, prices, 1.0, fixedExplicitSeconds(100000))
	require.Nil(t, clamp)
	assert.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), resolved[0].Seconds)
	assert.Equal(t, 3600000, quota) // 3600 × 0.002 × 500000
}

// TestResolveInputMaterialsEstimatedSeconds 钉住估算分支:配置了
// DefaultSeconds 用配置值,未配置用系统默认(视频 20s / 音频 60s),来源
// estimated;估算秒数参与计费。
func TestResolveInputMaterialsEstimatedSeconds(t *testing.T) {
	videoPrices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeVideo, PricePerSecond: 0.002, DefaultSeconds: 30}}
	resolved, quota, clamp := ResolveInputMaterials(nil,
		[]types.ResolvedInputMaterial{{MaterialType: types.MaterialTypeVideo, URL: ""}},
		videoPrices, 1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	require.Len(t, resolved, 1)
	assert.Equal(t, 30.0, resolved[0].Seconds)
	assert.Equal(t, materialSourceEstimated, resolved[0].Source)
	assert.Equal(t, 30000, quota) // 30 × 0.002 × 500000

	// DefaultSeconds 未配置 → 系统默认 20s
	resolved, quota, clamp = ResolveInputMaterials(nil,
		[]types.ResolvedInputMaterial{{MaterialType: types.MaterialTypeVideo, URL: ""}},
		types.InputMaterialPriceList{{MaterialType: types.MaterialTypeVideo, PricePerSecond: 0.002}},
		1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	assert.Equal(t, float64(ratio_setting.MaterialDefaultVideoSeconds), resolved[0].Seconds)
	assert.Equal(t, materialSourceEstimated, resolved[0].Source)
	assert.Equal(t, 20000, quota)

	// 音频走 MaterialDefaultAudioSeconds;0 价 = 显式免费,时长仍要落定
	resolved, quota, clamp = ResolveInputMaterials(nil,
		[]types.ResolvedInputMaterial{{MaterialType: types.MaterialTypeAudio, URL: ""}},
		types.InputMaterialPriceList{{MaterialType: types.MaterialTypeAudio}},
		1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	assert.Equal(t, float64(ratio_setting.MaterialDefaultAudioSeconds), resolved[0].Seconds)
	assert.Equal(t, materialSourceEstimated, resolved[0].Source)
	assert.Equal(t, 0, quota)
}

// TestResolveInputMaterialsProbedFromCache 钉住探测分支:URL 素材先探测成功
// 进缓存,ResolveInputMaterials 复用缓存值并标 url_probed。
func TestResolveInputMaterialsProbedFromCache(t *testing.T) {
	allowLoopbackProbe(t)
	srv := serveBytes(t, buildMinimalWav(2))
	d, ok := ProbeMediaDuration(srv.URL, 3*time.Second)
	require.True(t, ok)
	assert.InDelta(t, 2.0, d, 0.01)

	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeAudio, PricePerSecond: 0.001}}
	materials := []types.ResolvedInputMaterial{{MaterialType: types.MaterialTypeAudio, URL: srv.URL}}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	require.Len(t, resolved, 1)
	assert.Equal(t, materialSourceUrlProbed, resolved[0].Source)
	assert.InDelta(t, 2.0, resolved[0].Seconds, 0.01)
	assert.Equal(t, 1000, quota) // 2s × $0.001/s × 1.0 × 500000
}

// TestResolveInputMaterialsInlineAudio 钉住内联音频分支:data:audio URI 直接
// base64 解码实测(复用 GetAudioDuration),来源 inline_measured。
func TestResolveInputMaterialsInlineAudio(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(buildMinimalWav(3))
	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeAudio, PricePerSecond: 0.001}}
	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeAudio, URL: "data:audio/wav;base64," + data},
	}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	require.Len(t, resolved, 1)
	assert.Equal(t, materialSourceInlineMeasured, resolved[0].Source)
	assert.InDelta(t, 3.0, resolved[0].Seconds, 0.01)
	assert.Equal(t, 1500, quota) // 3s × 0.001 × 500000
}

// TestResolveInputMaterialsSkipsUnpricedType 钉住"未配置素材价的类型不计费":
// 该类型条目不进 resolved 快照,也不产生额度。
func TestResolveInputMaterialsSkipsUnpricedType(t *testing.T) {
	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeImage, PricePerUnit: 0.01}}
	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeImage, URL: "https://x/1.png"},
		{MaterialType: types.MaterialTypeAudio, URL: ""},
	}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 1.0, zeroExplicitSeconds)
	require.Nil(t, clamp)
	require.Len(t, resolved, 1)
	assert.Equal(t, types.MaterialTypeImage, resolved[0].MaterialType)
	assert.Equal(t, 5000, quota) // 1 张 × 0.01 × 1.0 × 500000;audio 未配置不计

	// 空清单 / 空价表直接短路
	_, quota, clamp = ResolveInputMaterials(nil, nil, prices, 1.0, nil)
	require.Nil(t, clamp)
	assert.Equal(t, 0, quota)
	_, quota, clamp = ResolveInputMaterials(nil, materials, nil, 1.0, nil)
	require.Nil(t, clamp)
	assert.Equal(t, 0, quota)
}

// TestResolveInputMaterialsQuotaSaturation 钉住计费饱和不变量:超额条目饱和
// 到 MaxQuota 而非回绕成负数,并通过 clamp 暴露给调用方审计。
func TestResolveInputMaterialsQuotaSaturation(t *testing.T) {
	prices := types.InputMaterialPriceList{{MaterialType: types.MaterialTypeImage, PricePerUnit: 1e6}}
	materials := []types.ResolvedInputMaterial{{MaterialType: types.MaterialTypeImage, URL: "https://x/1.png"}}
	resolved, quota, clamp := ResolveInputMaterials(nil, materials, prices, 1.0, nil)
	require.NotNil(t, clamp)
	assert.Equal(t, common.QuotaClampOverflow, clamp.Kind)
	assert.Equal(t, common.MaxQuota, quota)
	assert.Positive(t, quota)
	require.Len(t, resolved, 1)
}

func TestRefreshMaterialDurationsAtSettle(t *testing.T) {
	allowLoopbackProbe(t)
	srv := serveBytes(t, buildMinimalWav(4))
	d, ok := ProbeMediaDuration(srv.URL, 3*time.Second)
	require.True(t, ok)
	assert.InDelta(t, 4.0, d, 0.01)

	materials := []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeAudio, URL: srv.URL, Seconds: 60, UnitPrice: 0.001, Source: materialSourceEstimated},
		{MaterialType: types.MaterialTypeAudio, URL: "https://example.invalid/never-probed.mp3", Seconds: 60, Source: materialSourceEstimated},
		{MaterialType: types.MaterialTypeVideo, URL: "https://example.invalid/keep.mp4", Seconds: 9, Source: materialSourceUrlProbed},
		{MaterialType: types.MaterialTypeImage, URL: "https://example.invalid/a.png", Source: materialSourceExplicit},
	}
	corrected, changed := RefreshMaterialDurationsAtSettle(materials)
	require.True(t, changed)
	require.Len(t, corrected, 4)

	// 缓存真值修正估算条目
	assert.InDelta(t, 4.0, corrected[0].Seconds, 0.01)
	assert.Equal(t, materialSourceSettleCorrected, corrected[0].Source)
	// 缓存未命中保持原值原来源
	assert.Equal(t, 60.0, corrected[1].Seconds)
	assert.Equal(t, materialSourceEstimated, corrected[1].Source)
	// 非估算来源不修正
	assert.Equal(t, 9.0, corrected[2].Seconds)
	assert.Equal(t, materialSourceUrlProbed, corrected[2].Source)
	assert.Equal(t, materialSourceExplicit, corrected[3].Source)

	// 入参不被修改(不可变)
	assert.Equal(t, 60.0, materials[0].Seconds)
	assert.Equal(t, materialSourceEstimated, materials[0].Source)

	// 无可修正条目时 changed=false
	unchanged, changed := RefreshMaterialDurationsAtSettle(materials[2:])
	assert.False(t, changed)
	assert.Equal(t, materials[2:], unchanged)
}

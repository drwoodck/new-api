package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这些用例直接复刻 2026-09-11 在生产库里查到的配置形态:
// 两个同族图片模型(同一渠道、画布参数表完全相同)却配了不同维度,
// 而 OpenAI 兼容渠道只从 req.Size 取分辨率、且不产生 image_size ——
// 结果两张档表都命中不了,计费静默走回退路径。诊断必须能报出这件事。

func tiersOf(tierType, billingUnit string, keys ...string) *hosttypes.PriceTierList {
	list := make(hosttypes.PriceTierList, 0, len(keys))
	for _, k := range keys {
		list = append(list, hosttypes.PriceTier{
			Label: k, TierType: tierType, Key: k, BillingUnit: billingUnit, Price: 1,
		})
	}
	return &list
}

func rowOf(name string, tiers *hosttypes.PriceTierList) *model.ModelGroupPrice {
	return &model.ModelGroupPrice{ModelName: name, GroupName: "default", PriceTiers: tiers}
}

func TestDiagnoseTier_ImageSizeOnOpenAIChannelIsDead(t *testing.T) {
	// lec-ac-image-2-5-flare 的真实配置:image_size 维度、键 1K/2K/4K。
	// OpenAI 兼容渠道的 ImageSize 只来自 metadata["image_size"],而画布发的是
	// resolution 字段 → 该维度恒为空 → 全表不可命中。
	row := rowOf("lec-ac-image-2-5-flare", tiersOf(
		hosttypes.TierTypeImageSize, hosttypes.BillingUnitRequest, "1K", "2K", "4K"))

	reports := diagnoseTierRowForChannels(row, []int{constant.ChannelTypeOpenAI})

	require.Len(t, reports, 1)
	assert.True(t, reports[0].Problem, "image_size 档在 OpenAI 渠道下应判为不可命中")
	assert.Equal(t, "unavailable", reports[0].ResolvedStatus)
	assert.Empty(t, reports[0].SimulatedInput["image_size"], "该渠道不产生 image_size 值")
}

func TestDiagnoseTier_ResolutionKeysNotProducedByChannel(t *testing.T) {
	// lec-ac-image-2-5-sunburst 的真实配置:resolution 维度、键 1k/2k/4k。
	// OpenAI 兼容渠道在请求为空时分辨率回退渠道默认 720p,档表里没有这个键。
	row := rowOf("lec-ac-image-2-5-sunburst", tiersOf(
		hosttypes.TierTypeResolution, hosttypes.BillingUnitSecond, "1k", "2k", "4k"))

	reports := diagnoseTierRowForChannels(row, []int{constant.ChannelTypeOpenAI})

	require.Len(t, reports, 1)
	assert.True(t, reports[0].Problem, "档表里没有渠道会产出的键,应判为不可命中")
	assert.Equal(t, "720p", reports[0].SimulatedInput["resolution"],
		"OpenAI 渠道空请求的分辨率回退默认值")
}

func TestDiagnoseTier_HittableTableIsNotFlagged(t *testing.T) {
	// 反例对照:同一渠道下,键与渠道默认值对得上的档表必须判为可命中 ——
	// 否则诊断只会报错、不会放行,同样不可用。
	row := rowOf("lec-gt-seedance-2-0-full", tiersOf(
		hosttypes.TierTypeResolution, hosttypes.BillingUnitSecond, "480p", "720p", "1080p"))

	reports := diagnoseTierRowForChannels(row, []int{constant.ChannelTypeOpenAI})

	require.Len(t, reports, 1)
	assert.False(t, reports[0].Problem, "键与渠道默认值对得上时不该报问题")
	assert.Equal(t, "resolved", reports[0].ResolvedStatus)
	assert.Equal(t, "720p", reports[0].ResolvedKey)
}

func TestDiagnoseTier_NoChannelReportsUnknownInsteadOfOK(t *testing.T) {
	// 没有渠道时不能假装体检通过 —— 报 unknown 并说明原因。
	row := rowOf("no-channel-model", tiersOf(
		hosttypes.TierTypeResolution, hosttypes.BillingUnitSecond, "720p"))

	reports := diagnoseTierRowForChannels(row, nil)

	require.Len(t, reports, 1)
	assert.Equal(t, "unknown", reports[0].ResolvedStatus)
	assert.False(t, reports[0].Problem)
	assert.Contains(t, reports[0].Verdict, "没有可用渠道")
}

func TestDiagnoseTier_OneRowPerChannelType(t *testing.T) {
	// 同一模型可路由到多个渠道时,每个渠道各出一份结论 —— 取值逻辑按渠道而异。
	row := rowOf("multi-channel-model", tiersOf(
		hosttypes.TierTypeResolution, hosttypes.BillingUnitSecond, "720p"))

	reports := diagnoseTierRowForChannels(row, []int{
		constant.ChannelTypeOpenAI, constant.ChannelTypeVidu,
	})

	require.Len(t, reports, 2)
	assert.Equal(t, constant.ChannelTypeOpenAI, reports[0].ChannelType)
	assert.Equal(t, constant.ChannelTypeVidu, reports[1].ChannelType)
}

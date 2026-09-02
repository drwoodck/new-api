package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTierKeyResolution(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"720p", "720p"},
		{"720P", "720p"},
		{"1080P", "1080p"},
		{"4K", "4k"},
		{"480p", "480p"},
		{"768p", "768p"},
	}
	for _, c := range cases {
		got, ok := NormalizeTierKey(TierTypeResolution, c.input)
		require.True(t, ok, "key %q must be valid", c.input)
		require.Equal(t, c.want, got)
	}
}

func TestNormalizeTierKeyImageSize(t *testing.T) {
	got, ok := NormalizeTierKey(TierTypeImageSize, "1k")
	require.True(t, ok)
	require.Equal(t, "1K", got)

	got, ok = NormalizeTierKey(TierTypeImageSize, "4K")
	require.True(t, ok)
	require.Equal(t, "4K", got)
}

func TestNormalizeTierKeyRequest(t *testing.T) {
	// 纯秒数归一化为 "Ns"
	got, ok := NormalizeTierKey(TierTypeRequest, "5")
	require.True(t, ok)
	require.Equal(t, "5s", got)

	got, ok = NormalizeTierKey(TierTypeRequest, "10s")
	require.True(t, ok)
	require.Equal(t, "10s", got)

	// 组合键（如 "720p-5s"）必须拒绝 —— 选档逻辑只构造纯时长键，
	// 组合键是永远选不中的死档
	_, ok = NormalizeTierKey(TierTypeRequest, "720p-5s")
	require.False(t, ok, "request 档组合键必须拒绝")
}

func TestNormalizeTierKeyRejectsEmptyAndUnknown(t *testing.T) {
	_, ok := NormalizeTierKey(TierTypeResolution, "  ")
	require.False(t, ok)

	_, ok = NormalizeTierKey("unknown_type", "x")
	require.False(t, ok)
}

func TestNormalizePriceTierListValid(t *testing.T) {
	tiers, err := NormalizePriceTierList([]PriceTier{
		{Label: "720P", TierType: TierTypeResolution, Key: "720P", BillingUnit: BillingUnitSecond, Price: 0.75},
		{Label: "1080P", TierType: TierTypeResolution, Key: "1080p", BillingUnit: BillingUnitSecond, Price: 1.55},
	})
	require.NoError(t, err)
	require.Len(t, tiers, 2)
	require.Equal(t, "720p", tiers[0].Key, "键必须归一化为小写")
	require.Equal(t, "1080p", tiers[1].Key)

	// 入参未被修改（不可变性）
	raw := []PriceTier{{Key: "720P", TierType: TierTypeResolution, Price: 1}}
	_, _ = NormalizePriceTierList(raw)
	require.Equal(t, "720P", raw[0].Key, "归一化不得修改入参")
}

func TestNormalizePriceTierListEmpty(t *testing.T) {
	tiers, err := NormalizePriceTierList(nil)
	require.NoError(t, err)
	require.Nil(t, tiers, "空档表必须归一化为 nil（未配置语义）")
}

func TestNormalizePriceTierListMixedTypesRejected(t *testing.T) {
	_, err := NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 1},
		{TierType: TierTypeRequest, Key: "5s", BillingUnit: BillingUnitRequest, Price: 1},
	})
	require.Error(t, err, "档表内 tier_type 必须一致")
}

func TestNormalizePriceTierListRejectsInvalid(t *testing.T) {
	_, err := NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: "bogus", Price: 1},
	})
	require.Error(t, err, "非法 billing_unit 必须拒绝")

	_, err = NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 0},
	})
	require.Error(t, err, "0 价格必须拒绝（防止付费模型变免费）")

	_, err = NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: -1},
	})
	require.Error(t, err, "负价格必须拒绝")

	_, err = NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 1},
		{TierType: TierTypeResolution, Key: "720P", BillingUnit: BillingUnitSecond, Price: 2},
	})
	require.Error(t, err, "归一化后重复的档位键必须拒绝")
}

func TestSelectTierKey(t *testing.T) {
	tiers := PriceTierList{
		{Key: "480p", Price: 0.45},
		{Key: "720p", Price: 0.75},
	}
	tier, ok := SelectTierKey(tiers, "720p")
	require.True(t, ok)
	require.InDelta(t, 0.75, tier.Price, 1e-9)

	_, ok = SelectTierKey(tiers, "1080p")
	require.False(t, ok)
}

func TestPriceTierListJSONRoundTrip(t *testing.T) {
	list := PriceTierList{
		{Label: "720P", TierType: "resolution", Key: "720p", BillingUnit: "second", Price: 0.75},
	}
	data, err := json.Marshal(list)
	require.NoError(t, err)
	require.JSONEq(t, `[{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]`, string(data))

	var back PriceTierList
	require.NoError(t, back.UnmarshalJSON(data))
	require.Len(t, back, 1)
	require.InDelta(t, 0.75, back[0].Price, 1e-9)
}

func TestPriceTierListUnmarshalToleratesStringified(t *testing.T) {
	var list PriceTierList
	err := list.UnmarshalJSON([]byte(`"[{\"key\":\"720p\",\"tier_type\":\"resolution\",\"billing_unit\":\"second\",\"price\":0.75}]"`))
	require.NoError(t, err, "被字符串化存储的档表 JSON 必须可读")
	require.Len(t, list, 1)
	require.Equal(t, "720p", list[0].Key)
}

// TestNormalizePriceTierListRejectsMixedBillingUnits 同表 billing_unit 必须一致：
// 结算重选档按"每秒单价"重算，混用会把 request 档价当秒价乘时长。
func TestNormalizePriceTierListRejectsMixedBillingUnits(t *testing.T) {
	_, err := NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 0.75},
		{TierType: TierTypeResolution, Key: "1080p", BillingUnit: BillingUnitRequest, Price: 3},
	})
	require.Error(t, err, "档表内 billing_unit 混用必须拒绝")
}

// TestNormalizePriceTierListRejectsOutOfRangePrice 价格上下界护栏：
// 过低单价预扣恒 0（配了价=免费），过高单价经倍率连乘让目录接口 500。
func TestNormalizePriceTierListRejectsOutOfRangePrice(t *testing.T) {
	_, err := NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 1e-9},
	})
	require.Error(t, err, "低于 MinTierPrice 必须拒绝")

	_, err = NormalizePriceTierList([]PriceTier{
		{TierType: TierTypeResolution, Key: "720p", BillingUnit: BillingUnitSecond, Price: 1e308},
	})
	require.Error(t, err, "高于 MaxTierPrice 必须拒绝")
}

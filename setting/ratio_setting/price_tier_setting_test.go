package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// resetVideoPriceTiers restores an empty map so tests do not leak state.
func resetVideoPriceTiers(t *testing.T) {
	t.Helper()
	require.NoError(t, UpdateVideoPriceTiersByJSONString("{}"))
}

// TestVideoPriceTiersDefaultIsEmpty guards the upgrade-safety invariant,
// mirroring TestVideoSecondPriceDefaultIsEmpty: a non-empty default would
// silently switch existing models to tiered billing on upgrade.
func TestVideoPriceTiersDefaultIsEmpty(t *testing.T) {
	require.Empty(t, defaultVideoPriceTiers,
		"tiered billing must be opt-in; a non-empty default changes existing billing on upgrade")
}

func TestGetVideoPriceTiersUnconfigured(t *testing.T) {
	resetVideoPriceTiers(t)

	tiers, ok := GetVideoPriceTiers("sora-2")
	require.False(t, ok, "unconfigured model must not report tiered billing")
	require.Nil(t, tiers)
}

func TestGetVideoPriceTiersConfiguredAndNormalized(t *testing.T) {
	resetVideoPriceTiers(t)
	require.NoError(t, UpdateVideoPriceTiersByJSONString(
		`{"sora-2":[{"label":"720P","tier_type":"resolution","key":"720P","billing_unit":"second","price":0.75},{"label":"1080P","tier_type":"resolution","key":"1080p","billing_unit":"second","price":1.55}]}`))
	t.Cleanup(func() { resetVideoPriceTiers(t) })

	tiers, ok := GetVideoPriceTiers("sora-2")
	require.True(t, ok)
	require.Len(t, tiers, 2)
	require.Equal(t, "720p", tiers[0].Key, "键必须归一化为小写")
	require.InDelta(t, 0.75, tiers[0].Price, 1e-9)
	require.Equal(t, types.BillingUnitSecond, tiers[0].BillingUnit)

	_, ok = GetVideoPriceTiers("some-other-model")
	require.False(t, ok)
}

func TestUpdateVideoPriceTiersDropsEmptyTables(t *testing.T) {
	resetVideoPriceTiers(t)
	require.NoError(t, UpdateVideoPriceTiersByJSONString(
		`{"empty-model":[],"real-model":[{"tier_type":"request","key":"5","billing_unit":"request","price":0.18}]}`))
	t.Cleanup(func() { resetVideoPriceTiers(t) })

	_, ok := GetVideoPriceTiers("empty-model")
	require.False(t, ok, "空档表条目必须视为未配置")

	tiers, ok := GetVideoPriceTiers("real-model")
	require.True(t, ok)
	require.Len(t, tiers, 1)
	require.Equal(t, "5s", tiers[0].Key, "request 档纯秒数键必须归一化为 Ns")
}

func TestUpdateVideoPriceTiersRejectsInvalidTable(t *testing.T) {
	resetVideoPriceTiers(t)

	// 非法档表整体拒绝，不部分生效
	err := UpdateVideoPriceTiersByJSONString(
		`{"good-model":[{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}],
		  "bad-model":[{"tier_type":"resolution","key":"720p","billing_unit":"second","price":0}]}`)
	require.Error(t, err)

	_, ok := GetVideoPriceTiers("good-model")
	require.False(t, ok, "校验失败时不得有任何条目生效")
}

func TestUpdateVideoPriceTiersRejectsInvalidJSON(t *testing.T) {
	resetVideoPriceTiers(t)
	require.Error(t, UpdateVideoPriceTiersByJSONString(`{"sora-2":`))
}

func TestVideoPriceTiersJSONRoundTrip(t *testing.T) {
	resetVideoPriceTiers(t)
	require.NoError(t, UpdateVideoPriceTiersByJSONString(
		`{"sora-2":[{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`))
	t.Cleanup(func() { resetVideoPriceTiers(t) })

	require.JSONEq(t,
		`{"sora-2":[{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}`,
		VideoPriceTiers2JSONString())

	copied := GetVideoPriceTiersCopy()
	require.Len(t, copied["sora-2"], 1)

	// Mutating the copy must not affect the live map.
	copied["sora-2"] = nil
	tiers, ok := GetVideoPriceTiers("sora-2")
	require.True(t, ok)
	require.Len(t, tiers, 1)
}

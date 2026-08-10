package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// resetVideoSecondPrice restores an empty map so tests do not leak state.
func resetVideoSecondPrice(t *testing.T) {
	t.Helper()
	require.NoError(t, UpdateVideoSecondPriceByJSONString("{}"))
}

// TestVideoSecondPriceDefaultIsEmpty guards the upgrade-safety invariant:
// shipping a non-empty default would silently switch existing video models to
// per-second billing and change customer bills without admin consent.
func TestVideoSecondPriceDefaultIsEmpty(t *testing.T) {
	require.Empty(t, defaultVideoSecondPrice,
		"per-second billing must be opt-in; a non-empty default changes existing billing on upgrade")
}

func TestGetVideoSecondPriceUnconfigured(t *testing.T) {
	resetVideoSecondPrice(t)

	price, ok := GetVideoSecondPrice("sora-2")

	require.False(t, ok, "unconfigured model must not report per-second billing")
	require.Zero(t, price)
}

func TestGetVideoSecondPriceConfigured(t *testing.T) {
	resetVideoSecondPrice(t)
	require.NoError(t, UpdateVideoSecondPriceByJSONString(`{"sora-2":0.1,"kling-v1-pro":0.05}`))
	t.Cleanup(func() { resetVideoSecondPrice(t) })

	price, ok := GetVideoSecondPrice("sora-2")
	require.True(t, ok)
	require.InDelta(t, 0.1, price, 1e-9)

	price, ok = GetVideoSecondPrice("kling-v1-pro")
	require.True(t, ok)
	require.InDelta(t, 0.05, price, 1e-9)

	_, ok = GetVideoSecondPrice("some-other-model")
	require.False(t, ok)
}

// TestGetVideoSecondPriceRejectsNonPositive ensures a zero or negative entry is
// treated as unconfigured instead of turning a paid model into a free one.
func TestGetVideoSecondPriceRejectsNonPositive(t *testing.T) {
	resetVideoSecondPrice(t)
	require.NoError(t, UpdateVideoSecondPriceByJSONString(`{"zero-model":0,"negative-model":-1}`))
	t.Cleanup(func() { resetVideoSecondPrice(t) })

	_, ok := GetVideoSecondPrice("zero-model")
	require.False(t, ok, "zero price must not enable per-second billing")

	_, ok = GetVideoSecondPrice("negative-model")
	require.False(t, ok, "negative price must not enable per-second billing")
}

func TestUpdateVideoSecondPriceRejectsInvalidJSON(t *testing.T) {
	resetVideoSecondPrice(t)

	require.Error(t, UpdateVideoSecondPriceByJSONString(`{"sora-2":`))
}

func TestVideoSecondPriceJSONRoundTrip(t *testing.T) {
	resetVideoSecondPrice(t)
	require.NoError(t, UpdateVideoSecondPriceByJSONString(`{"sora-2":0.1}`))
	t.Cleanup(func() { resetVideoSecondPrice(t) })

	require.JSONEq(t, `{"sora-2":0.1}`, VideoSecondPrice2JSONString())

	copied := GetVideoSecondPriceCopy()
	require.InDelta(t, 0.1, copied["sora-2"], 1e-9)

	// Mutating the copy must not affect the live map.
	copied["sora-2"] = 99
	price, ok := GetVideoSecondPrice("sora-2")
	require.True(t, ok)
	require.InDelta(t, 0.1, price, 1e-9)
}

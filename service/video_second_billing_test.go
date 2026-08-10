package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// perSecondTask seeds a task pre-charged for billedSeconds at secondPrice.
func perSecondTask(t *testing.T, secondPrice float64, billedSeconds float64, otherRatios map[string]float64) *model.Task {
	t.Helper()

	multiplier := billedSeconds
	for key, ratio := range otherRatios {
		if key != "seconds" {
			multiplier *= ratio
		}
	}
	preConsumed := common.QuotaFromFloat(secondPrice * common.QuotaPerUnit * multiplier)

	task := makeTask(1, 1, preConsumed, 1, BillingSourceWallet, 0)
	ratios := map[string]float64{"seconds": billedSeconds}
	for key, ratio := range otherRatios {
		ratios[key] = ratio
	}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		ModelPrice:      secondPrice,
		GroupRatio:      1.0,
		OriginModelName: "test-video-model",
		OtherRatios:     ratios,
		SecondPrice:     secondPrice,
	}
	require.NoError(t, task.Insert())
	return task
}

// TestSettleVideoSecondBillingRefundsShorterVideo covers the common case where
// the upstream returns a shorter clip than requested: the user must be refunded.
func TestSettleVideoSecondBillingRefundsShorterVideo(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-per-second-refund", 1_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 8, nil)
	preConsumed := task.Quota

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4})

	require.True(t, handled, "per-second tasks must not fall through to token settlement")
	expected := common.QuotaFromFloat(0.1 * common.QuotaPerUnit * 4)
	require.Equal(t, expected, task.Quota)
	require.Less(t, task.Quota, preConsumed, "shorter video must cost less")
}

// TestSettleVideoSecondBillingChargesLongerVideo covers the inverse: upstream
// produced more seconds than requested, so the delta must be charged.
func TestSettleVideoSecondBillingChargesLongerVideo(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-per-second-charge", 1_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 4, nil)
	preConsumed := task.Quota

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 6})

	require.True(t, handled)
	expected := common.QuotaFromFloat(0.1 * common.QuotaPerUnit * 6)
	require.Equal(t, expected, task.Quota)
	require.Greater(t, task.Quota, preConsumed, "longer video must cost more")
}

// TestSettleVideoSecondBillingPreservesOtherRatios ensures resolution-style
// multipliers captured at submit time still apply after re-settlement.
func TestSettleVideoSecondBillingPreservesOtherRatios(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 10_000_000)
	seedToken(t, 1, 1, "sk-per-second-ratios", 10_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 8, map[string]float64{"size": 1.5})

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4})

	require.True(t, handled)
	expected := common.QuotaFromFloat(0.1 * common.QuotaPerUnit * 4 * 1.5)
	require.Equal(t, expected, task.Quota)
}

// TestSettleVideoSecondBillingNoAdjustmentWhenEqual verifies an accurate
// pre-charge is left untouched (no spurious refund/charge log).
func TestSettleVideoSecondBillingNoAdjustmentWhenEqual(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-per-second-equal", 1_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 5, nil)
	preConsumed := task.Quota

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 5})

	require.True(t, handled)
	require.Equal(t, preConsumed, task.Quota)
}

// TestSettleVideoSecondBillingWithoutUpstreamDuration covers upstreams that do
// not report a duration (Vidu, Hailuo, Veo): keep the requested-duration charge.
func TestSettleVideoSecondBillingWithoutUpstreamDuration(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-per-second-nodur", 1_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 5, nil)
	preConsumed := task.Quota

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 0})

	require.True(t, handled, "still owned by per-second billing; token settlement must not run")
	require.Equal(t, preConsumed, task.Quota, "charge stays at the requested duration")
}

// TestSettleVideoSecondBillingClampsUpstreamDuration guards against a
// hostile/buggy upstream reporting an absurd duration as a billing multiplier.
func TestSettleVideoSecondBillingClampsUpstreamDuration(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000_000)
	seedToken(t, 1, 1, "sk-per-second-clamp", 1_000_000_000)
	seedChannel(t, 1)

	task := perSecondTask(t, 0.1, 5, nil)

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 999_999_999})

	require.True(t, handled)
	capped := common.QuotaFromFloat(
		0.1 * common.QuotaPerUnit * float64(relaycommon.MaxTaskDurationSeconds))
	require.Equal(t, capped, task.Quota)
	require.Greater(t, task.Quota, 0, "clamped quota must stay positive, not overflow negative")
}

// TestSettleVideoSecondBillingSkipsNonVideoTasks confirms ordinary tasks still
// reach the adaptor/token settlement path.
func TestSettleVideoSecondBillingSkipsNonVideoTasks(t *testing.T) {
	truncate(t)

	task := makeTask(1, 1, 1000, 1, BillingSourceWallet, 0)
	require.Zero(t, task.PrivateData.BillingContext.SecondPrice)

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4})

	require.False(t, handled, "non per-second tasks must fall through")

	task.PrivateData.BillingContext = nil
	require.False(t, settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4}))
}

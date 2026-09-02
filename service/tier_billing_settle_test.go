package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quotaFor 把"美元"换算成额度，与按秒结算公式一致（GroupRatio=1）。
func quotaFor(usdPerSecond float64, seconds int) int {
	q, _ := common.QuotaFromFloatChecked(usdPerSecond * common.QuotaPerUnit * float64(seconds))
	return q
}

// tierSnapshotTiers 是测试用的三档分辨率快照（原价美元/秒）。
func tierSnapshotTiers() types.PriceTierList {
	return types.PriceTierList{
		{Key: "480p", TierType: types.TierTypeResolution, BillingUnit: types.BillingUnitSecond, Price: 0.45},
		{Key: "720p", TierType: types.TierTypeResolution, BillingUnit: types.BillingUnitSecond, Price: 0.75},
		{Key: "1080p", TierType: types.TierTypeResolution, BillingUnit: types.BillingUnitSecond, Price: 1.55},
	}
}

// tierKeyForPrice 反查预扣档位键（测试辅助，快照价格唯一）。
func tierKeyForPrice(price float64) string {
	for _, tier := range tierSnapshotTiers() {
		if tier.Price == price {
			return tier.Key
		}
	}
	panic("no tier with price " + string(rune(price)))
}

// newTierSettleTask 构造档位计费（second 档）任务：预扣 10 秒 × secondPriceUSD。
func newTierSettleTask(t *testing.T, userID, channelID, tokenID int, secondPriceUSD float64) *model.Task {
	t.Helper()
	task := makeTask(userID, channelID, quotaFor(secondPriceUSD, 10), tokenID, BillingSourceWallet, 0)
	bc := task.PrivateData.BillingContext
	bc.SecondPrice = secondPriceUSD
	bc.GroupRatio = 1.0
	bc.OtherRatios = map[string]float64{"seconds": 10}
	bc.TierBilling = true
	bc.TierType = types.TierTypeResolution
	bc.TierKey = tierKeyForPrice(secondPriceUSD)
	snapshot := tierSnapshotTiers()
	bc.TierSnapshot = &snapshot
	return task
}

// TestSettle_TierBilling_UpscalesWhenActualResolutionHigher 实际升档（720p→1080p）
// 且时长一致：必须补扣档价差额，不能因时长一致提前结束。
func TestSettle_TierBilling_UpscalesWhenActualResolutionHigher(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 40, 40, 40
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-upscale", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10, // 与请求时长一致 —— 只有档变了
		Resolution:      "1080p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	actualQuota := quotaFor(1.55, 10)
	delta := actualQuota - preConsumed
	assert.Equal(t, initQuota-delta, getUserQuota(t, userID), "升档必须补扣差额")
	assert.Equal(t, initQuota-delta, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, task.Quota)
}

// TestSettle_TierBilling_DownscalesWhenActualResolutionLower 实际降档（1080p→480p）：退还差额。
func TestSettle_TierBilling_DownscalesWhenActualResolutionLower(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 41, 41, 41
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-downscale", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 1.55)
	preConsumed := task.Quota

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10,
		Resolution:      "480p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	actualQuota := quotaFor(0.45, 10)
	delta := actualQuota - preConsumed // 负数 → 退还
	assert.Equal(t, initQuota-delta, getUserQuota(t, userID), "降档必须退还差额")
	assert.Equal(t, actualQuota, task.Quota)
}

// TestSettle_TierBilling_MissingActualTierKeepsPreConsumed 实际分辨率不在快照档表中
// （768p）：保持预扣档，时长一致 → 无任何调整。
func TestSettle_TierBilling_MissingActualTierKeepsPreConsumed(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 42, 42, 42
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-missing", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10,
		Resolution:      "768p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	assert.Equal(t, initQuota, getUserQuota(t, userID), "快照缺档必须保持预扣额")
	assert.Equal(t, preConsumed, task.Quota)
}

// TestSettle_TierBilling_NoActualResolutionKeepsPreConsumed 上游未返回实际分辨率：
// 回退请求档（行为与"未返回实际时长"一致）。
func TestSettle_TierBilling_NoActualResolutionKeepsPreConsumed(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 43, 43, 43
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-nores", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10,
		// Resolution 为空
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
}

// TestSettle_TierBilling_ActualDurationDiffersWithoutTierChange 实际时长不同但档位没变：
// 仍按实际时长差额结算。
func TestSettle_TierBilling_ActualDurationDiffersWithoutTierChange(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 44, 44, 44
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-duration", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 8, // 实际 8 秒，请求 10 秒
		Resolution:      "720p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	actualQuota := quotaFor(0.75, 8)
	delta := actualQuota - preConsumed
	assert.Equal(t, initQuota-delta, getUserQuota(t, userID), "时长差异必须差额结算")
	assert.Equal(t, actualQuota, task.Quota)
}

// TestSettle_LegacySecondPriceTaskUnaffected 旧任务（无 TierBilling）走既有按秒路径。
func TestSettle_LegacySecondPriceTaskUnaffected(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 45, 45, 45
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-legacy-second", initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, quotaFor(0.1, 5), tokenID, BillingSourceWallet, 0)
	bc := task.PrivateData.BillingContext
	bc.SecondPrice = 0.1
	bc.OtherRatios = map[string]float64{"seconds": 5}
	bc.TierBilling = false // 旧任务

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 7, // 实际 7 秒
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	actualQuota := quotaFor(0.1, 7)
	assert.Equal(t, actualQuota, task.Quota, "旧按秒任务照常按实际时长结算")
	require.NotNil(t, getLastLog(t))
}

// TestSettle_TierBilling_DropsTierDimensionRatios 结算侧同样剔除分辨率维度
// 倍率键(size/resolution):档价已按分辨率分档,再乘就是双计。
func TestSettle_TierBilling_DropsTierDimensionRatios(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 46, 46, 46
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-dims", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota
	// 模拟预扣侧漏入的旧维度键(结算侧必须防御性剔除)
	task.PrivateData.BillingContext.OtherRatios["size"] = 2.0
	task.PrivateData.BillingContext.OtherRatios["resolution"] = 2.0

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10,
		Resolution:      "720p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// 时长一致、档位未变 → 剔除维度键后无差额
	assert.Equal(t, initQuota, getUserQuota(t, userID), "维度倍率键不得参与结算重算")
	assert.Equal(t, preConsumed, task.Quota)
}

// TestSettle_TierBilling_RequestUnitTierKeepsPreConsumed 防御性护栏:快照重选档
// 若命中 request 单位档(历史脏数据),不得把按次价当秒价乘时长。
func TestSettle_TierBilling_RequestUnitTierKeepsPreConsumed(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 47, 47, 47
	const initQuota = 100_000_000
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-tier-mixed-unit", initQuota)
	seedChannel(t, channelID)

	task := newTierSettleTask(t, userID, channelID, tokenID, 0.75)
	preConsumed := task.Quota
	// 脏数据:快照里 1080p 是 request 单位(按次价 3 元)
	dirty := types.PriceTierList{
		{Key: "720p", TierType: types.TierTypeResolution, BillingUnit: types.BillingUnitSecond, Price: 0.75},
		{Key: "1080p", TierType: types.TierTypeResolution, BillingUnit: types.BillingUnitRequest, Price: 3},
	}
	task.PrivateData.BillingContext.TierSnapshot = &dirty

	adaptor := &mockAdaptor{}
	taskResult := &relaycommon.TaskInfo{
		Status:          model.TaskStatusSuccess,
		DurationSeconds: 10,
		Resolution:      "1080p",
	}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// request 单位档不得被当秒价:保持预扣档 720p 结算,时长一致无差额
	assert.Equal(t, initQuota, getUserQuota(t, userID), "request 单位档不得当秒价乘时长")
	assert.Equal(t, preConsumed, task.Quota)
}

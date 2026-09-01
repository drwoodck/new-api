package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTaskWithBillingContext 与 makeTask 同构,但允许自定义 BillingContext——
// RecalculateTaskQuotaByTokens 的分组分别定价分支只读 BillingContext 里的
// ModelRatio/GroupRatio,不重新查 ratio_setting,所以测试需要精确控制这两个值,
// 而不是复用 makeTask 里硬编码的 ModelPrice/GroupRatio 组合。
func makeTaskWithBillingContext(userId, channelId, quota, tokenId int, bc *model.TaskBillingContext) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: bc.OriginModelName,
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  BillingSourceWallet,
			TokenId:        tokenId,
			BillingContext: bc,
		},
	}
}

func insertGroupPricedModelForTaskBilling(t *testing.T, name string) {
	t.Helper()
	m := &model.Model{ModelName: name, Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
}

// TestRecalculateTaskQuotaByTokensGroupPricingUsesBillingContextNotGlobalRatio
// 锁住这次修复本身:分组分别定价模式下,重算必须读 BillingContext 里
// 预扣阶段已经存好的 ModelRatio/GroupRatio,不能重新查全局 ratio_setting ——
// 全局表里故意留空(甚至塞一个明显不同的值),如果实现有 bug 退回去查全局表,
// 断言会用错误的 actualQuota 抓到。
func TestRecalculateTaskQuotaByTokensGroupPricingUsesBillingContextNotGlobalRatio(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 2000
	const tokenRemain = 5000

	insertGroupPricedModelForTaskBilling(t, "gp-task-model")

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-gp", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	// BillingContext 里存的是分别定价模式下预扣阶段已经解析好的倍率
	// (ModelRatio=2, GroupRatio=1——分别定价恒为 1,已确认的既有约定)。
	task := makeTaskWithBillingContext(userID, channelID, preConsumed, tokenID, &model.TaskBillingContext{
		ModelRatio:      2.0,
		GroupRatio:      1.0,
		OriginModelName: "gp-task-model",
		PerCallBilling:  false,
	})

	const totalTokens = 2000 // 2000 * 2.0 * 1.0 = 4000,比预扣的 2000 多扣 2000
	RecalculateTaskQuotaByTokens(ctx, task, totalTokens)

	const wantActualQuota = 4000
	assert.Equal(t, wantActualQuota, task.Quota, "必须用 BillingContext 里的倍率算,不是全局表(全局表里这个模型压根没配置)")
	assert.Equal(t, initQuota-(wantActualQuota-preConsumed), getUserQuota(t, userID))
}

// TestRecalculateTaskQuotaByTokensGroupPricingZeroRatioSkipsRecalc 锁住早退出
// 条件:BillingContext.ModelRatio<=0 时必须直接跳过,不产生任何扣费改动
// (与统一模式下 hasRatioSetting=false 时的早退行为对齐)。
func TestRecalculateTaskQuotaByTokensGroupPricingZeroRatioSkipsRecalc(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 2000

	insertGroupPricedModelForTaskBilling(t, "gp-task-model-zero")

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-gp-zero", 5000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTaskWithBillingContext(userID, channelID, preConsumed, tokenID, &model.TaskBillingContext{
		ModelRatio:      0,
		GroupRatio:      1.0,
		OriginModelName: "gp-task-model-zero",
	})

	RecalculateTaskQuotaByTokens(ctx, task, 2000)

	assert.Equal(t, preConsumed, task.Quota, "ModelRatio<=0 必须跳过重算,预扣额度保持不变")
	assert.Equal(t, initQuota, getUserQuota(t, userID), "跳过重算意味着不应有任何额外扣费")
}

// TestRecalculateTaskQuotaByTokensUnifiedModeUnaffectedByGroupPricingModel 锁住
// "只影响开了分别定价的模型" —— 一个没开分别定价的模型,即便 DB 里同时存在
// 另一个开了分别定价的模型,也必须继续走原来的全局 ratio_setting 路径。
func TestRecalculateTaskQuotaByTokensUnifiedModeUnaffectedByGroupPricingModel(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 2000

	// DB 里存在一个开了分别定价的模型(sibling),但本测试用的模型不是它。
	insertGroupPricedModelForTaskBilling(t, "gp-sibling-unrelated")

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-unified", 5000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// makeTask 固定用 "test-model",没有在 model_group_price 或 Model 表里配置
	// GroupPricingEnabled——IsGroupPricingEnabled 对它必须返回 false。

	RecalculateTaskQuotaByTokens(ctx, task, 2000)

	// "test-model" 在全局 ratio_setting 里没有配置任何倍率,hasRatioSetting=false,
	// 走统一模式的既有早退——预扣额度不变,与开着分别定价的 sibling 模型无关。
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
}

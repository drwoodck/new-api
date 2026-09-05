package service

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMaterialDuration 把一条时长直接灌进探测缓存,模拟后台补探测的成果,
// 供结算修正(RefreshMaterialDurationsAtSettle)消费。
func seedMaterialDuration(t *testing.T, url string, seconds float64) {
	t.Helper()
	sum := sha256.Sum256([]byte(url))
	mediaDurationCache.Store(string(sum[:]), cachedMediaDuration{
		duration: seconds,
		expireAt: time.Now().Add(mediaProbeCacheTTL),
	})
}

// TestSettleVideoSecondBillingFreezesMaterialQuota 钉住结算侧素材冻结:按秒
// 重算出的额度只含生成费,终值必须 = 生成结算 + 冻结素材费,否则差额结算会把
// 素材费整笔退掉。
func TestSettleVideoSecondBillingFreezesMaterialQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-material-freeze", 1_000_000)
	seedChannel(t, 1)

	const materialQuota = 100
	task := perSecondTask(t, 0.1, 8, nil)
	bc := task.PrivateData.BillingContext
	bc.MaterialQuota = materialQuota
	bc.Materials = []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeImage, URL: "https://example.invalid/a.png", UnitPrice: 0.0002, Source: materialSourceExplicit},
	}
	task.Quota += materialQuota // 提交链路已把素材费随预扣入账
	preConsumed := task.Quota

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4})

	require.True(t, handled)
	expected := common.QuotaFromFloat(0.1*common.QuotaPerUnit*4) + materialQuota
	require.Equal(t, expected, task.Quota)
	require.Less(t, task.Quota, preConsumed, "短视频仍应退生成差额")
	require.Equal(t, materialQuota, bc.MaterialQuota, "无可修正条目时冻结素材费保持不变")
}

// TestSettleVideoSecondBillingCorrectsMaterialFromProbeCache 钉住探测修正:
// 提交时按估算(60s)计的素材,后台补探测把真值(4s)灌进缓存后,结算按提交侧
// 同公式重算素材费,修正量并入同一笔差额,清单来源标记 settle_corrected,
// 结算日志 reason 带出素材修正供对账审计。
func TestSettleVideoSecondBillingCorrectsMaterialFromProbeCache(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1_000_000)
	seedToken(t, 1, 1, "sk-material-correct", 1_000_000)
	seedChannel(t, 1)

	const refURL = "https://media.example.invalid/settle-ref.wav"
	seedMaterialDuration(t, refURL, 4)

	estimatedMaterial := common.QuotaFromFloat(60 * 0.001 * common.QuotaPerUnit)
	require.Equal(t, 30000, estimatedMaterial)

	task := perSecondTask(t, 0.1, 8, nil)
	bc := task.PrivateData.BillingContext
	bc.MaterialQuota = estimatedMaterial
	bc.Materials = []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeAudio, URL: refURL, Seconds: 60, UnitPrice: 0.001, Source: materialSourceEstimated},
	}
	task.Quota += estimatedMaterial

	handled := settleVideoSecondBilling(context.Background(), task,
		&relaycommon.TaskInfo{DurationSeconds: 4})

	require.True(t, handled)
	correctedMaterial := common.QuotaRound(4 * 0.001 * bc.GroupRatio * common.QuotaPerUnit)
	require.Equal(t, 2000, correctedMaterial)
	require.Equal(t, correctedMaterial, bc.MaterialQuota, "素材费按缓存真值重算")
	require.InDelta(t, 4.0, bc.Materials[0].Seconds, 1e-9)
	require.Equal(t, materialSourceSettleCorrected, bc.Materials[0].Source)

	expected := common.QuotaFromFloat(0.1*common.QuotaPerUnit*4) + correctedMaterial
	require.Equal(t, expected, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Contains(t, log.Content, "素材修正")
}

// TestSettleTaskBillingAdaptorAdjustKeepsMaterialQuota 钉住 adaptor 调整路径:
// adaptor 返回的生成额度必须加上冻结素材费,素材不能在差额结算中丢失。
func TestSettleTaskBillingAdaptorAdjustKeepsMaterialQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 10_000_000)
	seedToken(t, 1, 1, "sk-material-adaptor", 10_000_000)
	seedChannel(t, 1)

	const materialQuota = 100
	const generationQuota = 5000
	task := makeTask(1, 1, generationQuota+materialQuota, 1, BillingSourceWallet, 0)
	bc := task.PrivateData.BillingContext
	bc.SecondPrice = 0 // 非按秒计费:走 adaptor/token 路径
	bc.MaterialQuota = materialQuota
	bc.Materials = []types.ResolvedInputMaterial{
		{MaterialType: types.MaterialTypeImage, URL: "https://example.invalid/a.png", UnitPrice: 0.0002, Source: materialSourceExplicit},
	}

	settleTaskBillingOnComplete(context.Background(), &mockAdaptor{adjustReturn: 3000}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess})

	require.Equal(t, 3000+materialQuota, task.Quota, "adaptor 生成额度 + 素材费")
	assert.Equal(t, 10_000_000+(generationQuota+materialQuota)-(3000+materialQuota), getUserQuota(t, 1))
}

// TestSettleTaskBillingTokenRecalcKeepsMaterialQuota 钉住 token 重算路径:
// RecalculateTaskQuotaByTokens 算出的生成额度必须加上冻结素材费。
func TestSettleTaskBillingTokenRecalcKeepsMaterialQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 10_000_000)
	seedToken(t, 1, 1, "sk-material-token", 10_000_000)
	seedChannel(t, 1)

	const modelName = "test-material-token-model"
	ratiosBefore := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratiosBefore))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+modelName+`":2}`))

	const materialQuota = 100
	task := makeTask(1, 1, 5000+materialQuota, 1, BillingSourceWallet, 0)
	bc := task.PrivateData.BillingContext
	bc.OriginModelName = modelName
	bc.SecondPrice = 0
	bc.MaterialQuota = materialQuota
	task.Properties.OriginModelName = modelName

	settleTaskBillingOnComplete(context.Background(), &mockAdaptor{}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 1000})

	// token 重算:1000 tokens × modelRatio 2 × groupRatio 1 × otherMultiplier 1
	// = 20000,加回素材费 100
	expected := common.QuotaFromFloat(1000*2*1*1) + materialQuota
	require.Equal(t, expected, task.Quota)
}

// TestSettleTaskBillingTokenRecalcSaturatesMaterialQuota 钉住饱和审计:素材费
// 与生成费相加溢出时,终值饱和到 MaxQuota 而非回绕,且钳制事件经
// RecalculateTaskQuota 落进 task billing log 的 admin_info.quota_saturation。
func TestSettleTaskBillingTokenRecalcSaturatesMaterialQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 10_000_000)
	seedToken(t, 1, 1, "sk-material-saturate", 10_000_000)
	seedChannel(t, 1)

	const modelName = "test-material-saturate-model"
	ratiosBefore := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratiosBefore))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+modelName+`":2}`))

	task := makeTask(1, 1, 5100, 1, BillingSourceWallet, 0)
	bc := task.PrivateData.BillingContext
	bc.OriginModelName = modelName
	bc.SecondPrice = 0
	bc.MaterialQuota = common.MaxQuota
	task.Properties.OriginModelName = modelName

	settleTaskBillingOnComplete(context.Background(), &mockAdaptor{}, task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 1000})

	// 生成 20000 + 素材 MaxQuota 溢出 → 饱和到 MaxQuota
	require.Equal(t, common.MaxQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]interface{}
	require.NoError(t, common.Unmarshal([]byte(log.Other), &other))
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok, "admin_info missing in log other: %v", other)
	saturation, ok := adminInfo["quota_saturation"].(map[string]interface{})
	require.True(t, ok, "quota_saturation missing in admin_info: %v", adminInfo)
	assert.Equal(t, "AddQuotaSaturating", saturation["op"])
}

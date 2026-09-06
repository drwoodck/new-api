package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// upstreamSyncFixture 是同步/prefetch 测试用的 type2(/api/pricing)响应:
// 普通倍率模型 + quota_type=1 价格模型 + 37.5 哨兵模型 + 非法档表模型 +
// 全字段模型(档表+秒价+billing+描述)+ 越界价格模型。
const upstreamSyncFixture = `{
  "success": true,
  "data": [
    {
      "model_name": "sync-ratio-model",
      "quota_type": 0,
      "model_ratio": 1.5,
      "completion_ratio": 2.0,
      "model_price": 0,
      "cache_ratio": 0.1,
      "description": "sync ratio desc"
    },
    {
      "model_name": "sync-price-model",
      "quota_type": 1,
      "model_price": 0.25,
      "model_ratio": 0,
      "completion_ratio": 0
    },
    {
      "model_name": "sync-sentinel-model",
      "quota_type": 0,
      "model_ratio": 37.5,
      "completion_ratio": 1,
      "model_price": 0,
      "description": "sentinel desc"
    },
    {
      "model_name": "sync-bad-tier-model",
      "quota_type": 0,
      "model_ratio": 2.5,
      "completion_ratio": 1.5,
      "model_price": 0,
      "price_tiers": [
        {"label": "a", "tier_type": "resolution", "key": "720p", "billing_unit": "second", "price": 0.002},
        {"label": "b", "tier_type": "request", "key": "5s", "billing_unit": "second", "price": 0.002}
      ],
      "description": "bad tier desc"
    },
    {
      "model_name": "sync-full-model",
      "quota_type": 0,
      "model_ratio": 1.0,
      "completion_ratio": 3.0,
      "model_price": 0,
      "cache_ratio": 0.2,
      "create_cache_ratio": 1.25,
      "image_ratio": 1.2,
      "audio_ratio": 0.8,
      "audio_completion_ratio": 1.6,
      "video_second_price": 0.02,
      "price_tiers": [
        {"label": "720P", "tier_type": "resolution", "key": "720p", "billing_unit": "second", "price": 0.002}
      ],
      "billing_mode": "tiered_expr",
      "billing_expr": "tier(\"base\", p * 2.5 + c * 15)",
      "description": "full sync model"
    },
    {
      "model_name": "sync-invalid-price-model",
      "quota_type": 0,
      "model_ratio": 2000000,
      "completion_ratio": 1,
      "model_price": 0
    }
  ]
}`

// setupOnboardingPrefetchTest 初始化 sqlite 内存库 + 进程级全局状态隔离:
// 全部价格 RWMap、billing_setting 两表、prefetch 短缓存、自用模式开关、
// OptionMap。返回 db 供测试 seed channel/models/group price 行。
func setupOnboardingPrefetchTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB

	prevModelPrice := ratio_setting.ModelPrice2JSONString()
	prevModelRatio := ratio_setting.ModelRatio2JSONString()
	prevCompletionRatio := ratio_setting.CompletionRatio2JSONString()
	prevCacheRatio := ratio_setting.CacheRatio2JSONString()
	prevCreateCacheRatio := ratio_setting.CreateCacheRatio2JSONString()
	prevImageRatio := ratio_setting.ImageRatio2JSONString()
	prevAudioRatio := ratio_setting.AudioRatio2JSONString()
	prevAudioCompletionRatio := ratio_setting.AudioCompletionRatio2JSONString()
	prevVideoSecond := ratio_setting.VideoSecondPrice2JSONString()
	prevVideoTiers := ratio_setting.VideoPriceTiers2JSONString()
	prevBillingMode := billing_setting.GetBillingModeCopy()
	prevBillingExpr := billing_setting.GetBillingExprCopy()
	prevSelfUse := operation_setting.SelfUseModeEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Model{}, &model.Option{}, &model.ModelGroupPrice{}))
	model.DB = db

	// 生产环境由 model.InitOptionMap 初始化,单测先保证非 nil,否则
	// model.UpdateOption 写 option 时会 panic。
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()

	// 清空价格表 + billing 两表 + prefetch 缓存,从干净状态出发。
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString("{}"))
	require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString("{}"))
	// billing 两表同样清零,避免被其它测试遗留的 tiered_expr 污染。
	require.NoError(t, model.UpdateOption("billing_setting.billing_mode", "{}"))
	require.NoError(t, model.UpdateOption("billing_setting.billing_expr", "{}"))
	operation_setting.SelfUseModeEnabled = false
	model.DB.Exec("DELETE FROM options")

	t.Cleanup(func() {
		clearUpstreamPrefetchCache()
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prevModelPrice))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(prevModelRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(prevCompletionRatio))
		require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(prevCacheRatio))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(prevCreateCacheRatio))
		require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(prevImageRatio))
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(prevAudioRatio))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(prevAudioCompletionRatio))
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(prevVideoSecond))
		require.NoError(t, ratio_setting.UpdateVideoPriceTiersByJSONString(prevVideoTiers))
		require.NoError(t, model.UpdateOption("billing_setting.billing_mode", mustJSON(t, prevBillingMode)))
		require.NoError(t, model.UpdateOption("billing_setting.billing_expr", mustJSON(t, prevBillingExpr)))
		operation_setting.SelfUseModeEnabled = prevSelfUse
		model.DB = originalDB
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

func clearUpstreamPrefetchCache() {
	upstreamPrefetchCache.Range(func(k, _ any) bool {
		upstreamPrefetchCache.Delete(k)
		return true
	})
}

func seedOnboardingChannel(t *testing.T, db *gorm.DB, id int, baseURL string) {
	t.Helper()
	baseURL = strings.TrimRight(baseURL, "/")
	channel := model.Channel{Id: id, Type: 1, Name: fmt.Sprintf("ch-%d", id), BaseURL: &baseURL, Status: 1}
	require.NoError(t, db.Create(&channel).Error)
}

func sortedEntryKeys(m map[string]UpstreamPrefetchEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func appliedFields(changes []FieldChange) []string {
	fields := make([]string, 0, len(changes))
	for _, c := range changes {
		fields = append(fields, c.Field)
	}
	return fields
}

func skippedFields(skips []FieldSkip) []string {
	fields := make([]string, 0, len(skips))
	for _, s := range skips {
		fields = append(fields, s.Field)
	}
	return fields
}

// TestOnboardingPrefetchMarksEntries 钉住 prefetch:模型名过滤、suspicious/valid
// 标记、坏条目(越界价格)标 valid=false 且不阻塞其它条目。
func TestOnboardingPrefetchMarksEntries(t *testing.T) {
	db := setupOnboardingPrefetchTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/pricing", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamSyncFixture))
	}))
	defer srv.Close()
	seedOnboardingChannel(t, db, 8101, srv.URL)

	entries, source, err := PrefetchUpstreamPricing(context.Background(), 8101,
		[]string{"sync-ratio-model", "sync-sentinel-model", "sync-invalid-price-model"})
	require.NoError(t, err)
	assert.Equal(t, strings.TrimRight(srv.URL, "/"), source)

	// 只返回请求的模型(按字典序)。
	require.Equal(t, []string{"sync-invalid-price-model", "sync-ratio-model", "sync-sentinel-model"}, sortedEntryKeys(entries))

	// 正常条目 valid=true、非可疑。
	ratio := entries["sync-ratio-model"]
	assert.True(t, ratio.Valid)
	assert.False(t, ratio.Suspicious)
	assert.Equal(t, 1.5, ratio.ModelRatio)
	assert.Equal(t, "sync ratio desc", ratio.Description)

	// 37.5 哨兵条目 valid=true(数值合法)但 suspicious=true。
	sentinel := entries["sync-sentinel-model"]
	assert.True(t, sentinel.Valid)
	assert.True(t, sentinel.Suspicious)

	// 越界价格条目 valid=false 并带 error,不阻塞同响应其它条目。
	invalid := entries["sync-invalid-price-model"]
	assert.False(t, invalid.Valid)
	assert.Contains(t, invalid.Error, "超出合法区间")
}

// TestOnboardingPrefetchShortCache 钉住 60s 短缓存:同 channel+同模型名单命中缓存
// 不再外呼;不同模型名单构成不同缓存键,重新拉取。
func TestOnboardingPrefetchShortCache(t *testing.T) {
	db := setupOnboardingPrefetchTest(t)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamSyncFixture))
	}))
	defer srv.Close()
	seedOnboardingChannel(t, db, 8102, srv.URL)

	const model = "sync-ratio-model"
	first, _, err := PrefetchUpstreamPricing(context.Background(), 8102, []string{model})
	require.NoError(t, err)
	require.Contains(t, first, model)
	require.Equal(t, 1, requests, "首次拉取应命中上游")

	second, _, err := PrefetchUpstreamPricing(context.Background(), 8102, []string{model})
	require.NoError(t, err)
	require.Contains(t, second, model)
	require.Equal(t, 1, requests, "60s 内重复拉取应命中短缓存")

	other, _, err := PrefetchUpstreamPricing(context.Background(), 8102, []string{"sync-sentinel-model"})
	require.NoError(t, err)
	require.Contains(t, other, "sync-sentinel-model")
	require.Equal(t, 2, requests, "不同模型名单应重新拉取")
}

// TestApplyUpstreamEntryRatioAndDescription 钉住直接应用:倍率变化写入后
// GetModelRatio 变化、description 进 models 表(无行则建 status=1)、分组独立价
// 不被触碰、applied 携带 old/new。
func TestApplyUpstreamEntryRatioAndDescription(t *testing.T) {
	db := setupOnboardingPrefetchTest(t)

	// 预置:该模型已有倍率 5.0;分组价行存在。
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sync-ratio-model":5.0}`))
	groupPrice := 1.0
	require.NoError(t, db.Create(&model.ModelGroupPrice{
		ModelName: "sync-ratio-model", GroupName: "default", ModelPrice: &groupPrice,
	}).Error)

	entry := UpstreamModelPricing{
		ModelName:       "sync-ratio-model",
		QuotaType:       0,
		ModelRatio:      1.5,
		CompletionRatio: 2.0,
		Description:     "sync ratio desc",
	}
	applied, skipped, err := ApplyUpstreamEntryToSettings(&entry)
	require.NoError(t, err)
	require.Empty(t, skipped)
	assert.ElementsMatch(t, []string{"model_ratio", "completion_ratio", "description"}, appliedFields(applied))

	ratio, ok, _ := ratio_setting.GetModelRatio("sync-ratio-model")
	assert.True(t, ok)
	assert.InDelta(t, 1.5, ratio, 1e-9)

	var m model.Model
	require.NoError(t, db.Where("model_name = ?", "sync-ratio-model").First(&m).Error)
	assert.Equal(t, "sync ratio desc", m.Description)
	assert.Equal(t, 1, m.Status, "无 models 行时应新建 status=1 的 meta 行")

	// 分组独立价不被触碰。
	var gp model.ModelGroupPrice
	require.NoError(t, db.Where("model_name = ? AND group_name = ?", "sync-ratio-model", "default").First(&gp).Error)
	require.NotNil(t, gp.ModelPrice)
	assert.InDelta(t, 1.0, *gp.ModelPrice, 1e-9)

	// old/new 记录:model_ratio old=5.0 new=1.5;description old=nil(unset)。
	for _, c := range applied {
		switch c.Field {
		case "model_ratio":
			assert.InDelta(t, 5.0, c.Old.(float64), 1e-9)
			assert.InDelta(t, 1.5, c.New.(float64), 1e-9)
		case "description":
			assert.Nil(t, c.Old)
			assert.Equal(t, "sync ratio desc", c.New)
		}
	}
}

// TestApplyUpstreamEntryQuotaTypePrice 钉住 quota_type==1 写 ModelPrice map,
// 不再写倍率。
func TestApplyUpstreamEntryQuotaTypePrice(t *testing.T) {
	setupOnboardingPrefetchTest(t)

	entry := UpstreamModelPricing{
		ModelName:   "sync-price-model",
		QuotaType:   1,
		ModelPrice:  0.25,
		Description: "price model desc",
	}
	applied, skipped, err := ApplyUpstreamEntryToSettings(&entry)
	require.NoError(t, err)
	require.Empty(t, skipped)
	assert.ElementsMatch(t, []string{"model_price", "description"}, appliedFields(applied))

	price, ok := ratio_setting.GetModelPrice("sync-price-model", false)
	assert.True(t, ok)
	assert.InDelta(t, 0.25, price, 1e-9)

	_, okRatio, _ := ratio_setting.GetModelRatio("sync-price-model")
	assert.False(t, okRatio, "quota_type==1 不得写倍率 map")
}

// TestApplyUpstreamEntrySkipsInvalidTierTable 钉住逐字段独立:非法档表 skip,
// 同条目其它字段(model_ratio/completion_ratio/description)仍应用。
func TestApplyUpstreamEntrySkipsInvalidTierTable(t *testing.T) {
	setupOnboardingPrefetchTest(t)

	entry := UpstreamModelPricing{
		ModelName:       "sync-bad-tier-model",
		QuotaType:       0,
		ModelRatio:      2.5,
		CompletionRatio: 1.5,
		PriceTiers: &types.PriceTierList{
			{Label: "a", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 0.002},
			{Label: "b", TierType: types.TierTypeRequest, Key: "5s", BillingUnit: types.BillingUnitSecond, Price: 0.002},
		},
		Description: "bad tier desc",
	}
	applied, skipped, err := ApplyUpstreamEntryToSettings(&entry)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"model_ratio", "completion_ratio", "description"}, appliedFields(applied))
	require.Len(t, skipped, 1)
	assert.Equal(t, "price_tiers", skipped[0].Field)

	ratio, ok, _ := ratio_setting.GetModelRatio("sync-bad-tier-model")
	assert.True(t, ok)
	assert.InDelta(t, 2.5, ratio, 1e-9)

	_, okTier := ratio_setting.GetVideoPriceTiers("sync-bad-tier-model")
	assert.False(t, okTier, "非法档表不得写入 VideoPriceTiers")
}

// TestApplyUpstreamEntrySkipsSuspiciousSentinel 钉住 37.5 哨兵:倍率/价格块整体
// skip,描述仍应用,suspicious 单列。
func TestApplyUpstreamEntrySkipsSuspiciousSentinel(t *testing.T) {
	setupOnboardingPrefetchTest(t)

	entry := UpstreamModelPricing{
		ModelName:       "sync-sentinel-model",
		QuotaType:       0,
		ModelRatio:      37.5,
		CompletionRatio: 1.0,
		Description:     "sentinel desc",
	}
	applied, skipped, err := ApplyUpstreamEntryToSettings(&entry)
	require.NoError(t, err)
	assert.True(t, IsSuspiciousUpstreamEntry(&entry))

	assert.ElementsMatch(t, []string{"description"}, appliedFields(applied))
	require.Len(t, skipped, 1)
	assert.Equal(t, "model_ratio", skipped[0].Field)
	assert.Contains(t, skipped[0].Reason, "哨兵")

	_, ok, _ := ratio_setting.GetModelRatio("sync-sentinel-model")
	assert.False(t, ok, "哨兵条目的 model_ratio 不得写入")
}

// TestSyncFromUpstreamEndToEnd 钉住一键同步全链路:复用 prefetch 短缓存只拉一次;
// 缺失模型收进 errors;ratio 应用、tier 应用、秒价因档表优先被 skip、billing
// 应用、描述入库;sentinel 标 suspicious 且倍率块跳过。
func TestSyncFromUpstreamEndToEnd(t *testing.T) {
	db := setupOnboardingPrefetchTest(t)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamSyncFixture))
	}))
	defer srv.Close()
	seedOnboardingChannel(t, db, 8103, srv.URL)

	results, syncErrors, err := SyncModelsFromUpstream(context.Background(), 8103, []string{
		"sync-ratio-model", "sync-full-model", "sync-sentinel-model", "sync-missing-model",
	})
	require.NoError(t, err)
	require.Equal(t, 1, requests, "sync 复用 prefetch 短缓存只拉一次")

	// 缺失模型收进 errors。
	require.Len(t, syncErrors, 1)
	assert.Equal(t, "sync-missing-model", syncErrors[0].Model)

	require.Len(t, results, 3)
	byModel := make(map[string]SyncUpstreamResult, len(results))
	for _, r := range results {
		byModel[r.Model] = r
	}

	// sync-ratio-model:倍率/补全/cache/描述应用,倍率值已变。
	ratioRes := byModel["sync-ratio-model"]
	assert.False(t, ratioRes.Suspicious)
	assert.ElementsMatch(t, []string{"model_ratio", "completion_ratio", "cache_ratio", "description"}, appliedFields(ratioRes.Applied))
	r, ok, _ := ratio_setting.GetModelRatio("sync-ratio-model")
	assert.True(t, ok)
	assert.InDelta(t, 1.5, r, 1e-9)

	// sync-full-model:全字段应用;秒价因档表优先被 skip。
	fullRes := byModel["sync-full-model"]
	assert.ElementsMatch(t, []string{
		"model_ratio", "completion_ratio", "cache_ratio", "create_cache_ratio",
		"image_ratio", "audio_ratio", "audio_completion_ratio", "price_tiers",
		"billing_mode", "billing_expr", "description",
	}, appliedFields(fullRes.Applied))
	assert.Contains(t, skippedFields(fullRes.Skipped), "video_second_price")
	_, okSecond := ratio_setting.GetVideoSecondPrice("sync-full-model")
	assert.False(t, okSecond, "档表优先时不得写入秒价")
	_, okTier := ratio_setting.GetVideoPriceTiers("sync-full-model")
	assert.True(t, okTier)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("sync-full-model"))
	expr, okExpr := billing_setting.GetBillingExpr("sync-full-model")
	assert.True(t, okExpr)
	assert.Contains(t, expr, "tier(")

	// sync-sentinel-model:suspicious=true,倍率块跳过,描述仍应用。
	sentRes := byModel["sync-sentinel-model"]
	assert.True(t, sentRes.Suspicious)
	assert.ElementsMatch(t, []string{"description"}, appliedFields(sentRes.Applied))
	assert.Contains(t, skippedFields(sentRes.Skipped), "model_ratio")
}

package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupGroupPricingTestDB 给 ModelPriceHelper/ModelPriceHelperPerCall 的分组分别
// 定价分支一个独立内存库,并调 model.RefreshPricing() 让 IsGroupPricingEnabled
// 的缓存跟着 Model 行更新 —— 该函数刻意不在读时强制刷新(见 model/model_extra.go
// 的注释),测试必须自己在写完 Model 行之后手动 RefreshPricing 一次,与生产环境
// CreateModelMeta/UpdateModelMeta 保存后的既有行为完全对应。
func setupGroupPricingTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	original := model.DB
	t.Cleanup(func() {
		// 不在这里调 RefreshPricing() —— original 在没有更早测试设置过 model.DB
		// 时是 nil,对 nil DB 刷新缓存会直接 panic(同 IsGroupPricingEnabled 不
		// 强制刷新要规避的那类问题)。每个测试用的模型名互不相同,
		// modelGroupPricingEnabled 里残留这些名字的 true 不影响其它测试读到
		// 错误结果,只需要把 model.DB 本身还原、关掉这次的连接。
		model.DB = original
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Model{}, &model.ModelGroupPrice{}, &model.Ability{}, &model.Channel{}, &model.Vendor{},
	))
}

func float64Ptr(f float64) *float64 { return &f }

func insertGroupPricedModel(t *testing.T, name string) {
	t.Helper()
	m := &model.Model{ModelName: name, Status: 1, GroupPricingEnabled: true}
	require.NoError(t, m.Insert())
	model.RefreshPricing()
}

func newRelayInfoForGroup(modelName, userGroup, usingGroup string) (*gin.Context, *relaycommon.RelayInfo) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("group", usingGroup)
	return ctx, &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       userGroup,
		UsingGroup:      usingGroup,
	}
}

func TestModelPriceHelperGroupPricingHitUsesResolvedPriceNotGroupRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-hit-model")

	require.NoError(t, model.ReplaceModelGroupPrices("gp-hit-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.05)},
	}))

	// 全局 GroupRatio 故意设一个显眼的倍率(10)——分别定价模式下不该被叠乘,
	// 断言里如果实现有 bug 把它乘进去,会用 0.5 而不是 0.05 抓到。
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10}`))

	ctx, info := newRelayInfoForGroup("gp-hit-model", "vip", "vip")
	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)

	assert.True(t, priceData.UsePrice, "配了 model_price 应该走固定价路径")
	assert.Equal(t, 0.05, priceData.ModelPrice)
	assert.Equal(t, float64(1), priceData.GroupRatioInfo.GroupRatio,
		"分别定价模式下 GroupRatio 恒为 1,不叠乘全局 GroupRatio")
	assert.False(t, priceData.GroupRatioInfo.HasSpecialRatio,
		"分别定价模式下不应残留 auto_group 特殊倍率标记")
}

func TestModelPriceHelperGroupPricingUnconfiguredGroupReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-miss-model")

	require.NoError(t, model.ReplaceModelGroupPrices("gp-miss-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.05)},
	}))

	ctx, info := newRelayInfoForGroup("gp-miss-model", "default", "default")
	_, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.Error(t, err, "没配置价格的分组必须报错,不能静默放行或算出 0")
}

func TestModelPriceHelperGroupPricingRatioBasedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-ratio-model")

	require.NoError(t, model.ReplaceModelGroupPrices("gp-ratio-model", []model.ModelGroupPrice{
		{GroupName: "default", ModelRatio: float64Ptr(2.0), CompletionRatio: float64Ptr(3.0)},
	}))

	ctx, info := newRelayInfoForGroup("gp-ratio-model", "default", "default")
	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)

	assert.False(t, priceData.UsePrice, "配了 model_ratio 而非 model_price 时应按 token 倍率计费")
	assert.Equal(t, 2.0, priceData.ModelRatio)
	assert.Equal(t, 3.0, priceData.CompletionRatio)
	assert.Equal(t, 2000, priceData.QuotaToPreConsume, "1000 tokens * ratio 2.0 * groupRatio 1 = 2000")
}

// TestModelPriceHelperGroupPricingDoesNotAffectOtherModels 锁住"这次改动只影响
// 开了分别定价的模型" —— 一个没开分别定价的模型即便和一个开了的模型同时存在,
// 也必须继续走全局 ModelRatio/ModelPrice × GroupRatio 的老路径,行为不变。
func TestModelPriceHelperGroupPricingDoesNotAffectOtherModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-sibling-a")
	require.NoError(t, model.ReplaceModelGroupPrices("gp-sibling-a", []model.ModelGroupPrice{
		{GroupName: "default", ModelPrice: float64Ptr(0.9)},
	}))

	savedModelPrice := ratio_setting.ModelPrice2JSONString()
	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrice))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"unified-sibling-b":0.2}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))

	ctx, info := newRelayInfoForGroup("unified-sibling-b", "default", "default")
	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 0.2, priceData.ModelPrice)
	assert.Equal(t, float64(2), priceData.GroupRatioInfo.GroupRatio,
		"没开分别定价的模型必须继续走全局 GroupRatio,不受另一个模型开了分别定价影响")
}

// --- ModelPriceHelperPerCall ---

func TestModelPriceHelperPerCallGroupPricingHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-percall-model")
	require.NoError(t, model.ReplaceModelGroupPrices("gp-percall-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.3)},
	}))

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio)) })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10}`))

	ctx, info := newRelayInfoForGroup("gp-percall-model", "vip", "vip")
	priceData, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)

	assert.True(t, priceData.UsePrice)
	assert.Equal(t, 0.3, priceData.ModelPrice)
	assert.Equal(t, float64(1), priceData.GroupRatioInfo.GroupRatio)
	assert.Equal(t, 150000, priceData.Quota, "0.3 * QuotaPerUnit(500000) * groupRatio 1 = 150000")
}

func TestModelPriceHelperPerCallGroupPricingUnconfiguredGroupReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-percall-miss-model")
	require.NoError(t, model.ReplaceModelGroupPrices("gp-percall-miss-model", []model.ModelGroupPrice{
		{GroupName: "vip", ModelPrice: float64Ptr(0.3)},
	}))

	ctx, info := newRelayInfoForGroup("gp-percall-miss-model", "default", "default")
	_, err := ModelPriceHelperPerCall(ctx, info)
	require.Error(t, err)
}

// TestModelPriceHelperPerCallGroupPricingRowScalarOverGlobalSecondPrice 分别定价
// 模式下行内没有秒价列时,全局秒价不生效 —— 行是唯一价格权威,按行内标量
// (ModelPrice=999)计费,不得回退全局秒价 0.1。
// (旧契约"全局秒价赢过分别定价"已由分组秒价行内列取代,见
// video_group_second_price_test.go。)
func TestModelPriceHelperPerCallGroupPricingRowScalarOverGlobalSecondPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-video-conflict-model")
	require.NoError(t, model.ReplaceModelGroupPrices("gp-video-conflict-model", []model.ModelGroupPrice{
		{GroupName: "default", ModelPrice: float64Ptr(999)},
	}))

	savedVideoPrice := ratio_setting.VideoSecondPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(savedVideoPrice)) })
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(`{"gp-video-conflict-model":0.1}`))

	ctx, info := newRelayInfoForGroup("gp-video-conflict-model", "default", "default")
	priceData, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)
	assert.Zero(t, priceData.VideoSecondPrice, "行内无秒价列不得回退全局秒价计费")
	assert.Equal(t, 999.0, priceData.ModelPrice, "必须按行内标量固定价计费")
	assert.True(t, priceData.UsePrice)
}

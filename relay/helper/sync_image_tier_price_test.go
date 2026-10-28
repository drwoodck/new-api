package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// imageTierPriceContext 造一个同步图片计费上下文。
//
// 同步图片没有 task_request，档位维度来自已解析的图片请求（Size + Extra 里的
// resolution/image_size，见 relaycommon.BuildImageTierInput）。这里刻意走
// dto.ImageRequest 的真实反序列化，让 Extra 的填充方式与线上完全一致。
func imageTierPriceContext(t *testing.T, modelName string, extra map[string]string, relayMode int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	payload := map[string]any{"model": modelName, "prompt": "a cat"}
	for key, value := range extra {
		payload[key] = value
	}
	rawBody, err := common.Marshal(payload)
	require.NoError(t, err)

	imageReq := &dto.ImageRequest{}
	require.NoError(t, common.Unmarshal(rawBody, imageReq), "图片请求必须能按线上路径反序列化（Extra 才会被填）")

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		RelayMode:       relayMode,
		Request:         imageReq,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	}
	return ctx, info
}

func setImageTiers(t *testing.T, modelName string) {
	t.Helper()
	setTiers(t, `{"`+modelName+`":[
		{"label":"1K","tier_type":"image_size","key":"1K","billing_unit":"request","price":0.02},
		{"label":"2K","tier_type":"image_size","key":"2K","billing_unit":"request","price":0.03},
		{"label":"4K","tier_type":"image_size","key":"4K","billing_unit":"request","price":0.05}]}`)
}

// 同步图片命中 image_size 档：价格必须来自档表，而不是空标量算出的 0。
func TestSyncImagePriceUsesImageSizeTier(t *testing.T) {
	setImageTiers(t, "pimg-tier-sync")
	setGroupRatioTo2(t)

	ctx, info := imageTierPriceContext(t, "pimg-tier-sync",
		map[string]string{"resolution": "2K"}, relayconstant.RelayModeImagesGenerations)

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)

	assert.True(t, priceData.TierBilling, "档表命中必须标记档位计费")
	assert.True(t, priceData.UsePrice, "image_size 档是按次计价")
	assert.InDelta(t, 0.03, priceData.ModelPrice, 1e-9, "2K 档价必须来自档表")
	assert.Equal(t, "image_size", priceData.TierType)
	assert.Equal(t, "2K", priceData.TierKey)
	assert.Equal(t, int(0.03*common.QuotaPerUnit*2), priceData.Quota,
		"额度 = 档价 × QuotaPerUnit × 分组倍率")
}

// 上游同样接受的 image_size 别名也要能命中。
func TestSyncImagePriceUsesImageSizeAlias(t *testing.T) {
	setImageTiers(t, "pimg-tier-alias")
	setGroupRatioTo2(t)

	ctx, info := imageTierPriceContext(t, "pimg-tier-alias",
		map[string]string{"image_size": "4k"}, relayconstant.RelayModeImagesGenerations)

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.True(t, priceData.TierBilling)
	assert.InDelta(t, 0.05, priceData.ModelPrice, 1e-9)
}

// 配了档表但请求里没有档位维度：必须硬报错，不能退回 0 元放行。
func TestSyncImagePriceMissingTierIsRejected(t *testing.T) {
	setImageTiers(t, "pimg-tier-missing")
	setGroupRatioTo2(t)

	ctx, info := imageTierPriceContext(t, "pimg-tier-missing", nil, relayconstant.RelayModeImagesGenerations)

	_, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "档位", "报错要说清是档位没配，而不是笼统的不可用")
}

// 非图片 relay 模式不能被档表劫持：同一模型、同一档位参数，聊天请求应走到
// 「价格未配置」的既有分支，而不是报档位不可用。
func TestChatRequestDoesNotUseImageTierTable(t *testing.T) {
	setImageTiers(t, "pimg-tier-chat")
	setGroupRatioTo2(t)

	ctx, info := imageTierPriceContext(t, "pimg-tier-chat",
		map[string]string{"resolution": "2K"}, relayconstant.RelayModeChatCompletions)

	_, err := ModelPriceHelper(ctx, info, 10, &types.TokenCountMeta{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "档位", "非图片请求不该走图片档位定价")
	assert.Contains(t, err.Error(), "价格", "应回落到既有的价格未配置错误")
}

// 没有档表的图片模型保持原行为：走标量按次价。
func TestSyncImageWithoutTierTableUsesScalarPrice(t *testing.T) {
	savedPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
	setTiers(t, `{}`) // 确保没有档表残留
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"pimg-flat":0.04}`))
	setGroupRatioTo2(t)

	ctx, info := imageTierPriceContext(t, "pimg-flat",
		map[string]string{"resolution": "2K"}, relayconstant.RelayModeImagesGenerations)

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.False(t, priceData.TierBilling, "没有档表时不应标记档位计费")
	assert.InDelta(t, 0.04, priceData.ModelPrice, 1e-9, "应使用全局标量按次价")
}

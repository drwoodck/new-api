package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 分别定价（分组价格行）里的 image_size 档表 —— 线上 paipu 图片模型就是这个形态：
// 价格只存在 model_group_prices.price_tiers 里，model_price/model_ratio 为空。
// 旧实现同步图片完全不读档表，于是每次调用都按 0 元结算。
func TestSyncImageGroupPricedTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupGroupPricingTestDB(t)
	insertGroupPricedModel(t, "gp-img-tier")

	tiers := hosttypes.PriceTierList{
		{Label: "1K", TierType: "image_size", Key: "1K", BillingUnit: "request", Price: 0.02},
		{Label: "2K", TierType: "image_size", Key: "2K", BillingUnit: "request", Price: 0.03},
		{Label: "4K", TierType: "image_size", Key: "4K", BillingUnit: "request", Price: 0.05},
	}
	require.NoError(t, model.ReplaceModelGroupPrices("gp-img-tier", []model.ModelGroupPrice{
		{GroupName: "default", PriceTiers: &tiers},
	}))

	ctx, info := imageTierPriceContext(t, "gp-img-tier",
		map[string]string{"resolution": "2K"}, relayconstant.RelayModeImagesGenerations)

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.True(t, priceData.TierBilling)
	assert.InDelta(t, 0.03, priceData.ModelPrice, 1e-9, "2K 档价必须来自分组价格行的档表")
	assert.Equal(t, "2K", priceData.TierKey)
}

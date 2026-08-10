package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func videoPriceContext(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "per-second-video-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	}
	return ctx, info
}

func setVideoSecondPrice(t *testing.T, jsonStr string) {
	t.Helper()
	require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString(jsonStr))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoSecondPriceByJSONString("{}"))
	})
}

// TestModelPriceHelperPerCallUsesVideoSecondPrice covers the key usability
// requirement: configuring only a per-second price must be enough. Previously
// this path returned "model price not configured".
func TestModelPriceHelperPerCallUsesVideoSecondPrice(t *testing.T) {
	ctx, info := videoPriceContext(t)
	setVideoSecondPrice(t, `{"per-second-video-model":0.1}`)

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err, "a per-second price alone must be valid billing config")
	require.True(t, priceData.UsePrice)
	require.InDelta(t, 0.1, priceData.ModelPrice, 1e-9)
	require.InDelta(t, 0.1, priceData.VideoSecondPrice, 1e-9)
	require.False(t, priceData.FreeModel)
	// Quota at this stage is the one-second quota; duration is applied later.
	require.Equal(t, int(0.1*common.QuotaPerUnit), priceData.Quota)
}

// TestModelPriceHelperPerCallVideoSecondPriceWithGroupRatio pins that the group
// ratio is folded into the one-second base quota.
func TestModelPriceHelperPerCallVideoSecondPriceWithGroupRatio(t *testing.T) {
	ctx, info := videoPriceContext(t)
	setVideoSecondPrice(t, `{"per-second-video-model":0.2}`)

	savedGroupRatio := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0.5}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroupRatio))
	})

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.InDelta(t, 0.5, priceData.GroupRatioInfo.GroupRatio, 1e-9)
	require.Equal(t, int(0.2*common.QuotaPerUnit*0.5), priceData.Quota)
}

// TestModelPriceHelperPerCallIgnoresUnconfiguredVideoModel guards that models
// without a per-second price keep the original per-call behaviour.
func TestModelPriceHelperPerCallIgnoresUnconfiguredVideoModel(t *testing.T) {
	ctx, info := videoPriceContext(t)
	setVideoSecondPrice(t, `{"some-other-model":0.1}`)

	savedPrice := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"per-second-video-model":0.5}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrice))
	})

	priceData, err := ModelPriceHelperPerCall(ctx, info)

	require.NoError(t, err)
	require.Zero(t, priceData.VideoSecondPrice, "must not be treated as per-second billing")
	require.Equal(t, int(0.5*common.QuotaPerUnit), priceData.Quota)
}

// TestHasModelBillingConfigRecognisesVideoSecondPrice ensures model
// availability checks accept a per-second-only configuration.
func TestHasModelBillingConfigRecognisesVideoSecondPrice(t *testing.T) {
	setVideoSecondPrice(t, `{"per-second-only-model":0.1}`)

	require.True(t, HasModelBillingConfig("per-second-only-model"))
}

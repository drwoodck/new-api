package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upstreamPricingFixture 是合法 type2(/api/pricing)响应:
// 全字段模型 + 仅 ratio 普通模型 + 37.5 哨兵模型 + 非法价格模型。
const upstreamPricingFixture = `{
  "success": true,
  "data": [
    {
      "model_name": "full-field-model",
      "quota_type": 0,
      "model_ratio": 1.5,
      "completion_ratio": 2.0,
      "model_price": 0,
      "cache_ratio": 0.1,
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
      "description": "full-field test model",
      "icon": "https://example.com/icon.png",
      "tags": "video,image",
      "vendor_name": "ExampleVendor",
      "enable_groups": ["default", "vip"]
    },
    {
      "model_name": "plain-ratio-model",
      "quota_type": 0,
      "model_ratio": 3.2,
      "completion_ratio": 0.5,
      "model_price": 0
    },
    {
      "model_name": "suspicious-model",
      "quota_type": 0,
      "model_ratio": 37.5,
      "completion_ratio": 1,
      "model_price": 0
    },
    {
      "model_name": "invalid-price-model",
      "quota_type": 0,
      "model_ratio": 2,
      "completion_ratio": 1,
      "model_price": 2000000
    }
  ]
}`

func TestUpstreamPricingFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/pricing", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamPricingFixture))
	}))
	defer srv.Close()

	result, err := FetchUpstreamPricing(context.Background(), srv.URL, "")
	require.NoError(t, err)
	require.Len(t, result, 4)

	full, ok := result["full-field-model"]
	require.True(t, ok)
	assert.Equal(t, "full-field-model", full.ModelName)
	assert.Equal(t, 0, full.QuotaType)
	assert.Equal(t, 1.5, full.ModelRatio)
	assert.Equal(t, 2.0, full.CompletionRatio)
	assert.Equal(t, 0.0, full.ModelPrice)
	require.NotNil(t, full.CacheRatio)
	assert.Equal(t, 0.1, *full.CacheRatio)
	require.NotNil(t, full.CreateCacheRatio)
	assert.Equal(t, 1.25, *full.CreateCacheRatio)
	require.NotNil(t, full.ImageRatio)
	assert.Equal(t, 1.2, *full.ImageRatio)
	require.NotNil(t, full.AudioRatio)
	assert.Equal(t, 0.8, *full.AudioRatio)
	require.NotNil(t, full.AudioCompletionRatio)
	assert.Equal(t, 1.6, *full.AudioCompletionRatio)
	require.NotNil(t, full.VideoSecondPrice)
	assert.Equal(t, 0.02, *full.VideoSecondPrice)
	require.NotNil(t, full.PriceTiers)
	require.Len(t, *full.PriceTiers, 1)
	assert.Equal(t, "720p", (*full.PriceTiers)[0].Key)
	assert.Equal(t, "tiered_expr", full.BillingMode)
	assert.Equal(t, `tier("base", p * 2.5 + c * 15)`, full.BillingExpr)
	assert.Equal(t, "full-field test model", full.Description)
	assert.Equal(t, "https://example.com/icon.png", full.Icon)
	assert.Equal(t, "video,image", full.Tags)
	assert.Equal(t, "ExampleVendor", full.VendorName)
	assert.Equal(t, []string{"default", "vip"}, full.EnableGroups)

	plain, ok := result["plain-ratio-model"]
	require.True(t, ok)
	assert.Equal(t, 3.2, plain.ModelRatio)
	assert.Equal(t, 0.5, plain.CompletionRatio)
	assert.Nil(t, plain.CacheRatio)
	assert.Nil(t, plain.VideoSecondPrice)
	assert.Nil(t, plain.PriceTiers)
	assert.Empty(t, plain.EnableGroups)

	suspicious, ok := result["suspicious-model"]
	require.True(t, ok)
	assert.True(t, IsSuspiciousUpstreamEntry(&suspicious))
	assert.False(t, IsSuspiciousUpstreamEntry(&plain))

	invalid, ok := result["invalid-price-model"]
	require.True(t, ok)
	assert.Error(t, NormalizeUpstreamPricingEntry(&invalid))
}

func TestUpstreamPricingFromBody(t *testing.T) {
	t.Run("valid-body", func(t *testing.T) {
		result, err := FetchUpstreamPricingFromBody([]byte(upstreamPricingFixture))
		require.NoError(t, err)
		require.Len(t, result, 4)
		full, ok := result["full-field-model"]
		require.True(t, ok)
		require.NotNil(t, full.VideoSecondPrice)
		assert.Equal(t, 0.02, *full.VideoSecondPrice)
	})

	t.Run("invalid-json", func(t *testing.T) {
		_, err := FetchUpstreamPricingFromBody([]byte(`not json`))
		require.Error(t, err)
	})

	t.Run("success-false", func(t *testing.T) {
		_, err := FetchUpstreamPricingFromBody([]byte(`{"success": false, "message": "boom"}`))
		require.Error(t, err)
	})
}

func TestUpstreamPricingFetchErrors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		_, err := FetchUpstreamPricing(context.Background(), srv.URL, "")
		require.Error(t, err)
	})

	t.Run("success-false", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"success": false, "message": "boom"}`))
		}))
		defer srv.Close()
		_, err := FetchUpstreamPricing(context.Background(), srv.URL, "")
		require.Error(t, err)
	})

	t.Run("empty-model-name-skipped", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"success": true, "data": [
				{"model_name": "", "model_ratio": 1},
				{"model_name": "   ", "model_ratio": 2},
				{"model_name": "kept", "model_ratio": 3}
			]}`))
		}))
		defer srv.Close()
		result, err := FetchUpstreamPricing(context.Background(), srv.URL, "")
		require.NoError(t, err)
		require.Len(t, result, 1)
		_, ok := result["kept"]
		require.True(t, ok)
	})
}

func TestUpstreamPricingNormalize(t *testing.T) {
	valid := UpstreamModelPricing{
		ModelName:       "m",
		ModelRatio:      1.5,
		CompletionRatio: 2.0,
		PriceTiers: &types.PriceTierList{
			{Label: "720P", TierType: types.TierTypeResolution, Key: "720P", BillingUnit: types.BillingUnitSecond, Price: 0.002},
		},
		BillingExpr: `tier("base", p * 2.5 + c * 15)`,
	}
	require.NoError(t, NormalizeUpstreamPricingEntry(&valid))

	t.Run("price-above-max", func(t *testing.T) {
		outOfRange := valid
		outOfRange.ModelPrice = types.MaxTierPrice + 1
		require.Error(t, NormalizeUpstreamPricingEntry(&outOfRange))
	})

	t.Run("negative-ratio", func(t *testing.T) {
		neg := -0.5
		bad := valid
		bad.CacheRatio = &neg
		require.Error(t, NormalizeUpstreamPricingEntry(&bad))
	})

	t.Run("mixed-tier-type", func(t *testing.T) {
		bad := valid
		bad.PriceTiers = &types.PriceTierList{
			{Label: "a", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 0.002},
			{Label: "b", TierType: types.TierTypeRequest, Key: "5s", BillingUnit: types.BillingUnitSecond, Price: 0.002},
		}
		require.Error(t, NormalizeUpstreamPricingEntry(&bad))
	})

	t.Run("tier-price-below-min", func(t *testing.T) {
		bad := valid
		bad.PriceTiers = &types.PriceTierList{
			{Label: "a", TierType: types.TierTypeResolution, Key: "720p", BillingUnit: types.BillingUnitSecond, Price: 1e-9},
		}
		require.Error(t, NormalizeUpstreamPricingEntry(&bad))
	})

	t.Run("invalid-billing-expr", func(t *testing.T) {
		bad := valid
		bad.BillingExpr = "p *"
		require.Error(t, NormalizeUpstreamPricingEntry(&bad))
	})

	t.Run("empty-model-name", func(t *testing.T) {
		bad := valid
		bad.ModelName = " "
		require.Error(t, NormalizeUpstreamPricingEntry(&bad))
	})

	t.Run("nil-entry", func(t *testing.T) {
		require.Error(t, NormalizeUpstreamPricingEntry(nil))
	})
}

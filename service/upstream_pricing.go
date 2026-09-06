package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
)

// maxRatioConfigBytes 是上游 /api/pricing 响应体的读取上限(10MB)。
const maxRatioConfigBytes = 10 << 20

// UpstreamModelPricing 是上游 /api/pricing 返回的一条原始模型条目,
// 按模型维度保留全部字段(不折叠成倍率 map)。
type UpstreamModelPricing struct {
	ModelName            string
	QuotaType            int
	ModelRatio           float64
	CompletionRatio      float64
	ModelPrice           float64
	CacheRatio           *float64
	CreateCacheRatio     *float64
	ImageRatio           *float64
	AudioRatio           *float64
	AudioCompletionRatio *float64
	VideoSecondPrice     *float64
	PriceTiers           *types.PriceTierList
	BillingMode          string
	BillingExpr          string
	Description          string
	Icon                 string
	Tags                 string
	VendorName           string
	EnableGroups         []string
}

// upstreamPricingItem 是上游 type2(/api/pricing)JSON 数组的单条解析结构,
// 字段与 controller/ratio_sync.go 的 type2 匿名 struct 对齐,并扩展新字段。
type upstreamPricingItem struct {
	ModelName            string               `json:"model_name"`
	QuotaType            int                  `json:"quota_type"`
	ModelRatio           float64              `json:"model_ratio"`
	ModelPrice           float64              `json:"model_price"`
	CompletionRatio      float64              `json:"completion_ratio"`
	CacheRatio           *float64             `json:"cache_ratio"`
	CreateCacheRatio     *float64             `json:"create_cache_ratio"`
	ImageRatio           *float64             `json:"image_ratio"`
	AudioRatio           *float64             `json:"audio_ratio"`
	AudioCompletionRatio *float64             `json:"audio_completion_ratio"`
	VideoSecondPrice     *float64             `json:"video_second_price"`
	PriceTiers           *types.PriceTierList `json:"price_tiers"`
	BillingMode          string               `json:"billing_mode"`
	BillingExpr          string               `json:"billing_expr"`
	Description          string               `json:"description"`
	Icon                 string               `json:"icon"`
	Tags                 string               `json:"tags"`
	VendorName           string               `json:"vendor_name"`
	EnableGroups         []string             `json:"enable_groups"`
}

// FetchUpstreamPricing 拉取并解析上游 {baseURL}/api/pricing(type2)响应,
// 返回按模型名索引的原始条目 map。ctx 超时由调用方控制,失败返回 error。
func FetchUpstreamPricing(ctx context.Context, baseURL string, proxy string) (map[string]UpstreamModelPricing, error) {
	client, err := NewProxyHttpClient(proxy)
	if err != nil {
		return nil, fmt.Errorf("build http client failed: %w", err)
	}

	fullURL := strings.TrimRight(baseURL, "/") + "/api/pricing"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request failed: %w", err)
	}

	// 简单重试:最多 3 次,指数退避(沿用 ratio_sync 的 client 语义)
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, lastErr = client.Do(req)
		if lastErr == nil {
			break
		}
		time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("http error: %w", lastErr)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	limited := io.LimitReader(resp.Body, maxRatioConfigBytes)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	var body struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Message string          `json:"message"`
	}
	if err := common.DecodeJson(bytes.NewReader(bodyBytes), &body); err != nil {
		return nil, fmt.Errorf("json decode failed: %w", err)
	}
	if !body.Success {
		return nil, fmt.Errorf("upstream returned success=false: %s", body.Message)
	}

	var items []upstreamPricingItem
	if err := common.Unmarshal(body.Data, &items); err != nil {
		return nil, fmt.Errorf("unrecognized data format: %w", err)
	}

	result := make(map[string]UpstreamModelPricing, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ModelName) == "" {
			continue
		}
		result[item.ModelName] = UpstreamModelPricing{
			ModelName:            item.ModelName,
			QuotaType:            item.QuotaType,
			ModelRatio:           item.ModelRatio,
			CompletionRatio:      item.CompletionRatio,
			ModelPrice:           item.ModelPrice,
			CacheRatio:           item.CacheRatio,
			CreateCacheRatio:     item.CreateCacheRatio,
			ImageRatio:           item.ImageRatio,
			AudioRatio:           item.AudioRatio,
			AudioCompletionRatio: item.AudioCompletionRatio,
			VideoSecondPrice:     item.VideoSecondPrice,
			PriceTiers:           item.PriceTiers,
			BillingMode:          item.BillingMode,
			BillingExpr:          item.BillingExpr,
			Description:          item.Description,
			Icon:                 item.Icon,
			Tags:                 item.Tags,
			VendorName:           item.VendorName,
			EnableGroups:         item.EnableGroups,
		}
	}
	return result, nil
}

// NormalizeUpstreamPricingEntry 校验并归一化一条上游条目:
//   - model_name 非空(空条目应被调用方剔除)
//   - 数值一律过 [0, MaxTierPrice] 价格边界(与档价同级护栏)
//   - price_tiers 经 types.NormalizePriceTierList 规范化
//   - BillingExpr 非空时走 billing_setting.SmokeTestExpr 编译冒烟(与保存校验同一入口)
//
// QuotaType 0/1 原样保留。调用方按条目粒度调用,坏条目剔除不阻塞其它条目。
func NormalizeUpstreamPricingEntry(p *UpstreamModelPricing) error {
	if p == nil {
		return fmt.Errorf("nil pricing entry")
	}
	if strings.TrimSpace(p.ModelName) == "" {
		return fmt.Errorf("empty model_name")
	}

	numericFields := []struct {
		name  string
		value *float64
	}{
		{"model_ratio", &p.ModelRatio},
		{"completion_ratio", &p.CompletionRatio},
		{"model_price", &p.ModelPrice},
		{"cache_ratio", p.CacheRatio},
		{"create_cache_ratio", p.CreateCacheRatio},
		{"image_ratio", p.ImageRatio},
		{"audio_ratio", p.AudioRatio},
		{"audio_completion_ratio", p.AudioCompletionRatio},
		{"video_second_price", p.VideoSecondPrice},
	}
	for _, f := range numericFields {
		if f.value == nil {
			continue
		}
		if *f.value < 0 || *f.value > types.MaxTierPrice {
			return fmt.Errorf("%s %v 超出合法区间 [0, %g]", f.name, *f.value, types.MaxTierPrice)
		}
	}

	if p.PriceTiers != nil {
		if _, err := types.NormalizePriceTierList(*p.PriceTiers); err != nil {
			return fmt.Errorf("price_tiers 非法: %w", err)
		}
	}

	if strings.TrimSpace(p.BillingExpr) != "" {
		if err := billing_setting.SmokeTestExpr(p.BillingExpr); err != nil {
			return fmt.Errorf("billing_expr 编译冒烟失败: %w", err)
		}
	}
	return nil
}

// IsSuspiciousUpstreamEntry 判断条目是否为不可信哨兵:
// model_ratio == 37.5 且 completion_ratio == 1.0(沿用 ratio_sync 的 confidence 判定)。
func IsSuspiciousUpstreamEntry(p *UpstreamModelPricing) bool {
	if p == nil {
		return false
	}
	const epsilon = 1e-9
	ratio37_5 := p.ModelRatio >= 37.5-epsilon && p.ModelRatio <= 37.5+epsilon
	completion1 := p.CompletionRatio >= 1.0-epsilon && p.CompletionRatio <= 1.0+epsilon
	return ratio37_5 && completion1
}

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

// OnboardingIgnoredModelsOption 是忽略名单 option 键。
const OnboardingIgnoredModelsOption = "OnboardingIgnoredModels"

// HasAnyBillingConfig 判断模型是否已有任何计费配置。上新工作台"待定价"列与
// launch 未定价拦截共用这一个判定。
//
// 覆盖四类:
//   - 全局五种价格:ModelPrice / ModelRatio / VideoSecondPrice / VideoPriceTiers /
//     InputMaterialPrices,任一命中即算已配价;
//   - tiered_expr:计费模式为 tiered_expr 且表达式非空;
//   - 分组定价行:model_group_price 表存在该模型任意一行(逐分组遍历查真源
//     是 N 次查询,model.HasAnyModelGroupPrice 一次 Exists 到位)。
//
// 分组定价开关本身不参与判定 —— 开关开但全空行 = 没配,依然算 unpriced。
//
// 实现刻意不依赖 relay/helper.HasModelBillingConfig:service → relay/helper →
// service 会形成 import 环(relay/helper/stream_scanner.go 已 import service),
// 且 helper 版未覆盖 InputMaterialPrices 与分组行。全局判定的口径与 helper 版
// 保持一致(含 GetModelRatio 的 SelfUseModeEnabled 回退语义)。
func HasAnyBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetVideoSecondPrice(modelName); ok {
		return true
	}
	if _, ok := ratio_setting.GetVideoPriceTiers(modelName); ok {
		return true
	}
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if _, ok := ratio_setting.GetInputMaterialPrices(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		expr, ok := billing_setting.GetBillingExpr(modelName)
		if ok && strings.TrimSpace(expr) != "" {
			return true
		}
	}
	// 该查询不区分 flag 状态:开关关、甚至没有 models 行的孤儿分组价行也算
	// "已配价" —— 过度计数方向,可接受。与 launch 新建 models 行时继承
	// GroupPricingEnabled(controller.launchOnboardingModel)配对,实际影响面已闭合。
	has, err := model.HasAnyModelGroupPrice(modelName)
	if err != nil {
		common.SysError(fmt.Sprintf("查询模型 %s 的分组定价行失败: %v", modelName, err))
		return false
	}
	return has
}

// FilterLaunchable 把待启动的模型名单按"是否已配价"拆成两列。纯函数,不触库。
// launch 时 force=true 会把 hasConfig 换成一个恒真函数,即全部放行。
func FilterLaunchable(names []string, hasConfig func(string) bool) (ok []string, unpriced []string) {
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if hasConfig(name) {
			ok = append(ok, name)
		} else {
			unpriced = append(unpriced, name)
		}
	}
	return ok, unpriced
}

// GetIgnoredModelNames 读取忽略名单 option(JSON 字符串数组)。非法/空值按空
// 名单处理 —— 忽略名单写错不该让工作台整体报错。
func GetIgnoredModelNames() []string {
	raw := model.GetOption(OnboardingIgnoredModelsOption)
	if raw == "" {
		return []string{}
	}
	var names []string
	if err := common.UnmarshalJsonStr(raw, &names); err != nil {
		common.SysError("解析 " + OnboardingIgnoredModelsOption + " 失败: " + err.Error())
		return []string{}
	}
	return names
}

// AppendIgnoredModelNames 把名字并入忽略名单并持久化(与其它 option 写入同走
// model.UpdateOption 通道)。去重、保序、跳过空白名。返回合并后的完整名单。
func AppendIgnoredModelNames(additional []string) ([]string, error) {
	current := GetIgnoredModelNames()
	merged := make([]string, 0, len(current)+len(additional))
	seen := make(map[string]struct{}, len(current)+len(additional))
	for _, name := range current {
		merged = append(merged, name)
		seen[name] = struct{}{}
	}
	for _, raw := range additional {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	data, err := common.Marshal(merged)
	if err != nil {
		return nil, err
	}
	if err := model.UpdateOption(OnboardingIgnoredModelsOption, string(data)); err != nil {
		return nil, err
	}
	return merged, nil
}

// OnboardingDraftCatalogEntry 是工作台第三列(起草态目录条目)的一行。
type OnboardingDraftCatalogEntry struct {
	CatalogID    int    `json:"catalog_id"`
	RemoteID     string `json:"remote_id"`
	DisplayName  string `json:"display_name"`
	Capabilities string `json:"capabilities"`
	Contract     string `json:"contract"`
	Enabled      bool   `json:"enabled"`
}

// OnboardingOverview 是 GET /api/onboarding/overview 的响应体。
type OnboardingOverview struct {
	MissingMeta  []string                      `json:"missing_meta"`
	Unpriced     []string                      `json:"unpriced"`
	DraftCatalog []OnboardingDraftCatalogEntry `json:"draft_catalog"`
	Ignored      []string                      `json:"ignored"`
}

// GetOnboardingOverview 聚合工作台四列待办:
//   - missing_meta:abilities 有、models 表无(复用 model.GetMissingModels);
//   - unpriced:已启用模型里无任何计费配置的;
//   - draft_catalog:起草态(enabled=false)目录条目;
//   - ignored:忽略名单,且从上述三列里剔除。
func GetOnboardingOverview() (*OnboardingOverview, error) {
	ignored := GetIgnoredModelNames()
	ignoredSet := make(map[string]struct{}, len(ignored))
	for _, name := range ignored {
		ignoredSet[name] = struct{}{}
	}

	missing, err := model.GetMissingModels()
	if err != nil {
		return nil, err
	}
	missingMeta := make([]string, 0, len(missing))
	for _, name := range missing {
		if _, ok := ignoredSet[name]; ok {
			continue
		}
		missingMeta = append(missingMeta, name)
	}

	enabled := model.GetEnabledModels()
	unpriced := make([]string, 0, len(enabled))
	for _, name := range enabled {
		if _, ok := ignoredSet[name]; ok {
			continue
		}
		if !HasAnyBillingConfig(name) {
			unpriced = append(unpriced, name)
		}
	}

	drafts, err := model.GetAllCanvasCatalogModelsAdmin()
	if err != nil {
		return nil, err
	}
	draftCatalog := make([]OnboardingDraftCatalogEntry, 0, len(drafts))
	for i := range drafts {
		d := &drafts[i]
		if d.IsEnabled() {
			continue
		}
		if _, ok := ignoredSet[d.RemoteID]; ok {
			continue
		}
		draftCatalog = append(draftCatalog, OnboardingDraftCatalogEntry{
			CatalogID:    d.Id,
			RemoteID:     d.RemoteID,
			DisplayName:  d.DisplayName,
			Capabilities: d.Capabilities,
			Contract:     d.Contract,
			Enabled:      d.IsEnabled(),
		})
	}

	return &OnboardingOverview{
		MissingMeta:  missingMeta,
		Unpriced:     unpriced,
		DraftCatalog: draftCatalog,
		Ignored:      ignored,
	}, nil
}

// ---------------------------------------------------------------------------
// 上游预填(prefetch)与一键同步(sync_from_upstream)
// ---------------------------------------------------------------------------

// upstreamPrefetchCacheTTL 是 prefetch 短缓存 TTL。同步(sync)也复用同一缓存,
// 60s 内的重复拉取直接命中,降低对上游 /api/pricing 的调用频率。
const upstreamPrefetchCacheTTL = 60 * time.Second

// UpstreamPrefetchEntry 是 prefetch 响应里单条模型条目:上游字段(snake_case)
// + valid/suspicious/error 标记。valid=false 表示该条目未通过
// NormalizeUpstreamPricingEntry,不阻塞同响应里的其它条目。
type UpstreamPrefetchEntry struct {
	ModelName            string               `json:"model_name"`
	QuotaType            int                  `json:"quota_type"`
	ModelRatio           float64              `json:"model_ratio"`
	CompletionRatio      float64              `json:"completion_ratio"`
	ModelPrice           float64              `json:"model_price"`
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

	Suspicious bool   `json:"suspicious"`
	Valid      bool   `json:"valid"`
	Error      string `json:"error,omitempty"`
}

// FieldChange 是 sync 响应里 applied 的一条:field/old/new。old 为 nil 表示
// 之前未配置(unset),new 为写入后的值。
type FieldChange struct {
	Field string      `json:"field"`
	Old   interface{} `json:"old"`
	New   interface{} `json:"new"`
}

// FieldSkip 是 sync 响应里 skipped 的一条:字段名 + 跳过原因。
type FieldSkip struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// SyncUpstreamResult 是 sync 响应里单个模型的处理结果。
type SyncUpstreamResult struct {
	Model      string        `json:"model"`
	Applied    []FieldChange `json:"applied"`
	Skipped    []FieldSkip   `json:"skipped"`
	Suspicious bool          `json:"suspicious"`
}

// SyncUpstreamError 是 sync 响应里单个模型的整体错误(如上游缺条目)。
type SyncUpstreamError struct {
	Model string `json:"model"`
	Error string `json:"error"`
}

type upstreamPrefetchCacheEntry struct {
	entries  map[string]UpstreamPrefetchEntry
	source   string
	expireAt time.Time
}

// upstreamPrefetchCache 是包级短缓存:key=channelID+sha256(排序拼接的模型名)。
var upstreamPrefetchCache sync.Map

func upstreamPrefetchCacheKey(channelID int, modelNames []string) string {
	sorted := append([]string(nil), modelNames...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return fmt.Sprintf("%d:%s", channelID, hex.EncodeToString(h[:]))
}

// PrefetchUpstreamPricing 拉取并缓存上游定价条目,按 modelNames 过滤(空=全部),
// 逐条目 Normalize 标记 valid、IsSuspicious 标记 suspicious。返回 entries 与
// source(渠道 baseURL)。坏条目标 valid=false 不阻塞其它条目。
func PrefetchUpstreamPricing(ctx context.Context, channelID int, modelNames []string) (map[string]UpstreamPrefetchEntry, string, error) {
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, "", fmt.Errorf("查询渠道失败: %w", err)
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		return nil, "", fmt.Errorf("渠道 %d 未配置 base_url", channelID)
	}
	source := strings.TrimRight(baseURL, "/")

	key := upstreamPrefetchCacheKey(channelID, modelNames)
	if v, ok := upstreamPrefetchCache.Load(key); ok {
		cached := v.(upstreamPrefetchCacheEntry)
		if time.Now().Before(cached.expireAt) {
			return cached.entries, cached.source, nil
		}
		upstreamPrefetchCache.Delete(key)
	}

	proxy := channel.GetSetting().Proxy
	raw, err := FetchUpstreamPricing(ctx, source, proxy)
	if err != nil {
		return nil, "", err
	}

	requested := make(map[string]bool, len(modelNames))
	for _, rawName := range modelNames {
		if name := strings.TrimSpace(rawName); name != "" {
			requested[name] = true
		}
	}

	entries := make(map[string]UpstreamPrefetchEntry, len(raw))
	for name, item := range raw {
		if len(requested) > 0 && !requested[name] {
			continue
		}
		entry := UpstreamPrefetchEntry{
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
		if err := NormalizeUpstreamPricingEntry(&item); err != nil {
			entry.Valid = false
			entry.Error = err.Error()
		} else {
			entry.Valid = true
		}
		entry.Suspicious = IsSuspiciousUpstreamEntry(&item)
		entries[name] = entry
	}

	upstreamPrefetchCache.Store(key, upstreamPrefetchCacheEntry{
		entries:  entries,
		source:   source,
		expireAt: time.Now().Add(upstreamPrefetchCacheTTL),
	})
	return entries, source, nil
}

// applyOptionField 整表读改写一个 map 型 option:读(传入的 current 副本)→ 改
// 该模型键 → 写回(model.UpdateOption 同时落库与更新内存)。与手动改价同一路径。
// 返回值 old 为 nil 表示该模型此前未配置此 option。
func applyOptionField[V any](name, fieldName, optionKey string, current map[string]V, value V) (FieldChange, error) {
	old, existed := current[name]
	current[name] = value
	data, err := common.Marshal(current)
	if err != nil {
		return FieldChange{}, fmt.Errorf("marshal %s failed: %w", optionKey, err)
	}
	if err := model.UpdateOption(optionKey, string(data)); err != nil {
		return FieldChange{}, fmt.Errorf("persist %s failed: %w", optionKey, err)
	}
	change := FieldChange{Field: fieldName, New: value}
	if existed {
		change.Old = old
	}
	return change, nil
}

// recordFieldResult 把单个字段的写结果按成功/失败分派到 applied / skipped。
func recordFieldResult(fieldName string, change FieldChange, err error, applied *[]FieldChange, skipped *[]FieldSkip) {
	if err != nil {
		*skipped = append(*skipped, FieldSkip{Field: fieldName, Reason: err.Error()})
		return
	}
	*applied = append(*applied, change)
}

// inPriceBound 是数值字段写库前的边界护栏,与 NormalizeUpstreamPricingEntry 同一
// 区间 [0, MaxTierPrice]。越界的字段按 skip 处理,不阻塞同条目其它字段。
func inPriceBound(v float64) bool {
	return v >= 0 && v <= types.MaxTierPrice
}

// upsertModelDescription 以 models 表为 Description 真源写描述:
// 行存在(含软删除)→ Update(Select 白名单含 description);不存在 → Create(status=1)。
func upsertModelDescription(name, description string) (old string, existed bool, err error) {
	var m model.Model
	err = model.DB.Unscoped().Where("model_name = ?", name).Limit(1).Find(&m).Error
	if err != nil {
		return "", false, err
	}
	if m.Id == 0 {
		meta := &model.Model{ModelName: name, Description: description, Status: 1}
		return "", false, meta.Insert()
	}
	old = m.Description
	err = model.DB.Unscoped().Model(&model.Model{}).Where("id = ?", m.Id).
		Select("description").Updates(map[string]interface{}{"description": description}).Error
	return old, true, err
}

// ApplyUpstreamEntryToSettings 把一条上游条目按字段独立 try 应用到本地全局定价
// 设置:数值字段越界 skip、非法档表 skip、billing_expr 编译冒烟失败 skip、37.5
// 哨兵条目整体跳过倍率/价格块并返回 suspicious。每个字段互不阻塞。分组独立价
// (model_group_price)与目录手填 pricing 一律不碰。
func ApplyUpstreamEntryToSettings(p *UpstreamModelPricing) (applied []FieldChange, skipped []FieldSkip, err error) {
	if p == nil {
		return nil, nil, fmt.Errorf("nil pricing entry")
	}
	name := p.ModelName
	if strings.TrimSpace(name) == "" {
		return nil, nil, fmt.Errorf("empty model_name")
	}

	applied = make([]FieldChange, 0, 8)
	skipped = make([]FieldSkip, 0, 4)

	// 倍率/价格块:QuotaType==1 写 ModelPrice;否则写 ModelRatio+CompletionRatio。
	// 37.5 哨兵条目整体跳过该块(与 ratio_sync 的 confidence 判定一致)。
	if IsSuspiciousUpstreamEntry(p) {
		skipped = append(skipped, FieldSkip{
			Field:  "model_ratio",
			Reason: "37.5/1.0 哨兵倍率,model_ratio/completion_ratio/model_price 整体跳过",
		})
	} else if p.QuotaType == 1 {
		if inPriceBound(p.ModelPrice) {
			change, ferr := applyOptionField(name, "model_price", "ModelPrice", ratio_setting.GetModelPriceCopy(), p.ModelPrice)
			recordFieldResult("model_price", change, ferr, &applied, &skipped)
		} else {
			skipped = append(skipped, FieldSkip{Field: "model_price", Reason: fmt.Sprintf("model_price %v 超出合法区间 [0, %g]", p.ModelPrice, types.MaxTierPrice)})
		}
	} else {
		if inPriceBound(p.ModelRatio) {
			change, ferr := applyOptionField(name, "model_ratio", "ModelRatio", ratio_setting.GetModelRatioCopy(), p.ModelRatio)
			recordFieldResult("model_ratio", change, ferr, &applied, &skipped)
		} else {
			skipped = append(skipped, FieldSkip{Field: "model_ratio", Reason: fmt.Sprintf("model_ratio %v 超出合法区间 [0, %g]", p.ModelRatio, types.MaxTierPrice)})
		}
		if inPriceBound(p.CompletionRatio) {
			change, ferr := applyOptionField(name, "completion_ratio", "CompletionRatio", ratio_setting.GetCompletionRatioCopy(), p.CompletionRatio)
			recordFieldResult("completion_ratio", change, ferr, &applied, &skipped)
		} else {
			skipped = append(skipped, FieldSkip{Field: "completion_ratio", Reason: fmt.Sprintf("completion_ratio %v 超出合法区间 [0, %g]", p.CompletionRatio, types.MaxTierPrice)})
		}
	}

	// 各辅助倍率:指针非 nil 才写。
	for _, spec := range []struct {
		fieldName string
		optionKey string
		current   map[string]float64
		value     *float64
	}{
		{"cache_ratio", "CacheRatio", ratio_setting.GetCacheRatioCopy(), p.CacheRatio},
		{"create_cache_ratio", "CreateCacheRatio", ratio_setting.GetCreateCacheRatioCopy(), p.CreateCacheRatio},
		{"image_ratio", "ImageRatio", ratio_setting.GetImageRatioCopy(), p.ImageRatio},
		{"audio_ratio", "AudioRatio", ratio_setting.GetAudioRatioCopy(), p.AudioRatio},
		{"audio_completion_ratio", "AudioCompletionRatio", ratio_setting.GetAudioCompletionRatioCopy(), p.AudioCompletionRatio},
	} {
		if spec.value == nil {
			continue
		}
		if !inPriceBound(*spec.value) {
			skipped = append(skipped, FieldSkip{Field: spec.fieldName, Reason: fmt.Sprintf("%s %v 超出合法区间 [0, %g]", spec.fieldName, *spec.value, types.MaxTierPrice)})
			continue
		}
		change, ferr := applyOptionField(name, spec.fieldName, spec.optionKey, spec.current, *spec.value)
		recordFieldResult(spec.fieldName, change, ferr, &applied, &skipped)
	}

	// video_second_price:Plan2 遗留裁决——PriceTiers 非空时档表优先,跳过按秒价。
	if p.VideoSecondPrice != nil {
		if p.PriceTiers != nil && len(*p.PriceTiers) > 0 {
			skipped = append(skipped, FieldSkip{Field: "video_second_price", Reason: "tier table wins:已配置 price_tiers,跳过 video_second_price"})
		} else if inPriceBound(*p.VideoSecondPrice) {
			change, ferr := applyOptionField(name, "video_second_price", "VideoSecondPrice", ratio_setting.GetVideoSecondPriceCopy(), *p.VideoSecondPrice)
			recordFieldResult("video_second_price", change, ferr, &applied, &skipped)
		} else {
			skipped = append(skipped, FieldSkip{Field: "video_second_price", Reason: fmt.Sprintf("video_second_price %v 超出合法区间 [0, %g]", *p.VideoSecondPrice, types.MaxTierPrice)})
		}
	}

	// price_tiers:条目级 NormalizePriceTierList 校验失败 skip,不影响其它字段。
	if p.PriceTiers != nil && len(*p.PriceTiers) > 0 {
		norm, terr := types.NormalizePriceTierList(*p.PriceTiers)
		if terr != nil {
			skipped = append(skipped, FieldSkip{Field: "price_tiers", Reason: "price_tiers 非法: " + terr.Error()})
		} else {
			change, ferr := applyOptionField(name, "price_tiers", "VideoPriceTiers", ratio_setting.GetVideoPriceTiersCopy(), norm)
			recordFieldResult("price_tiers", change, ferr, &applied, &skipped)
		}
	}

	// billing_mode + billing_expr:tiered_expr 且 expr 非空时过编译冒烟后写 billing_setting。
	if p.BillingMode == billing_setting.BillingModeTieredExpr {
		expr := strings.TrimSpace(p.BillingExpr)
		if expr == "" {
			skipped = append(skipped, FieldSkip{Field: "billing_expr", Reason: "billing_mode=tiered_expr 但 billing_expr 为空,跳过"})
		} else if berr := billing_setting.SmokeTestExpr(expr); berr != nil {
			skipped = append(skipped, FieldSkip{Field: "billing_expr", Reason: "billing_expr 编译冒烟失败: " + berr.Error()})
		} else {
			change, ferr := applyOptionField(name, "billing_mode", "billing_setting.billing_mode", billing_setting.GetBillingModeCopy(), billing_setting.BillingModeTieredExpr)
			recordFieldResult("billing_mode", change, ferr, &applied, &skipped)
			change, ferr = applyOptionField(name, "billing_expr", "billing_setting.billing_expr", billing_setting.GetBillingExprCopy(), expr)
			recordFieldResult("billing_expr", change, ferr, &applied, &skipped)
		}
	}

	// description:models 表 Description 真源。上游描述为空时不覆盖本地已有描述。
	if desc := strings.TrimSpace(p.Description); desc != "" {
		old, existed, derr := upsertModelDescription(name, desc)
		if derr != nil {
			skipped = append(skipped, FieldSkip{Field: "description", Reason: "models 表写入失败: " + derr.Error()})
		} else {
			change := FieldChange{Field: "description", New: desc}
			if existed {
				change.Old = old
			}
			applied = append(applied, change)
		}
	} else {
		skipped = append(skipped, FieldSkip{Field: "description", Reason: "upstream description 为空,跳过"})
	}

	return applied, skipped, nil
}

// SyncModelsFromUpstream 一键同步:按 modelNames(空=全部)拉上游 → 逐模型应用
// ApplyUpstreamEntryToSettings。上游缺条目收进 errors;每个模型的处理明细
// (applied/skipped/suspicious)收进 results。分组独立价与目录手填 pricing 不碰。
func SyncModelsFromUpstream(ctx context.Context, channelID int, modelNames []string) ([]SyncUpstreamResult, []SyncUpstreamError, error) {
	entries, _, err := PrefetchUpstreamPricing(ctx, channelID, modelNames)
	if err != nil {
		return nil, nil, err
	}

	requested := make([]string, 0, len(modelNames))
	for _, rawName := range modelNames {
		if name := strings.TrimSpace(rawName); name != "" {
			requested = append(requested, name)
		}
	}

	// 指定了模型按请求序处理;空=全部,按字典序保证响应稳定。
	targets := requested
	if len(targets) == 0 {
		for name := range entries {
			targets = append(targets, name)
		}
		sort.Strings(targets)
	}

	results := make([]SyncUpstreamResult, 0, len(targets))
	errs := make([]SyncUpstreamError, 0)
	for _, name := range targets {
		item, ok := entries[name]
		if !ok {
			errs = append(errs, SyncUpstreamError{Model: name, Error: "上游未返回该模型条目"})
			continue
		}
		raw := UpstreamModelPricing{
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
		applied, skipped, applyErr := ApplyUpstreamEntryToSettings(&raw)
		if applyErr != nil {
			errs = append(errs, SyncUpstreamError{Model: name, Error: applyErr.Error()})
			continue
		}
		results = append(results, SyncUpstreamResult{
			Model:      name,
			Applied:    applied,
			Skipped:    skipped,
			Suspicious: item.Suspicious,
		})
	}
	return results, errs, nil
}

package helper

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(modelName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 的价格未配置。请前往「系统设置 → 运营设置」开启自用模式，或在「系统设置 → 分组与模型定价设置」中为该模型配置价格；"+
				"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName, modelName,
		)
	}
	return fmt.Errorf(
		"模型 %s 的价格尚未由管理员配置，暂时无法使用，请联系站点管理员开启该模型；"+
			"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName, modelName,
	)
}

// modelGroupPriceNotAvailableError 分别定价模式下,该模型对当前分组没有配置价格。
// 与 modelPriceNotConfiguredError(全局压根没定价)是不同的失败原因,分开给
// 提示文案 —— 管理员看到的是"去补哪个分组",不是"这个模型完全没配置"。
func modelGroupPriceNotAvailableError(modelName, groupName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 已开启分组分别定价,但分组「%s」尚未配置价格。请在「模型」页面为该模型的这个分组补充价格；"+
				"Model %s has group-specific pricing enabled, but no price is configured for group %q. "+
				"Please add a price for this group on the Models page.",
			modelName, groupName, modelName, groupName,
		)
	}
	return fmt.Errorf(
		"模型 %s 对您当前的分组不可用，请联系站点管理员；"+
			"Model %s is not available for your current group. Please contact the site administrator.",
		modelName, modelName,
	)
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// defaultTieredPreConsumeMaxTokens is the fallback completion-token estimate
// used for tiered expression pre-consume when the client omits max_tokens, so
// the pre-consumed quota still reflects a plausible output cost in paid groups.
const defaultTieredPreConsumeMaxTokens = 8192

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) hosttypes.GroupRatioInfo {
	groupRatioInfo := hosttypes.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	// Check if this model uses tiered_expr billing
	//
	// 必须在检查分组分别定价(IsGroupPricingEnabled)**之前**判断并 return ——
	// 后者会触发 model.GetPricing() 的整套定价缓存刷新(打 abilities/channels/
	// vendors 表),tiered_expr 模型不需要也不应该为了判断"要不要走分别定价"
	// 而承担这次刷新开销。tiered_expr 与分组分别定价互斥、以 tiered_expr 为准
	// 是既定行为,这个早返回本身就是那条规则的落地,不需要额外读一次分组分别
	// 定价开关来"确认"冲突再放行——那样反而对每个 tiered_expr 请求都强制刷新
	// 一次定价缓存,得不偿失,牺牲的只是一条运营方诊断日志。
	if billing_setting.GetBillingMode(info.OriginModelName) == billing_setting.BillingModeTieredExpr {
		return modelPriceHelperTiered(c, info, promptTokens, meta, groupRatioInfo)
	}

	groupPricingEnabled := model.IsGroupPricingEnabled(info.OriginModelName)

	var modelPrice float64
	var usePrice bool
	var groupPriceResult model.ResolvedGroupPrice
	if groupPricingEnabled {
		var err error
		groupPriceResult, err = model.ResolveGroupPrice(info.OriginModelName, info.UsingGroup)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if !groupPriceResult.Available {
			return hosttypes.PriceData{}, modelGroupPriceNotAvailableError(info.OriginModelName, info.UsingGroup, info.UserId)
		}
		// 分别定价的数字本身就是最终价,不再叠乘 GroupRatio(ResolveGroupPrice
		// 已确认的约定)。覆盖到 groupRatioInfo 上,让下面所有既有的
		// "× groupRatioInfo.GroupRatio" 计算保持不变 —— 不用为分别定价另写
		// 一套公式。同时清掉 HandleGroupRatio 可能已经算出的 auto_group 跨组
		// 特殊倍率标记 —— 那是统一倍率模式下的独立概念,分别定价模式下
		// GroupRatio 已经是最终价的一部分,继续留着 HasSpecialRatio=true 只会让
		// 日志里的 user_group_ratio 显示一个跟实际扣费无关的旧数字。
		groupRatioInfo.GroupRatio = groupPriceResult.GroupRatioApplied
		groupRatioInfo.HasSpecialRatio = false
		groupRatioInfo.GroupSpecialRatio = -1
		usePrice = groupPriceResult.QuotaType == 1
		modelPrice = groupPriceResult.ModelPrice
	} else {
		modelPrice, usePrice = ratio_setting.GetModelPrice(info.OriginModelName, false)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumedTokens := common.Max(promptTokens, common.PreConsumedQuota)
		if meta.MaxTokens != 0 {
			preConsumedTokens += meta.MaxTokens
		}
		if groupPricingEnabled {
			// Available 已在上面确认过;分别定价模式下不存在"该分组没配置
			// 倍率但仍放行"的逃生舱 —— AcceptUnsetRatioModel 是给"全局压根
			// 没定价"这个不同问题用的,分组分别定价开着就意味着这个模型的
			// 价格权威来源是 model_group_price,不回退到全局 ratio 表。
			modelRatio = groupPriceResult.ModelRatio
			completionRatio = groupPriceResult.CompletionRatio
		} else {
			var success bool
			var matchName string
			modelRatio, success, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
			if !success {
				acceptUnsetRatio := false
				if info.UserSetting.AcceptUnsetRatioModel {
					acceptUnsetRatio = true
				}
				if !acceptUnsetRatio {
					return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
				}
			}
			completionRatio = ratio_setting.GetCompletionRatio(info.OriginModelName)
		}
		cacheRatio, _ = ratio_setting.GetCacheRatio(info.OriginModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(info.OriginModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// 固定1h和5min缓存写入价格的比例
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(info.OriginModelName)
		audioRatio = ratio_setting.GetAudioRatio(info.OriginModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(info.OriginModelName)
		ratio := modelRatio * groupRatioInfo.GroupRatio
		quota, err := common.QuotaFromFloatStrict(float64(preConsumedTokens) * ratio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedQuota = quota
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)

	// 档位计费（price_tiers）：模型配置了档表时优先于此后的全部路径。
	// NoTable（无档表）时继续走既有流程，旧模型行为完全不变。
	//
	// 分别定价模式的行内档表需要查行：把这次行查询缓存下来，让下方旧标量
	// 分支复用（ResolveGroupPriceFromRow），避免同一请求查两次
	// model_group_price 的热路径回归。统一模式只读全局档表（RWMap），零 DB。
	tierInput := relaycommon.BuildTaskTierInput(c, info)
	var groupPriceRow *model.ModelGroupPrice
	var groupPriceRowChecked bool
	groupPricingEnabled := model.IsGroupPricingEnabled(info.OriginModelName)
	if groupPricingEnabled {
		row, err := model.GetModelGroupPrice(info.OriginModelName, info.UsingGroup)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		groupPriceRow = row
		groupPriceRowChecked = true
		resolved := model.ResolveTierPriceFromRow(info.OriginModelName, info.UsingGroup, tierInput, row)
		if resolved.Status == model.TierResolutionResolved {
			return buildTierPriceData(resolved, groupRatioInfo)
		}
		if resolved.Status == model.TierResolutionUnavailable {
			return hosttypes.PriceData{}, modelTierPriceNotAvailableError(
				info.OriginModelName, info.UsingGroup, resolved.TierKey, info.UserId)
		}
	} else {
		resolved := model.ResolveTierPriceUnified(info.OriginModelName, info.UserGroup, info.UsingGroup, tierInput)
		if resolved.Status == model.TierResolutionResolved {
			return buildTierPriceData(resolved, groupRatioInfo)
		}
		if resolved.Status == model.TierResolutionUnavailable {
			return hosttypes.PriceData{}, modelTierPriceNotAvailableError(
				info.OriginModelName, info.UsingGroup, resolved.TierKey, info.UserId)
		}
	}

	// 视频按秒计费的模型只需配置每秒单价，不应再要求额外的按次价格/倍率。
	// 真正的额度计算（单价 × 时长）在 RelayTaskSubmit 的按秒计费步骤完成。
	if secondPrice, ok := ratio_setting.GetVideoSecondPrice(info.OriginModelName); ok {
		return buildVideoSecondPriceData(secondPrice, groupRatioInfo)
	}

	var modelPrice float64
	var usePrice bool
	var modelRatio float64

	if groupPricingEnabled {
		var groupPriceResult model.ResolvedGroupPrice
		if groupPriceRowChecked {
			// 复用档位检查时已查好的行 —— 热路径每请求只查一次表
			groupPriceResult = model.ResolveGroupPriceFromRow(groupPriceRow)
		} else {
			var err error
			groupPriceResult, err = model.ResolveGroupPrice(info.OriginModelName, info.UsingGroup)
			if err != nil {
				return hosttypes.PriceData{}, err
			}
		}
		if !groupPriceResult.Available {
			return hosttypes.PriceData{}, modelGroupPriceNotAvailableError(info.OriginModelName, info.UsingGroup, info.UserId)
		}
		groupRatioInfo.GroupRatio = groupPriceResult.GroupRatioApplied
		groupRatioInfo.HasSpecialRatio = false
		groupRatioInfo.GroupSpecialRatio = -1
		usePrice = groupPriceResult.QuotaType == 1
		if usePrice {
			modelPrice = groupPriceResult.ModelPrice
		} else {
			modelRatio = groupPriceResult.ModelRatio
		}
	} else {
		var success bool
		modelPrice, success = ratio_setting.GetModelPrice(info.OriginModelName, true)
		usePrice = success

		if !success {
			defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
			if ok {
				modelPrice = defaultPrice
				usePrice = true
			} else {
				var ratioSuccess bool
				var matchName string
				modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
				acceptUnsetRatio := false
				if info.UserSetting.AcceptUnsetRatioModel {
					acceptUnsetRatio = true
				}
				if !ratioSuccess && !acceptUnsetRatio {
					return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
				}
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		var err error
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		var err error
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:      freeModel,
		ModelPrice:     modelPrice,
		ModelRatio:     modelRatio,
		UsePrice:       usePrice,
		Quota:          quota,
		GroupRatioInfo: groupRatioInfo,
	}
	return priceData, nil
}

// buildVideoSecondPriceData 构造按秒计费的基础价格数据。
// Quota 此处仅为「1 秒」的额度，时长倍率由 RelayTaskSubmit 统一应用。
func buildVideoSecondPriceData(secondPrice float64, groupRatioInfo hosttypes.GroupRatioInfo) (hosttypes.PriceData, error) {
	quota, err := common.QuotaFromFloatStrict(secondPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume && groupRatioInfo.GroupRatio == 0 {
		quota = 0
		freeModel = true
	}
	return hosttypes.PriceData{
		FreeModel:        freeModel,
		ModelPrice:       secondPrice,
		UsePrice:         true,
		Quota:            quota,
		VideoSecondPrice: secondPrice,
		GroupRatioInfo:   groupRatioInfo,
	}, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetVideoSecondPrice(modelName); ok {
		return true
	}
	// 档位计费也是合法的计费配置 —— 只配了档表的模型不应从模型列表消失。
	if _, ok := ratio_setting.GetVideoPriceTiers(modelName); ok {
		return true
	}
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo hosttypes.GroupRatioInfo) (hosttypes.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(info.OriginModelName)
	if !ok {
		return hosttypes.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", info.OriginModelName)
	}

	estimatedCompletionTokens := meta.MaxTokens
	if estimatedCompletionTokens == 0 && groupRatioInfo.GroupRatio != 0 {
		estimatedCompletionTokens = defaultTieredPreConsumeMaxTokens
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	rawCost, trace, err := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   float64(estimatedCompletionTokens),
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return hosttypes.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", info.OriginModelName, err)
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeGroup := rawCost / 1_000_000 * common.QuotaPerUnit
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * groupRatioInfo.GroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	exprHash := billingexpr.ExprHashString(exprStr)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:               billing_setting.BillingModeTieredExpr,
		ModelName:                 info.OriginModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                groupRatioInfo.GroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: estimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		QuotaPerUnit:              common.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := hosttypes.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", info.OriginModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}

// modelTierPriceNotAvailableError 档表模型的请求档位在当前分组未配置时的错误。
// 与 modelGroupPriceNotAvailableError 同构：管理员看到"缺哪个档"，
// 用户看到"请联系管理员"。
func modelTierPriceNotAvailableError(modelName, groupName, tierKey string, userId int) error {
	tierDisplay := tierKey
	if tierDisplay == "" {
		tierDisplay = "(默认档)"
	}
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"模型 %s 已配置档位定价，但档位「%s」在分组「%s」的档表中未配置价格。请在「模型」页面的分组配置或「计费与支付 → 分组定价」中补充该档位；"+
				"Model %s has tiered pricing enabled, but tier %q is not configured for group %q.",
			modelName, tierDisplay, groupName, modelName, tierDisplay, groupName,
		)
	}
	return fmt.Errorf(
		"模型 %s 对您当前的分组与请求档位不可用，请联系站点管理员；"+
			"Model %s is not available for your current group and requested tier.",
		modelName, modelName,
	)
}

// buildTierPriceData 把解析出的档位换算成 PriceData。
// resolved.Price 是档位原价（未乘倍率），统一模式与分别定价模式的倍率差异
// 全部体现在 resolved.GroupRatioApplied 上 —— 这里覆盖 groupRatioInfo.GroupRatio
// 为实际应用值后按既有公式乘一次，与全局标量路径"原价 × GroupRatio"同口径。
//
// billing_unit=second：委托 buildVideoSecondPriceData（1 秒的额度），
// VideoSecondPrice 字段承载档价 —— relay_task.go 的 applyVideoSecondPricing 与
// controller/relay.go 的 PerCallBilling 判定无需感知档位即可自动正确工作
// （tier-second → 参与差额结算；tier-request → 固定价不结算）。
func buildTierPriceData(resolved model.ResolvedTierPrice, groupRatioInfo hosttypes.GroupRatioInfo) (hosttypes.PriceData, error) {
	groupRatioInfo.GroupRatio = resolved.GroupRatioApplied
	groupRatioInfo.HasSpecialRatio = false
	groupRatioInfo.GroupSpecialRatio = -1

	var priceData hosttypes.PriceData
	var err error
	switch resolved.BillingUnit {
	case hosttypes.BillingUnitSecond:
		priceData, err = buildVideoSecondPriceData(resolved.Price, groupRatioInfo)
	case hosttypes.BillingUnitRequest:
		quota, convErr := common.QuotaFromFloatStrict(resolved.Price * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		if convErr != nil {
			return hosttypes.PriceData{}, convErr
		}
		freeModel := false
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || resolved.Price == 0 {
				quota = 0
				freeModel = true
			}
		}
		priceData = hosttypes.PriceData{
			FreeModel:      freeModel,
			ModelPrice:     resolved.Price,
			UsePrice:       true,
			Quota:          quota,
			GroupRatioInfo: groupRatioInfo,
		}
	default:
		return hosttypes.PriceData{}, fmt.Errorf("model tier %q has unsupported billing unit %q", resolved.TierKey, resolved.BillingUnit)
	}
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	priceData.TierBilling = true
	priceData.TierType = resolved.TierType
	priceData.TierKey = resolved.TierKey
	priceData.TierBillingUnit = resolved.BillingUnit
	priceData.TierSnapshot = resolved.TierSnapshot
	return priceData, nil
}

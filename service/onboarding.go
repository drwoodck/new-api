package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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

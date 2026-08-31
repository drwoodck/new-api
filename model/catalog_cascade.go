package model

import "github.com/QuantumNous/new-api/common"

// SoftDisableUncoveredCatalogEntries 把「已无任何启用渠道覆盖」的目录条目软停用。
//
// 判据是「这个 remote_id 在 abilities 里还有没有任何 enabled=true 的行」，
// 不是「某个渠道停了」——模型可以挂在多个渠道上，停一个不代表整个模型没法调了
// (决策 3)。abilities 的复合主键含 channel_id，同一 model 名字在不同渠道
// 各占一行，互不影响。
//
// 只单向：只会把 enabled=true 的条目改成 false，绝不会把 enabled=false 的条目
// 改回 true。运营方手动停用的条目，即便渠道后来恢复了，也不会被这个函数碰——
// 那必须是运营方自己在目录管理页里重新勾选启用，不能被渠道状态的巧合联动覆盖。
//
// 调用时机：渠道状态变化后（UpdateChannelStatus 里的启用/停用）。函数本身
// 对渠道状态一无所知，只读 abilities 当前状态，所以调用方随便什么时候调都对，
// 不要求传入"刚才是哪个渠道被改了"。
func SoftDisableUncoveredCatalogEntries() error {
	var enabledCatalogEntries []CanvasCatalogModel
	if err := DB.Where("enabled = ?", true).Find(&enabledCatalogEntries).Error; err != nil {
		return err
	}
	if len(enabledCatalogEntries) == 0 {
		return nil
	}

	for _, entry := range enabledCatalogEntries {
		var count int64
		if err := DB.Model(&Ability{}).
			Where("model = ? AND enabled = ?", entry.RemoteID, true).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		// 再无任何启用渠道覆盖这个 remote_id —— 软停用。用 Update 按列名直接写
		// SQL 值（不走整行 Save，避免带出其它字段的并发覆盖）；enabled 在 Go
		// 侧是 *bool（见字段注释），但这里走的是原始列名更新，传裸 bool 即可，
		// 不需要构造指针去匹配struct 字段类型。
		if err := DB.Model(&CanvasCatalogModel{}).Where("id = ?", entry.Id).
			Update("enabled", false).Error; err != nil {
			return err
		}
	}
	return nil
}

// ModelMappingInconsistency 描述同一对外模型名在不同渠道上映射到不同上游目标的情况。
// 供管理后台展示告警，不阻断渠道保存（决策 6：多渠道映射不同是合法的负载均衡场景，
// 只有"看起来像手误"的不一致才值得提醒，硬拦会挡掉正当用法）。
type ModelMappingInconsistency struct {
	ModelName      string   `json:"model_name"`
	UpstreamTargets []string `json:"upstream_targets"`
	ChannelIds     []int    `json:"channel_ids"`
}

// CheckModelMappingConsistency 扫描所有启用渠道的 model_mapping，找出同一个
// 对外模型名在不同渠道被映射到不同上游目标的情况。
//
// 只报告"同一模型名 → 不同上游目标"这一种不一致；同一模型名在多个渠道映射到
// 相同目标(纯粹的负载均衡冗余)不算不一致，不告警。
func CheckModelMappingConsistency() ([]ModelMappingInconsistency, error) {
	var channels []Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).
		Select("id", "model_mapping").Find(&channels).Error; err != nil {
		return nil, err
	}

	// modelName -> upstreamTarget -> []channelId
	seen := make(map[string]map[string][]int)
	for _, ch := range channels {
		mapping := ch.GetModelMapping()
		modelMap, err := common.StrToMap(mapping)
		if err != nil || modelMap == nil {
			continue
		}
		for exposedName, rawTarget := range modelMap {
			// model_mapping 的值理应总是字符串,但它是管理员在后台文本框里手填的
			// JSON,防一下手误填了数字/布尔之类的畸形值 —— 静默跳过而不是让整次
			// 一致性检查因为一条渠道的错误配置而报错中断。
			upstreamTarget, ok := rawTarget.(string)
			if !ok || upstreamTarget == "" {
				continue
			}
			if seen[exposedName] == nil {
				seen[exposedName] = make(map[string][]int)
			}
			seen[exposedName][upstreamTarget] = append(seen[exposedName][upstreamTarget], ch.Id)
		}
	}

	var result []ModelMappingInconsistency
	for modelName, targets := range seen {
		if len(targets) <= 1 {
			continue
		}
		inconsistency := ModelMappingInconsistency{ModelName: modelName}
		for target, channelIds := range targets {
			inconsistency.UpstreamTargets = append(inconsistency.UpstreamTargets, target)
			inconsistency.ChannelIds = append(inconsistency.ChannelIds, channelIds...)
		}
		result = append(result, inconsistency)
	}
	return result, nil
}

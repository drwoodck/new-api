package model

func GetModelEnableGroups(modelName string) []string {
	// 确保缓存最新
	GetPricing()

	if modelName == "" {
		return make([]string, 0)
	}

	modelEnableGroupsLock.RLock()
	groups, ok := modelEnableGroups[modelName]
	modelEnableGroupsLock.RUnlock()
	if !ok {
		return make([]string, 0)
	}
	return groups
}

// GetModelQuotaTypes 返回指定模型的计费类型集合（来自缓存）
func GetModelQuotaTypes(modelName string) []int {
	GetPricing()

	modelEnableGroupsLock.RLock()
	quota, ok := modelQuotaTypeMap[modelName]
	modelEnableGroupsLock.RUnlock()
	if !ok {
		return []int{}
	}
	return []int{quota}
}

// IsGroupPricingEnabled 返回指定模型是否开启了「分组分别定价模式」(来自缓存)。
//
// 刻意**不**像 GetModelEnableGroups/GetModelQuotaTypes 那样先调 GetPricing()
// 强制刷新 —— 这个函数在计费热路径(relay/helper/price.go 的 ModelPriceHelper/
// ModelPriceHelperPerCall)上对每一次请求都会被调用,强制刷新意味着每次请求都
// 触发一次 abilities/channels/vendors 的整套查询,代价太大,而且这条路径原本
// 完全不依赖 DB 就能跑(不少单测直接调 ModelPriceHelper,从不设置 model.DB)。
// 缓存的新鲜度由写路径保证:CreateModelMeta/UpdateModelMeta 保存后已经调
// model.RefreshPricing() 强制重建过一次,管理员一开完关就是新值;此外
// GetPricing() 本身在系统其它读路径(/api/pricing、管理端模型列表等)上
// 几乎每次页面加载都会被调到,持续保持缓存新鲜。唯一的盲区是进程刚启动、
// 一次 GetPricing() 都还没跑过时的第一批请求——此时缓存为空,本函数会
// 如实返回 false(视为统一定价模式),不会主动去填它;这是可接受的冷启动
// 窗口,不值得让每次计费都多背一次 DB 查询。
func IsGroupPricingEnabled(modelName string) bool {
	modelEnableGroupsLock.RLock()
	enabled := modelGroupPricingEnabled[modelName]
	modelEnableGroupsLock.RUnlock()
	return enabled
}

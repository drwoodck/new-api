package setting

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// GroupMaxConcurrentTasks 按分组配置的最大同时在跑异步任务数(视频/图片生成等
// RelayTask 提交的任务)。0 或未配置 = 该分组不限制。
//
// 照 GetGroupRateLimit(model_request_rate_limit.go)的形状:全局 map + 读写锁,
// 管理后台改配置走 UpdateGroupMaxConcurrentTasksByJSONString。
var groupMaxConcurrentTasks = map[string]int{}
var groupMaxConcurrentTasksMutex sync.RWMutex

// GetGroupMaxConcurrentTasks 返回该分组的并发任务上限。found=false 表示该分组
// 未配置,调用方应视为不限制,而不是当成 0(0 是"未配置"与"限制为 0" 两种含义
// 里，这个函数只返回前者，"限制为 0" 需要调用方显式配置且非零校验后写入 map，
// 见 UpdateGroupMaxConcurrentTasksByJSONString)。
func GetGroupMaxConcurrentTasks(group string) (limit int, found bool) {
	groupMaxConcurrentTasksMutex.RLock()
	defer groupMaxConcurrentTasksMutex.RUnlock()

	if groupMaxConcurrentTasks == nil {
		return 0, false
	}
	limit, found = groupMaxConcurrentTasks[group]
	return limit, found
}

// UpdateGroupMaxConcurrentTasksByJSONString 从管理后台的 JSON 字符串更新分组并发配置。
// 形如 {"default": 5, "vip": 20}。值必须为正 —— 传 0 或负数的分组会被拒绝而不是
// 静默存成"不限制"(不限制的表达方式是不出现在这个 map 里，不是出现且为 0)。
func UpdateGroupMaxConcurrentTasksByJSONString(jsonStr string) error {
	parsed := make(map[string]int)
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	for group, limit := range parsed {
		if limit <= 0 {
			return fmt.Errorf("分组 %q 的并发上限必须为正数，得到 %d", group, limit)
		}
	}

	groupMaxConcurrentTasksMutex.Lock()
	defer groupMaxConcurrentTasksMutex.Unlock()
	groupMaxConcurrentTasks = parsed
	return nil
}

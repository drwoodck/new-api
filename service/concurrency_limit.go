package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// ErrGroupConcurrencyLimitReached 表示该分组已达到并发在跑任务上限。
type ErrGroupConcurrencyLimitReached struct {
	Group   string
	Current int64
	Limit   int
}

func (e *ErrGroupConcurrencyLimitReached) Error() string {
	return fmt.Sprintf("分组 %q 当前在跑任务数 %d 已达到上限 %d，请等待任务完成后重试", e.Group, e.Current, e.Limit)
}

// CheckGroupConcurrencyLimit 检查该用户所在分组的并发在跑任务数是否已达上限。
//
// 用 DB COUNT(*) 现算，不用内存/Redis 计数器（决策 5）：计数器要求每次任务终结时
// 递减，进程崩溃、面板直接改任务状态等路径都可能漏掉递减，计数器会永久性地比
// 真实值偏高，之后这个用户会被误判为一直超限。tasks 表的 COUNT(*) 每次都是当前
// 真实状态的直接查询，没有"漏更新"这一失效模式，代价是每次提交多一次 DB 查询——
// 与提交任务本身要写多张表相比可忽略。
//
// group 未在 setting.GetGroupMaxConcurrentTasks 配置时不限制（找不到 ≠ 限制为 0，
// 见该函数注释）。
func CheckGroupConcurrencyLimit(userId int, group string) error {
	limit, found := setting.GetGroupMaxConcurrentTasks(group)
	if !found {
		return nil
	}

	current, err := model.CountActiveTasksForUser(userId)
	if err != nil {
		return err
	}
	if current >= int64(limit) {
		return &ErrGroupConcurrencyLimitReached{Group: group, Current: current, Limit: limit}
	}
	return nil
}

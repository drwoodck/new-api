package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

// TestConcurrencyLimitUnconfiguredGroupIsUnlimited 锁住"找不到配置 = 不限制"
// 而不是"限制为 0"——setting.GetGroupMaxConcurrentTasks 的 found=false 分支。
func TestConcurrencyLimitUnconfiguredGroupIsUnlimited(t *testing.T) {
	require.NoError(t, setting.UpdateGroupMaxConcurrentTasksByJSONString(`{}`))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.Task{}).Error)

	for i := 0; i < 50; i++ {
		task := &model.Task{UserId: 910, TaskID: "t", Status: model.TaskStatusInProgress}
		require.NoError(t, task.Insert())
	}

	err := CheckGroupConcurrencyLimit(910, "no-such-group")
	require.NoError(t, err, "未配置的分组不应被限制，无论已有多少在跑任务")
}

// TestConcurrencyLimitBlocksAtThreshold 锁住 >= 上限即拒绝（不是 > 上限），
// 且错误里带上当前数与上限，不能只说"请稍后再试"。
func TestConcurrencyLimitBlocksAtThreshold(t *testing.T) {
	require.NoError(t, setting.UpdateGroupMaxConcurrentTasksByJSONString(`{"vip": 2}`))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.Task{}).Error)

	task1 := &model.Task{UserId: 911, TaskID: "a", Status: model.TaskStatusQueued}
	task2 := &model.Task{UserId: 911, TaskID: "b", Status: model.TaskStatusInProgress}
	require.NoError(t, task1.Insert())
	require.NoError(t, task2.Insert())

	err := CheckGroupConcurrencyLimit(911, "vip")
	require.Error(t, err)
	limitErr, ok := err.(*ErrGroupConcurrencyLimitReached)
	require.True(t, ok, "错误类型必须是 *ErrGroupConcurrencyLimitReached，调用方要能取出 Current/Limit")
	require.EqualValues(t, 2, limitErr.Current)
	require.Equal(t, 2, limitErr.Limit)
	require.Contains(t, err.Error(), "2")
}

// TestConcurrencyLimitAllowsBelowThreshold 确认未达上限时放行，且只统计该用户
// 自己的在跑任务，不受其他用户影响。
func TestConcurrencyLimitAllowsBelowThreshold(t *testing.T) {
	require.NoError(t, setting.UpdateGroupMaxConcurrentTasksByJSONString(`{"default": 3}`))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.Task{}).Error)

	task := &model.Task{UserId: 912, TaskID: "c", Status: model.TaskStatusQueued}
	require.NoError(t, task.Insert())
	otherUserTask := &model.Task{UserId: 913, TaskID: "d", Status: model.TaskStatusInProgress}
	require.NoError(t, otherUserTask.Insert())

	err := CheckGroupConcurrencyLimit(912, "default")
	require.NoError(t, err)
}

// TestConcurrencyLimitIgnoresFinishedTasks 确认 success/failure 状态的任务
// 不计入在跑数——否则用户的历史任务会永久占着并发槽位。
func TestConcurrencyLimitIgnoresFinishedTasks(t *testing.T) {
	require.NoError(t, setting.UpdateGroupMaxConcurrentTasksByJSONString(`{"default": 1}`))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.Task{}).Error)

	for i := 0; i < 10; i++ {
		task := &model.Task{UserId: 914, TaskID: "done", Status: model.TaskStatusSuccess}
		require.NoError(t, task.Insert())
	}

	err := CheckGroupConcurrencyLimit(914, "default")
	require.NoError(t, err, "已终结的任务不该占用并发槽位")
}

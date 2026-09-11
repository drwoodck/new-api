package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTaskQueryFiltersByModelName 锁住「按模型筛任务」的口径,并确认**列表与
// 计数走的是同一套条件** —— 这两处是各写一遍的,编译器管不着,漏改一处后台
// 就会出现「筛出 2 条、总数显示 4」这种对不上的账。
//
// 同时钉住:ModelName 为空(加列之前的存量行)只在**不筛模型**时出现,
// 一旦按模型筛就被排除 —— 这是有意的,存量行没有模型名可匹配。
func TestTaskQueryFiltersByModelName(t *testing.T) {
	require.NoError(t, DB.Where("1 = 1").Delete(&Task{}).Error)

	rows := []*Task{
		{UserId: 901, TaskID: "mf1", ModelName: "seedance-2.5", Status: TaskStatusSuccess, SubmitTime: 100},
		{UserId: 901, TaskID: "mf2", ModelName: "seedance-2.5", Status: TaskStatusFailure, SubmitTime: 200},
		{UserId: 901, TaskID: "mf3", ModelName: "nano-banana-2", Status: TaskStatusSuccess, SubmitTime: 300},
		// 加列之前的存量行:ModelName 为空
		{UserId: 901, TaskID: "mf4", ModelName: "", Status: TaskStatusSuccess, SubmitTime: 400},
	}
	for _, r := range rows {
		require.NoError(t, r.Insert())
	}

	byModel := SyncTaskQueryParams{ModelName: "seedance-2.5"}
	list := TaskGetAllTasks(0, 10, byModel)
	require.Len(t, list, 2)
	for _, task := range list {
		require.Equal(t, "seedance-2.5", task.ModelName)
	}
	require.EqualValues(t, 2, TaskCountAllTasks(byModel), "列表与计数必须同口径")

	// 不传模型:存量行也回得来
	all := SyncTaskQueryParams{}
	require.Len(t, TaskGetAllTasks(0, 10, all), 4)
	require.EqualValues(t, 4, TaskCountAllTasks(all))

	// 与其它条件叠加时也要两边一致
	combined := SyncTaskQueryParams{ModelName: "seedance-2.5", Status: string(TaskStatusFailure)}
	require.Len(t, TaskGetAllTasks(0, 10, combined), 1)
	require.EqualValues(t, 1, TaskCountAllTasks(combined))

	// 不存在的模型:两边都空。若只给一边加了条件,这里立刻红
	none := SyncTaskQueryParams{ModelName: "no-such-model"}
	require.Empty(t, TaskGetAllTasks(0, 10, none))
	require.EqualValues(t, 0, TaskCountAllTasks(none))

	// 用户自查路径是另一对函数(各写一遍、字段顺序都不同),同样要覆盖 ——
	// controller.GetUserTask 会把 model_name 传进来,这里不消费就是静默失效。
	require.Len(t, TaskGetAllUserTask(901, 0, 10, byModel), 2)
	require.EqualValues(t, 2, TaskCountAllUserTask(901, byModel), "用户路径的列表与计数也要同口径")
	require.Empty(t, TaskGetAllUserTask(902, 0, 10, byModel), "别的用户没有任务")
	require.EqualValues(t, 0, TaskCountAllUserTask(902, byModel))
}

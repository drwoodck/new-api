package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCountActiveTasksForUser 锁住 CountActiveTasksForUser 的状态集口径:
// queued/submitted/in_progress 算在跑，success/failure 不算，且只统计该用户自己的。
func TestCountActiveTasksForUser(t *testing.T) {
	require.NoError(t, DB.Where("1 = 1").Delete(&Task{}).Error)

	rows := []*Task{
		{UserId: 901, TaskID: "t1", Status: TaskStatusQueued},
		{UserId: 901, TaskID: "t2", Status: TaskStatusSubmitted},
		{UserId: 901, TaskID: "t3", Status: TaskStatusInProgress},
		{UserId: 901, TaskID: "t4", Status: TaskStatusSuccess},  // 已终结，不算
		{UserId: 901, TaskID: "t5", Status: TaskStatusFailure},  // 已终结，不算
		{UserId: 902, TaskID: "t6", Status: TaskStatusQueued},   // 别的用户，不算
	}
	for _, r := range rows {
		require.NoError(t, r.Insert())
	}

	count, err := CountActiveTasksForUser(901)
	require.NoError(t, err)
	require.EqualValues(t, 3, count, "queued+submitted+in_progress 三条，success/failure 不计入")

	otherCount, err := CountActiveTasksForUser(902)
	require.NoError(t, err)
	require.EqualValues(t, 1, otherCount)

	zeroCount, err := CountActiveTasksForUser(999)
	require.NoError(t, err)
	require.EqualValues(t, 0, zeroCount)
}

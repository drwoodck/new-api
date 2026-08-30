package model

import (
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupCanvasCatalogTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&CanvasCatalogModel{}))
}

func TestCanvasCatalogInsert(t *testing.T) {
	setupCanvasCatalogTestDB(t)
	c := &CanvasCatalogModel{
		RemoteID: "test-model-1",
		DisplayName: "Test Model",
		Capabilities: "video_gen",
		Enabled: true,
		Contract: "relay_video_async_v1",
		RequiresVocab: 1,
		SortOrder: 100,
	}
	err := c.Insert()
	require.NoError(t, err)
	assert.NotZero(t, c.Id)
	assert.NotZero(t, c.CreatedTime)
	assert.NotZero(t, c.UpdatedTime)
	assert.Equal(t, c.CreatedTime, c.UpdatedTime)
}

// TestCanvasCatalogUpdateWritesZeroValues 锁住 GORM"结构体形式 Updates 跳过零值字段"
// 的坑:Enabled 从 true 改为 false 必须真正落到数据库,而不是被静默忽略。
func TestCanvasCatalogUpdateWritesZeroValues(t *testing.T) {
	setupCanvasCatalogTestDB(t)
	c := &CanvasCatalogModel{
		RemoteID: "toggle-me", DisplayName: "Toggle Me", Capabilities: "video_gen",
		Enabled: true, Contract: "relay_video_async_v1", RequiresVocab: 1,
	}
	require.NoError(t, c.Insert())

	c.Enabled = false
	require.NoError(t, c.Update())

	reloaded, err := GetCanvasCatalogModelByID(c.Id)
	require.NoError(t, err)
	assert.False(t, reloaded.Enabled, "Enabled=false must persist, not be skipped as a zero value")
}

func TestGetCanvasCatalog(t *testing.T) {
	setupCanvasCatalogTestDB(t)

	models := []*CanvasCatalogModel{
		{RemoteID: "m1", DisplayName: "Model 1", Capabilities: "video_gen", Enabled: true, Contract: "c1", RequiresVocab: 1, SortOrder: 10},
		{RemoteID: "m2", DisplayName: "Model 2", Capabilities: "image_gen", Enabled: false, Contract: "c2", RequiresVocab: 1, SortOrder: 20},
		{RemoteID: "m3", DisplayName: "Model 3", Capabilities: "video_gen", Enabled: true, Contract: "c1", RequiresVocab: 1, SortOrder: 5},
	}
	for _, m := range models {
		require.NoError(t, m.Insert())
	}

	result, version, err := GetCanvasCatalog()
	require.NoError(t, err)
	assert.Equal(t, int64(3), version) // total count including disabled
	// 契约要求返回全部条目(含禁用):客户端要靠 enabled=false 做软下线,
	// 服务端过滤停用项会让客户端无法区分"停用"与"已删除"。
	assert.Len(t, result, 3)
	assert.Equal(t, "m3", result[0].RemoteID) // sorted by SortOrder
	assert.Equal(t, "m1", result[1].RemoteID)
	assert.Equal(t, "m2", result[2].RemoteID)
	assert.False(t, result[2].Enabled)
}

// TestGetCanvasCatalogNeverRewritesEnabled 锁住模型层的两条不变量:
// 既不按分组删行,也**不改写 Enabled**。
//
// 这个测试存在的理由是一次被否掉的设计:曾打算让分组不可见的条目下发
// Enabled=false,以此既保留行又隐藏模型。那样会出事 —— 画布把 Enabled 直接
// 写进本地 models.enabled 列,而那一列同时是用户自己的模型勾选开关,
// 且同步冲突时无条件覆写。于是每次同步都会静默清掉用户的选择。
//
// 分组可见性因此改为 controller 层的独立 group_visible 字段。
// 模型层只管如实返回存了什么。
func TestGetCanvasCatalogNeverRewritesEnabled(t *testing.T) {
	setupCanvasCatalogTestDB(t)

	models := []*CanvasCatalogModel{
		{RemoteID: "m1", DisplayName: "Model 1", Capabilities: "video_gen", Enabled: true, Contract: "c1", RequiresVocab: 1, SortOrder: 10},
		{RemoteID: "m2", DisplayName: "Model 2", Capabilities: "image_gen", Enabled: true, Contract: "c2", RequiresVocab: 1, SortOrder: 20},
		{RemoteID: "m3", DisplayName: "Model 3 (already disabled)", Capabilities: "video_gen", Enabled: false, Contract: "c1", RequiresVocab: 1, SortOrder: 5},
	}
	for _, m := range models {
		require.NoError(t, m.Insert())
	}

	result, version, err := GetCanvasCatalog()
	require.NoError(t, err)

	// catalog_version 是全局单调代理(全部行计数),与分组无关
	assert.Equal(t, int64(3), version)
	require.Len(t, result, 3, "模型层绝不删行")

	byRemoteID := make(map[string]*CanvasCatalogModel, len(result))
	for i := range result {
		byRemoteID[result[i].RemoteID] = &result[i]
	}

	// Enabled 一律是库里存的值,与任何分组信息无关
	assert.True(t, byRemoteID["m1"].Enabled, "m1 存的是 true,必须原样返回")
	assert.True(t, byRemoteID["m2"].Enabled, "m2 存的是 true,即便某分组用不了它也不能被改成 false")
	assert.False(t, byRemoteID["m3"].Enabled, "m3 存的是 false,原样返回")
}

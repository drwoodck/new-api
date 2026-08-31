package model

import (
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupCanvasCatalogTestDB 给每个测试一个**独立**的内存库。
//
// 不能用 `file::memory:?cache=shared` —— 那个 DSN 让同包所有测试共用一个库,
// 行会在测试之间累积(实测:期望 3 行拿到 6 行)。每个测试用自己的命名库
// (t.Name() 保证唯一)并在 Cleanup 里关掉连接,库随最后一个连接关闭而消失。
// boolPtr 是 CanvasCatalogModel.Enabled 变成 *bool 之后的测试辅助。
// 指针是必需的:该字段带 gorm default:true,bool 零值会被 GORM 当成
// 「未设置」而写入默认值,导致 enabled=false 根本插不进去。
func boolPtr(b bool) *bool { return &b }

func setupCanvasCatalogTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	// package 级 DB 是 TestMain 建的共享实例，装了 User/UserSession/Task 等一整套表——
	// 本包其它文件的测试直接假设它一直在。此前这里只关连接、不还原 DB，
	// 导致跑在这个测试之后的任何测试都会撞上 "no such table: tasks" 之类的错误
	// （同一个坑在 device_binding_test.go 和 catalog_cascade_test.go 里也出现过，
	// 这是这三个文件里最后一个没修的）。
	original := DB
	t.Cleanup(func() {
		DB = original
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	DB = db
	require.NoError(t, db.AutoMigrate(&CanvasCatalogModel{}))
}

func TestCanvasCatalogInsert(t *testing.T) {
	setupCanvasCatalogTestDB(t)
	c := &CanvasCatalogModel{
		RemoteID: "test-model-1",
		DisplayName: "Test Model",
		Capabilities: "video_gen",
		Enabled: boolPtr(true),
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

// TestCanvasCatalogInsertWritesDisabledEntry 锁住 Create 侧的零值坑:
// Enabled 带 `gorm:"default:true"`,GORM 对 Create 会把零值字段交给数据库默认值,
// 于是 Enabled=false 被静默写成 true —— 管理员新建一个「建好但先不上架」的条目,
// 建出来是已上架的。修法是 Insert() 里显式 Select("*")。
func TestCanvasCatalogInsertWritesDisabledEntry(t *testing.T) {
	setupCanvasCatalogTestDB(t)
	c := &CanvasCatalogModel{
		RemoteID: "born-disabled", DisplayName: "Born Disabled", Capabilities: "video_gen",
		Enabled: boolPtr(false), Contract: "relay_video_async_v1", RequiresVocab: 1,
	}
	require.NoError(t, c.Insert())

	reloaded, err := GetCanvasCatalogModelByID(c.Id)
	require.NoError(t, err)
	assert.False(t, reloaded.IsEnabled(),
		"Enabled=false 必须落库,不能被 gorm default:true 顶替")
}

// TestCanvasCatalogUpdateWritesZeroValues 锁住 GORM"结构体形式 Updates 跳过零值字段"
// 的坑:Enabled 从 true 改为 false 必须真正落到数据库,而不是被静默忽略。
func TestCanvasCatalogUpdateWritesZeroValues(t *testing.T) {
	setupCanvasCatalogTestDB(t)
	c := &CanvasCatalogModel{
		RemoteID: "toggle-me", DisplayName: "Toggle Me", Capabilities: "video_gen",
		Enabled: boolPtr(true), Contract: "relay_video_async_v1", RequiresVocab: 1,
	}
	require.NoError(t, c.Insert())

	c.Enabled = boolPtr(false)
	require.NoError(t, c.Update())

	reloaded, err := GetCanvasCatalogModelByID(c.Id)
	require.NoError(t, err)
	assert.False(t, reloaded.IsEnabled(), "Enabled=false must persist, not be skipped as a zero value")
}

func TestGetCanvasCatalog(t *testing.T) {
	setupCanvasCatalogTestDB(t)

	models := []*CanvasCatalogModel{
		{RemoteID: "m1", DisplayName: "Model 1", Capabilities: "video_gen", Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1, SortOrder: 10},
		{RemoteID: "m2", DisplayName: "Model 2", Capabilities: "image_gen", Enabled: boolPtr(false), Contract: "c2", RequiresVocab: 1, SortOrder: 20},
		{RemoteID: "m3", DisplayName: "Model 3", Capabilities: "video_gen", Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1, SortOrder: 5},
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
	assert.False(t, result[2].IsEnabled())
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
		{RemoteID: "m1", DisplayName: "Model 1", Capabilities: "video_gen", Enabled: boolPtr(true), Contract: "c1", RequiresVocab: 1, SortOrder: 10},
		{RemoteID: "m2", DisplayName: "Model 2", Capabilities: "image_gen", Enabled: boolPtr(true), Contract: "c2", RequiresVocab: 1, SortOrder: 20},
		{RemoteID: "m3", DisplayName: "Model 3 (already disabled)", Capabilities: "video_gen", Enabled: boolPtr(false), Contract: "c1", RequiresVocab: 1, SortOrder: 5},
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
	assert.True(t, byRemoteID["m1"].IsEnabled(), "m1 存的是 true,必须原样返回")
	assert.True(t, byRemoteID["m2"].IsEnabled(), "m2 存的是 true,即便某分组用不了它也不能被改成 false")
	assert.False(t, byRemoteID["m3"].IsEnabled(), "m3 存的是 false,原样返回")
}

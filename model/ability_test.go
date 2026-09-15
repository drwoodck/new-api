package model

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAbilityTestDB 给 ensureModelsExist 一个独立内存库，同
// catalog_cascade_test.go 的夹具 —— 换掉全局 DB，因为夹具里的 Insert() 自己
// 走全局 DB（它不接受 db 参数）。
func setupAbilityTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:ability_%s?mode=memory&cache=private", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Model{}))
	saved := DB
	DB = db
	t.Cleanup(func() {
		DB = saved
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

// loadModelRow 读回一行。读操作不受 GORM default 标签影响（只有写会），
// 所以这里拿到的就是列里真实存着的东西。
func loadModelRow(t *testing.T, name string) Model {
	t.Helper()
	var rows []Model
	require.NoError(t, DB.Where("model_name = ?", name).Find(&rows).Error)
	require.Len(t, rows, 1, "模型 %q 应当存在", name)
	return rows[0]
}

// 渠道保存时 ensureModelsExist 建的行必须落为 Official Sync(=1)。
//
// 这条断言守的不是字面值，而是「别让 default:1 的标签替我们做决定」。
// sync_official 是「本行是否跟随自动同步」的闸门：元信息富化与 status 级联
// 都以它为准，取 0 则整行被静默跳过。而带 default:1 标签的列在写入零值时会被
// GORM 从 INSERT 里整个省略、由数据库默认值接管（model/model_meta.go:69 的
// 注释自陈此事，Model.Insert() 正为此做了二段式写回）—— 也就是说，源码里写
// 0 还是写 1，在读回数据库之前都看不出来。
//
// 这里必须从库里读回来看真值：断言对象是落库结果，不是结构体字段。
func TestEnsureModelsExistWritesIntendedSyncOfficial(t *testing.T) {
	setupAbilityTestDB(t)

	require.NoError(t, ensureModelsExist(DB, map[string]struct{}{"sync-official-probe": {}}))

	row := loadModelRow(t, "sync-official-probe")
	assert.Equal(t, 1, row.SyncOfficial,
		"渠道建的 models 行必须落为 Official Sync(=1)；落成 0 会让富化与 status 级联整行静默跳过")
	assert.Equal(t, 1, row.Status, "随渠道新建的行应当已启用")
}

// 已存在的行绝不能被渠道保存改写 sync_official。
//
// 管理员把某行切成 No Sync 之后，哪怕该模型仍挂在渠道里、渠道被反复保存，
// 这个「手动接管」也必须活着 —— 这是自定义级联唯一的逃逸口。它靠
// Create 的 OnConflict{DoNothing} 保证，一旦改成 Upsert 就会被打破。
func TestEnsureModelsExistKeepsNoSyncOnExistingRow(t *testing.T) {
	setupAbilityTestDB(t)

	require.NoError(t, (&Model{ModelName: "manual-takeover", Status: 0, SyncOfficial: 0}).Insert())
	// 先确认夹具真的造出了 0/0 的行：Insert() 走的也是 Create，不校验的话
	// 这里可能已经被 default 标签顶成 1，后面的断言就变成了假绿。
	fixture := loadModelRow(t, "manual-takeover")
	require.Equal(t, 0, fixture.SyncOfficial, "夹具失败：No Sync 行没造出来")
	require.Equal(t, 0, fixture.Status, "夹具失败：禁用行没造出来")

	require.NoError(t, ensureModelsExist(DB, map[string]struct{}{"manual-takeover": {}}))

	row := loadModelRow(t, "manual-takeover")
	assert.Equal(t, 0, row.SyncOfficial, "渠道保存不得把管理员的 No Sync 改回 Official Sync")
	assert.Equal(t, 0, row.Status, "渠道保存不得覆盖已存在行的状态")
}

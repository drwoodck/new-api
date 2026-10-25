package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupModelMetadataTestDB 给每个用例一个独立内存库,并在结束时把包级 DB 还原成
// TestMain 建的共享实例。
//
// 这里以前用 InitDB()/CloseDB():CloseDB() 关的是**全局** DB —— TestMain 那份装好
// User/Task/Token 等一整套表的共享库被一起关掉,于是本包按文件序排在这些用例之后
// 的测试(如 model_owner_test.go)全部撞上 "sql: database is closed"。
// 单跑这几个用例永远是绿的,跑整包才炸 —— 典型的测试间污染。
func setupModelMetadataTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:model_metadata_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ModelMetadata{}))

	originalDB, originalLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestModelMetadata_CRUD(t *testing.T) {
	setupModelMetadataTestDB(t)

	// 插入测试数据
	paramSchema := `{"fields":[{"name":"duration","type":"number"}]}`
	meta := &ModelMetadata{
		ModelName:   "test-model",
		ParamSchema: &paramSchema,
	}
	err := UpsertModelMetadata(meta)
	require.NoError(t, err)

	// 查询单个
	fetched, err := GetModelMetadata("test-model")
	require.NoError(t, err)
	assert.Equal(t, "test-model", fetched.ModelName)
	assert.NotNil(t, fetched.ParamSchema)
	assert.Contains(t, *fetched.ParamSchema, "duration")

	// 批量查询
	metaMap, err := GetModelMetadataMap([]string{"test-model", "non-existent"})
	require.NoError(t, err)
	assert.Len(t, metaMap, 1)
	assert.Contains(t, metaMap, "test-model")
}

func TestGetModelMetadataMap_EmptyInput(t *testing.T) {
	setupModelMetadataTestDB(t)

	result, err := GetModelMetadataMap([]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUpsertModelMetadata_SecondWriteUpdatesInPlace(t *testing.T) {
	setupModelMetadataTestDB(t)

	first := `{"prompt":{"type":"string"}}`
	require.NoError(t, UpsertModelMetadata(&ModelMetadata{
		ModelName:   "lec-example-upsert",
		ParamSchema: &first,
	}))

	second := `{"prompt":{"type":"string"},"duration":{"type":"integer"}}`
	require.NoError(t, UpsertModelMetadata(&ModelMetadata{
		ModelName:   "lec-example-upsert",
		ParamSchema: &second,
	}))

	// 主键冲突必须走更新而不是插成第二行 —— 否则每跑一次导入脚本
	// 就堆一行同名记录,目录端点查回来的那一行还是随机的
	var count int64
	require.NoError(t, DB.Model(&ModelMetadata{}).
		Where("model_name = ?", "lec-example-upsert").Count(&count).Error)
	assert.Equal(t, int64(1), count)

	fetched, err := GetModelMetadata("lec-example-upsert")
	require.NoError(t, err)
	require.NotNil(t, fetched.ParamSchema)
	assert.Equal(t, second, *fetched.ParamSchema)
}

func TestBatchUpsertModelMetadata_WritesAll(t *testing.T) {
	setupModelMetadataTestDB(t)

	schema := `{"prompt":{"type":"string"}}`
	batch := []*ModelMetadata{
		{ModelName: "lec-example-b1", ParamSchema: &schema},
		{ModelName: "lec-example-b2", ParamSchema: &schema},
	}
	require.NoError(t, BatchUpsertModelMetadata(batch))

	got, err := GetModelMetadataMap([]string{"lec-example-b1", "lec-example-b2"})
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// 空输入不该报错(导入脚本在目录为空时走到这里)
	assert.NoError(t, BatchUpsertModelMetadata(nil))
}

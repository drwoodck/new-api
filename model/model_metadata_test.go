package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMetadata_CRUD(t *testing.T) {
	err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	// 确保表存在
	err = DB.AutoMigrate(&ModelMetadata{})
	require.NoError(t, err)

	// 插入测试数据
	paramSchema := `{"fields":[{"name":"duration","type":"number"}]}`
	meta := &ModelMetadata{
		ModelName:   "test-model",
		ParamSchema: &paramSchema,
	}
	err = UpsertModelMetadata(meta)
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
	err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDB()

	result, err := GetModelMetadataMap([]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUpsertModelMetadata_SecondWriteUpdatesInPlace(t *testing.T) {
	require.NoError(t, InitDB())
	defer CloseDB()
	require.NoError(t, DB.AutoMigrate(&ModelMetadata{}))

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
	require.NoError(t, InitDB())
	defer CloseDB()
	require.NoError(t, DB.AutoMigrate(&ModelMetadata{}))

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

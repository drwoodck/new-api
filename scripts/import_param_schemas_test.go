package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exampleSchema = `{"prompt":{"type":"string","required":true}}`

func TestBuildImportPlan_KeysByRemoteIDNotCanvasID(t *testing.T) {
	// 目录端点查 model_metadata 用的是 canvas_catalog_models.remote_id,
	// 不是画布内部 id。用错这一个字,功能就会静默什么都不做 ——
	// 这是本功能最容易犯、且最难发现的错误,单独钉一条测试。
	idToRemote := map[string]string{"paipu-example-1": "lec-example-1"}
	idToSchema := map[string]string{"paipu-example-1": exampleSchema}

	plan := buildImportPlan(idToRemote, idToSchema, []string{"lec-example-1"})

	require.Len(t, plan.toImport, 1)
	assert.Equal(t, "lec-example-1", plan.toImport[0].remoteID,
		"写入键必须是 remote_id;写成画布 id 目录端点就查不到")
	assert.Equal(t, "paipu-example-1", plan.toImport[0].canvasID)
	assert.Equal(t, exampleSchema, plan.toImport[0].schema)
	assert.Empty(t, plan.notInCatalog)
	assert.Empty(t, plan.catalogWithoutSchema)
}

func TestBuildImportPlan_NoOverlapWhenPrefixesDisagree(t *testing.T) {
	// 两边命名规则不一致(例如中转站改过前缀)时的形态:
	// 一个都进不了导入集,两边各自的「落单」清单都要报出来,
	// 否则调用方看到的只是「导入 0 条」,无从判断原因。
	idToRemote := map[string]string{"paipu-example-1": "lec-example-1"}
	idToSchema := map[string]string{"paipu-example-1": exampleSchema}

	plan := buildImportPlan(idToRemote, idToSchema, []string{"kungai-example-1"})

	assert.Empty(t, plan.toImport)
	assert.Equal(t, []string{"lec-example-1"}, plan.notInCatalog)
	assert.Equal(t, []string{"kungai-example-1"}, plan.catalogWithoutSchema)
}

func TestBuildImportPlan_SkipsWhenCanvasHasNoSchema(t *testing.T) {
	idToRemote := map[string]string{"paipu-example-1": "lec-example-1"}
	idToSchema := map[string]string{} // 画布没有对应的 endpoint profile

	plan := buildImportPlan(idToRemote, idToSchema, []string{"lec-example-1"})

	assert.Empty(t, plan.toImport)
	assert.Equal(t, []string{"paipu-example-1"}, plan.noCanvasSchema)
	// remote_id 虽然不在导入集里,但它确实在目录表中,不该被误报为「目录没有」
	assert.Empty(t, plan.notInCatalog)
	assert.Equal(t, []string{"lec-example-1"}, plan.catalogWithoutSchema)
}

func TestBuildImportPlan_CountsCatalogIDsDeduped(t *testing.T) {
	// canvas_catalog_models.remote_id 只有普通索引、没有唯一约束,
	// 表里可能有多行同名,计数按去重后算。
	plan := buildImportPlan(map[string]string{}, map[string]string{},
		[]string{"lec-example-1", "lec-example-1", "lec-example-2"})

	assert.Equal(t, 2, plan.catalogCount)
	assert.ElementsMatch(t, []string{"lec-example-1", "lec-example-2"}, plan.catalogWithoutSchema)
}

func TestBuildImportPlan_OutputIsSorted(t *testing.T) {
	// 排序只为让报告可 diff;map 迭代顺序随机,不排的话两次运行输出会乱
	idToRemote := map[string]string{
		"paipu-c": "lec-c",
		"paipu-a": "lec-a",
		"paipu-b": "lec-b",
	}
	idToSchema := map[string]string{
		"paipu-a": exampleSchema,
		"paipu-b": exampleSchema,
		"paipu-c": exampleSchema,
	}

	plan := buildImportPlan(idToRemote, idToSchema, []string{"lec-a", "lec-b", "lec-c"})

	require.Len(t, plan.toImport, 3)
	assert.Equal(t, []string{"lec-a", "lec-b", "lec-c"}, []string{
		plan.toImport[0].remoteID, plan.toImport[1].remoteID, plan.toImport[2].remoteID,
	})
}

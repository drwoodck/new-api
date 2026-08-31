package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCatalogCascadeTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:catalog_cascade_%s?mode=memory&cache=private", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&CanvasCatalogModel{}, &Ability{}, &Channel{}))
	saved := DB
	DB = db
	t.Cleanup(func() {
		DB = saved
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
}

func boolPtrCascade(v bool) *bool { return &v }

// 模型挂两个渠道，停一个：目录条目必须保持启用。这条最容易写错——
// 判据必须是"再无任何启用渠道"，不是"某个渠道停了"。
func TestSoftDisableUncoveredCatalogEntries_KeepsEnabledWhenAnotherChannelStillCovers(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	entry := &CanvasCatalogModel{
		RemoteID: "seedance-2.0", DisplayName: "Seedance 2.0", Contract: "relay_video_async_v1",
		Enabled: boolPtrCascade(true),
	}
	require.NoError(t, entry.Insert())

	// 两个渠道都暴露同一 model 名，其中一个已停用（enabled=false）
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "seedance-2.0", ChannelId: 1, Enabled: false}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "seedance-2.0", ChannelId: 2, Enabled: true}).Error)

	require.NoError(t, SoftDisableUncoveredCatalogEntries())

	reloaded, err := GetCanvasCatalogModelByID(entry.Id)
	require.NoError(t, err)
	assert.True(t, reloaded.IsEnabled(), "另一个渠道还在启用，目录条目不该被软停用")
}

// 模型的最后一个渠道停用：目录条目必须被软停用。
func TestSoftDisableUncoveredCatalogEntries_DisablesWhenLastChannelStops(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	entry := &CanvasCatalogModel{
		RemoteID: "seedance-2.0", DisplayName: "Seedance 2.0", Contract: "relay_video_async_v1",
		Enabled: boolPtrCascade(true),
	}
	require.NoError(t, entry.Insert())

	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "seedance-2.0", ChannelId: 1, Enabled: false}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "seedance-2.0", ChannelId: 2, Enabled: false}).Error)

	require.NoError(t, SoftDisableUncoveredCatalogEntries())

	reloaded, err := GetCanvasCatalogModelByID(entry.Id)
	require.NoError(t, err)
	assert.False(t, reloaded.IsEnabled(), "两个渠道都停了，目录条目应被软停用")
}

// 已被运营方手动停用的条目，渠道恢复后不会被自动重新启用——只单向。
func TestSoftDisableUncoveredCatalogEntries_NeverReEnables(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	entry := &CanvasCatalogModel{
		RemoteID: "seedance-2.0", DisplayName: "Seedance 2.0", Contract: "relay_video_async_v1",
		Enabled: boolPtrCascade(false), // 运营方手动停用
	}
	require.NoError(t, entry.Insert())

	// 渠道恢复启用
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "seedance-2.0", ChannelId: 1, Enabled: true}).Error)

	require.NoError(t, SoftDisableUncoveredCatalogEntries())

	reloaded, err := GetCanvasCatalogModelByID(entry.Id)
	require.NoError(t, err)
	assert.False(t, reloaded.IsEnabled(), "函数只单向软停用，绝不会把已停用的条目改回启用")
}

// 目录里本来就没有启用条目：函数应是空操作，不报错。
func TestSoftDisableUncoveredCatalogEntries_NoEnabledEntriesIsNoop(t *testing.T) {
	setupCatalogCascadeTestDB(t)
	require.NoError(t, SoftDisableUncoveredCatalogEntries())
}

func strPtrCascade(s string) *string { return &s }

// 同一模型名在不同渠道映射到不同上游目标：告警，但不阻断（决策 6）。
func TestCheckModelMappingConsistency_FlagsDifferentTargets(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	require.NoError(t, DB.Create(&Channel{
		Id: 1, Status: common.ChannelStatusEnabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-08-06"}`),
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id: 2, Status: common.ChannelStatusEnabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-11-20"}`),
	}).Error)

	inconsistencies, err := CheckModelMappingConsistency()
	require.NoError(t, err)
	require.Len(t, inconsistencies, 1)
	assert.Equal(t, "gpt-4o", inconsistencies[0].ModelName)
	assert.ElementsMatch(t, []string{"gpt-4o-2024-08-06", "gpt-4o-2024-11-20"}, inconsistencies[0].UpstreamTargets)
}

// 同一模型名在多个渠道映射到相同目标：纯粹的负载均衡冗余，不算不一致。
func TestCheckModelMappingConsistency_SameTargetIsNotFlagged(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	require.NoError(t, DB.Create(&Channel{
		Id: 1, Status: common.ChannelStatusEnabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-08-06"}`),
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id: 2, Status: common.ChannelStatusEnabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-08-06"}`),
	}).Error)

	inconsistencies, err := CheckModelMappingConsistency()
	require.NoError(t, err)
	assert.Empty(t, inconsistencies, "相同目标是合法的负载均衡冗余，不该告警")
}

// 已停用的渠道不参与一致性检查——它已经不在分发路径里了。
func TestCheckModelMappingConsistency_IgnoresDisabledChannels(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	require.NoError(t, DB.Create(&Channel{
		Id: 1, Status: common.ChannelStatusEnabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-08-06"}`),
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id: 2, Status: common.ChannelStatusManuallyDisabled,
		ModelMapping: strPtrCascade(`{"gpt-4o":"gpt-4o-2024-11-20"}`),
	}).Error)

	inconsistencies, err := CheckModelMappingConsistency()
	require.NoError(t, err)
	assert.Empty(t, inconsistencies, "已停用的渠道不在分发路径里，不该参与一致性判定")
}

// 空 model_mapping（未配置映射）不应导致 panic 或误报。
func TestCheckModelMappingConsistency_HandlesEmptyMapping(t *testing.T) {
	setupCatalogCascadeTestDB(t)

	require.NoError(t, DB.Create(&Channel{Id: 1, Status: common.ChannelStatusEnabled, ModelMapping: nil}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Status: common.ChannelStatusEnabled, ModelMapping: strPtrCascade("")}).Error)

	inconsistencies, err := CheckModelMappingConsistency()
	require.NoError(t, err)
	assert.Empty(t, inconsistencies)
}

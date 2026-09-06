package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func newDraftChannel(id int, channelType int) *model.Channel {
	return &model.Channel{Id: id, Type: channelType, Key: "sk-draft", Name: "draft-channel", Status: common.ChannelStatusEnabled}
}

func countCanvasRemoteID(t *testing.T, remoteID string) int64 {
	t.Helper()
	var cnt int64
	require.NoError(t, model.DB.Unscoped().Model(&model.CanvasCatalogModel{}).
		Where("remote_id = ?", remoteID).Count(&cnt).Error)
	return cnt
}

func countModelMeta(t *testing.T, modelName string) int64 {
	t.Helper()
	var cnt int64
	require.NoError(t, model.DB.Unscoped().Model(&model.Model{}).
		Where("model_name = ?", modelName).Count(&cnt).Error)
	return cnt
}

// TestDraftCatalogCreatesEntryAndMetaRow 钉住新模型起草:目录条目创建
// (enabled=false、display_name=模型名、capabilities=video_gen、contract 由
// ContractForCapability 推导为 relay_video_async_v1),models 行创建(status=0)。
func TestDraftCatalogCreatesEntryAndMetaRow(t *testing.T) {
	truncate(t)
	drafted, err := DraftCatalogEntries(newDraftChannel(1, constant.ChannelTypeSora), []string{"draft-sora-model"})
	require.NoError(t, err)
	require.Equal(t, 1, drafted)

	var entry model.CanvasCatalogModel
	require.NoError(t, model.DB.Where("remote_id = ?", "draft-sora-model").First(&entry).Error)
	assert.Equal(t, "draft-sora-model", entry.DisplayName)
	assert.Equal(t, "video_gen", entry.Capabilities)
	assert.Equal(t, "relay_video_async_v1", entry.Contract)
	require.NotNil(t, entry.Enabled)
	assert.False(t, *entry.Enabled, "起草条目必须 enabled=false")
	assert.Greater(t, entry.CreatedTime, int64(0), "起草条目必须写 created_time")
	assert.Equal(t, entry.CreatedTime, entry.UpdatedTime)

	var meta model.Model
	require.NoError(t, model.DB.Where("model_name = ?", "draft-sora-model").First(&meta).Error)
	assert.Equal(t, 0, meta.Status, "起草 meta 行必须 status=0")
	assert.Equal(t, 1, meta.SyncOfficial)
	assert.Greater(t, meta.CreatedTime, int64(0), "起草 meta 行必须写 created_time")
	assert.Equal(t, meta.CreatedTime, meta.UpdatedTime)
}

// TestDraftCatalogIdempotent 钉住幂等:再次调用 drafted==0,条目/行不重复。
func TestDraftCatalogIdempotent(t *testing.T) {
	truncate(t)
	channel := newDraftChannel(1, constant.ChannelTypeSora)

	drafted, err := DraftCatalogEntries(channel, []string{"draft-idem-model"})
	require.NoError(t, err)
	require.Equal(t, 1, drafted)

	drafted, err = DraftCatalogEntries(channel, []string{"draft-idem-model"})
	require.NoError(t, err)
	require.Equal(t, 0, drafted, "再次调用不应重复起草")
	require.Equal(t, int64(1), countCanvasRemoteID(t, "draft-idem-model"))
	require.Equal(t, int64(1), countModelMeta(t, "draft-idem-model"))
}

// TestDraftCatalogUnknownChannelTypeLeavesCapabilityEmpty 钉住未知渠道:
// capabilities/contract 留空,条目自然留在「未配置」,由管理员手填。
func TestDraftCatalogUnknownChannelTypeLeavesCapabilityEmpty(t *testing.T) {
	truncate(t)
	drafted, err := DraftCatalogEntries(newDraftChannel(1, constant.ChannelTypeMidjourney), []string{"draft-unknown-model"})
	require.NoError(t, err)
	require.Equal(t, 1, drafted)

	var entry model.CanvasCatalogModel
	require.NoError(t, model.DB.Where("remote_id = ?", "draft-unknown-model").First(&entry).Error)
	assert.Equal(t, "", entry.Capabilities)
	assert.Equal(t, "", entry.Contract)
}

// TestDraftCatalogDoesNotTouchExistingModelMetaRow 钉住 models 行已存在:
// 不触碰(状态不被覆盖),但目录条目缺失仍应补起草。
func TestDraftCatalogDoesNotTouchExistingModelMetaRow(t *testing.T) {
	truncate(t)
	existing := &model.Model{ModelName: "draft-existing-meta", Status: 1}
	require.NoError(t, existing.Insert())

	drafted, err := DraftCatalogEntries(newDraftChannel(1, constant.ChannelTypeSora), []string{"draft-existing-meta"})
	require.NoError(t, err)
	require.Equal(t, 1, drafted, "目录条目缺失,仍应起草该模型")

	var meta model.Model
	require.NoError(t, model.DB.Where("model_name = ?", "draft-existing-meta").First(&meta).Error)
	assert.Equal(t, 1, meta.Status, "已存在 models 行的状态不得被覆盖")
	require.Equal(t, int64(1), countModelMeta(t, "draft-existing-meta"), "不得重复插入 models 行")
	require.Equal(t, int64(1), countCanvasRemoteID(t, "draft-existing-meta"), "目录条目应被创建")
}

// TestDraftCatalogSkipsSoftDeletedCatalogEntry 钉住目录条目软删除态算存在:
// 跳过不重建新条目;models 行缺失仍应补起草。
func TestDraftCatalogSkipsSoftDeletedCatalogEntry(t *testing.T) {
	truncate(t)
	entry := &model.CanvasCatalogModel{
		RemoteID:     "draft-soft-deleted",
		DisplayName:  "draft-soft-deleted",
		Capabilities: "video_gen",
		Enabled:      boolPtr(false),
		Contract:     "relay_video_async_v1",
	}
	require.NoError(t, model.DB.Create(entry).Error)
	require.NoError(t, model.DB.Delete(entry).Error)

	drafted, err := DraftCatalogEntries(newDraftChannel(1, constant.ChannelTypeSora), []string{"draft-soft-deleted"})
	require.NoError(t, err)
	require.Equal(t, 1, drafted, "models 行缺失,仍应补起草 meta 行")

	require.Equal(t, int64(1), countCanvasRemoteID(t, "draft-soft-deleted"), "软删除条目算存在,不得重建新条目")
	require.Equal(t, int64(1), countModelMeta(t, "draft-soft-deleted"), "models 行应被补上")
}

// TestDraftCatalogDisabledByMasterSwitch 钉住总开关:关闭时不写任何行。
func TestDraftCatalogDisabledByMasterSwitch(t *testing.T) {
	truncate(t)
	original := IsCatalogAutoDraftEnabled()
	SetCatalogAutoDraftEnabled(false)
	t.Cleanup(func() { SetCatalogAutoDraftEnabled(original) })

	drafted, err := DraftCatalogEntries(newDraftChannel(1, constant.ChannelTypeSora), []string{"draft-disabled-model"})
	require.NoError(t, err)
	require.Equal(t, 0, drafted)
	require.Equal(t, int64(0), countCanvasRemoteID(t, "draft-disabled-model"))
	require.Equal(t, int64(0), countModelMeta(t, "draft-disabled-model"))
}

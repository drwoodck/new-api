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

	result, version, err := GetCanvasCatalog(nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), version) // total count including disabled
	assert.Len(t, result, 2) // only enabled
	assert.Equal(t, "m3", result[0].RemoteID) // sorted by SortOrder
	assert.Equal(t, "m1", result[1].RemoteID)
}

package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&model.Token{})
	require.NoError(t, err)
	model.DB = db
	return db
}

func countCanvasTokens(t *testing.T, userId int) int {
	var count int64
	err := model.DB.Model(&model.Token{}).
		Where("user_id = ? AND name LIKE '画布token-%'", userId).
		Count(&count).Error
	require.NoError(t, err)
	return int(count)
}

func TestEnsureCanvasTokens_CreatesAllTenForNewUser(t *testing.T) {
	db := setupTestDB(t)
	defer func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	userId := 1
	err := EnsureCanvasTokens(userId)
	assert.NoError(t, err)
	assert.Equal(t, 10, countCanvasTokens(t, userId))

	// 验证命名
	var tokens []model.Token
	err = model.DB.Where("user_id = ?", userId).Order("id ASC").Find(&tokens).Error
	require.NoError(t, err)
	require.Equal(t, 10, len(tokens))
	assert.Equal(t, "画布token-1", tokens[0].Name)
	assert.Equal(t, "画布token-10", tokens[9].Name)
}

func TestEnsureCanvasTokens_FillsGapForPartialUser(t *testing.T) {
	db := setupTestDB(t)
	defer func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	userId := 2
	// 预先创建3个token
	for i := 1; i <= 3; i++ {
		token := model.Token{
			UserId:         userId,
			Name:           fmt.Sprintf("画布token-%d", i),
			Key:            fmt.Sprintf("sk-test-key-%d", i),
			CreatedTime:    1000,
			AccessedTime:   1000,
			ExpiredTime:    -1,
			UnlimitedQuota: true,
			Status:         1,
		}
		err := token.Insert()
		require.NoError(t, err)
	}

	err := EnsureCanvasTokens(userId)
	assert.NoError(t, err)
	assert.Equal(t, 10, countCanvasTokens(t, userId))
}

func TestEnsureCanvasTokens_IdempotentForFullSet(t *testing.T) {
	db := setupTestDB(t)
	defer func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}()

	userId := 3
	// 预先创建全部10个
	for i := 1; i <= 10; i++ {
		token := model.Token{
			UserId:         userId,
			Name:           fmt.Sprintf("画布token-%d", i),
			Key:            fmt.Sprintf("sk-test-key-%d", i),
			CreatedTime:    1000,
			AccessedTime:   1000,
			ExpiredTime:    -1,
			UnlimitedQuota: true,
			Status:         1,
		}
		err := token.Insert()
		require.NoError(t, err)
	}

	err := EnsureCanvasTokens(userId)
	assert.NoError(t, err)
	assert.Equal(t, 10, countCanvasTokens(t, userId))
}

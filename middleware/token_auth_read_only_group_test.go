package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupTokenAuthReadOnlyGroupTest 复用 auth_test.go 里 setupDashboardAuthMiddlewareTest
// 的建库方式,额外迁移 Token 表(TokenAuthReadOnly 需要按 key 查令牌)。
//
// model.GetTokenByKey 用未导出的 commonKeyCol 拼 WHERE 子句,只有 model.InitDB()
// 内部的 initCol() 会填充它。先跑一次 model.InitDB()(照
// controller/model_list_test.go 的 initModelListColumnNames 同一套路)让包级变量
// 就位,再换成本测试自己的内存库。
func setupTokenAuthReadOnlyGroupTest(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}

	previousDB := model.DB
	previousRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB = db
	common.RedisEnabled = false

	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainType, originalLogType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

func createReadOnlyGroupTestUser(t *testing.T, username, group string) *model.User {
	t.Helper()
	user := &model.User{
		Username: username, Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: group, AuthVersion: 1,
		AffCode: "readonly-group-aff-" + username,
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func createReadOnlyGroupTestToken(t *testing.T, userId int, key, group string) *model.Token {
	t.Helper()
	token := &model.Token{
		UserId: userId, Key: key, Status: common.TokenStatusEnabled,
		Name: "readonly-group-token", Group: group,
	}
	require.NoError(t, model.DB.Create(token).Error)
	return token
}

// TestTokenAuthReadOnlySetsEffectiveGroupFromToken 锁住 DEFECT 3 的中间件改动:
// TokenAuthReadOnly 必须把有效分组写进 constant.ContextKeyUsingGroup,
// token.Group 非空时覆盖用户分组 —— 与完整 TokenAuth(middleware/auth.go:459-470)
// 同一优先级。
func TestTokenAuthReadOnlySetsEffectiveGroupFromToken(t *testing.T) {
	setupTokenAuthReadOnlyGroupTest(t)
	user := createReadOnlyGroupTestUser(t, "readonly-token-group-user", "default")
	// TokenAuthReadOnly strips "sk-" then splits the remainder on "-" and looks
	// up only parts[0] — the stored token key must not itself contain a dash.
	createReadOnlyGroupTestToken(t, user.Id, "readonlykey1", "vip")

	var capturedGroup string
	router := gin.New()
	router.Use(TokenAuthReadOnly())
	router.GET("/probe", func(c *gin.Context) {
		capturedGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/probe", nil)
	req.Header.Set("Authorization", "Bearer sk-readonlykey1")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "vip", capturedGroup, "token.Group must override the user's group")
}

// TestTokenAuthReadOnlyFallsBackToUserGroupWhenTokenGroupEmpty 令牌未设分组时
// 应回退到用户自身分组,而不是留空。
func TestTokenAuthReadOnlyFallsBackToUserGroupWhenTokenGroupEmpty(t *testing.T) {
	setupTokenAuthReadOnlyGroupTest(t)
	user := createReadOnlyGroupTestUser(t, "readonly-user-group-user", "default")
	createReadOnlyGroupTestToken(t, user.Id, "readonlykey2", "")

	var capturedGroup string
	router := gin.New()
	router.Use(TokenAuthReadOnly())
	router.GET("/probe", func(c *gin.Context) {
		capturedGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/probe", nil)
	req.Header.Set("Authorization", "Bearer sk-readonlykey2")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "default", capturedGroup, "empty token.Group must fall back to the user's group")
}

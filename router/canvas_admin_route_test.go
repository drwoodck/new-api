package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanvasAdminRoutesRegistered 验证画布目录管理端全部路由都已注册。
//
// 背景: GetCanvasContractStats 控制器曾长期存在但从未挂到路由上 —— 前端
// 画布目录页挂载即请求 /api/canvas/admin/contract-stats,生产环境 404,
// 后台页面 toast "Request failed with status code 404"。控制器单测自己
// 搭路由测不到这个缺口,只有从 SetApiRouter 的真实路由树上断言才拦得住。
func TestCanvasAdminRoutesRegistered(t *testing.T) {
	setupApiRouterTestDB(t)
	gin.SetMode(gin.TestMode)

	r := gin.New()
	SetApiRouter(r)

	// 未带管理员凭据请求 → 401(路由存在、被 AdminAuth 拦下);
	// 路由缺失时 gin 会返回 404 —— 两类响应一眼可分。
	for _, path := range []string{
		"/api/canvas/admin/models",
		"/api/canvas/admin/catalog-overview",
		"/api/canvas/admin/model-mapping-warnings",
		"/api/canvas/admin/contract-stats",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusNotFound, w.Code,
			"路由 %s 未注册(前端页面会 404),请检查 SetApiRouter 的注册", path)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"路由 %s 应被 AdminAuth 拦截返回 401", path)
	}
}

// setupApiRouterTestDB 复用 relay_router_test.go 的内存库方案,
// SetApiRouter 注册阶段需要 model.DB 可用。
func setupApiRouterTestDB(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

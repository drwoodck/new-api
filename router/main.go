package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)

	// 模型元数据管理页:独立于 SPA 的单页,由 Go 直接服务。
	// 挂在这里而不是 SetWebRouter 里 —— 后者在配置了 FRONTEND_BASE_URL 时
	// 整个分支不会执行,页面会直接 404。显式路由优先于 NoRoute 的 SPA 兜底。
	router.GET("/admin/model-metadata", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ModelMetadataPage)
	})

	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}

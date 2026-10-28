package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// SetImageTaskRouter registers the async image task query endpoint.
//
// The submit endpoint stays where it belongs (POST /v1/images/generations, next
// to the synchronous image API — the handler decides which pipeline to use).
// Polling needs its own route because the client addresses a task, not a model,
// and the distributor must skip channel selection for it (see
// middleware/distributor.go, same treatment as GET /v1/videos/{id}).
func SetImageTaskRouter(router *gin.Engine) {
	imageTaskRouter := router.Group("/v1")
	imageTaskRouter.Use(middleware.RouteTag("relay"))
	imageTaskRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		imageTaskRouter.GET("/images/:task_id", controller.RelayTaskFetch)
	}
}

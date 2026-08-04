package router

import (
	"github.com/01121531/HUICHUAN-AI/controller"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/gin-gonic/gin"
)

func registerProxyRoutes(apiRouter *gin.RouterGroup) {
	proxyRoute := apiRouter.Group("/proxy")
	proxyRoute.Use(middleware.RootAuth())
	{
		proxyRoute.GET("/groups", controller.ListProxyGroups)
		proxyRoute.POST("/groups", controller.CreateProxyGroup)
		proxyRoute.PUT("/groups/:id", controller.UpdateProxyGroup)
		proxyRoute.DELETE("/groups/:id", controller.DeleteProxyGroup)
		proxyRoute.GET("/groups/:id/proxies", controller.ListGroupProxies)
		proxyRoute.POST("/proxies", controller.CreateProxy)
		proxyRoute.PUT("/proxies/:id", controller.UpdateProxy)
		proxyRoute.DELETE("/proxies/:id", controller.DeleteProxy)
		proxyRoute.GET("/bindings", controller.ListProxyBindings)
		proxyRoute.PUT("/bindings/:channel_id", controller.UpsertProxyBinding)
		proxyRoute.DELETE("/bindings/:channel_id", controller.DeleteProxyBinding)
		proxyRoute.GET("/analyses", controller.ListProxyLogAnalyses)
		proxyRoute.GET("/events", controller.ListProxyStateEvents)
	}
}

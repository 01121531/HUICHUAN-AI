package router

import (
	"github.com/01121531/HUICHUAN-AI/controller"
	"github.com/01121531/HUICHUAN-AI/middleware"
	"github.com/gin-gonic/gin"
)

func registerProxyRoutes(apiRouter *gin.RouterGroup) {
	proxyOptionRoute := apiRouter.Group("/proxy-pool")
	proxyOptionRoute.Use(middleware.AdminAuth())
	{
		proxyOptionRoute.GET("/options", controller.ListProxyPoolOptions)
	}

	proxyRoute := apiRouter.Group("/proxy")
	proxyRoute.Use(middleware.RootAuth())
	{
		proxyRoute.GET("/groups", controller.ListProxyGroups)
		proxyRoute.GET("/health-settings", controller.GetProxyHealthSettings)
		proxyRoute.PUT("/health-settings", controller.UpdateProxyHealthSettings)
		proxyRoute.POST("/groups", controller.CreateProxyGroup)
		proxyRoute.PUT("/groups/:id", controller.UpdateProxyGroup)
		proxyRoute.POST("/groups/:id/switch", controller.SwitchProxyGroupNow)
		proxyRoute.POST("/groups/:id/check", controller.CheckProxyGroupNow)
		proxyRoute.DELETE("/groups/:id", controller.DeleteProxyGroup)
		proxyRoute.GET("/groups/:id/proxies", controller.ListGroupProxies)
		proxyRoute.POST("/groups/:id/proxies/batch", controller.BatchCreateProxies)
		proxyRoute.POST("/proxies", controller.CreateProxy)
		proxyRoute.PUT("/proxies/:id", controller.UpdateProxy)
		proxyRoute.DELETE("/proxies/:id", controller.DeleteProxy)
		proxyRoute.POST("/proxies/check-all", controller.CheckAllProxiesNow)
		proxyRoute.POST("/proxies/:id/check", controller.CheckProxyNow)
		proxyRoute.POST("/proxies/:id/pause", controller.PauseProxy)
		proxyRoute.POST("/proxies/:id/resume", controller.ResumeProxy)
		proxyRoute.POST("/proxies/:id/reset-observation", controller.ResetProxyObservation)
		proxyRoute.GET("/bindings", controller.ListProxyBindings)
		proxyRoute.PUT("/bindings/:channel_id", controller.UpsertProxyBinding)
		proxyRoute.DELETE("/bindings/:channel_id", controller.DeleteProxyBinding)
		proxyRoute.GET("/analyses", controller.ListProxyLogAnalyses)
		proxyRoute.GET("/trend", controller.GetProxyTrend)
		proxyRoute.GET("/events", controller.ListProxyStateEvents)
		proxyRoute.GET("/attempts", controller.ListProxyUpstreamAttempts)
	}
}

package routes

import (
	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/openapi/controllers"
)

// RegisterAdminRoutes requires the existing JWT/DemoAccount/Casbin protected /api
// group. The controller additionally resolves current operator scope and checks
// the exact route permission; this registrar does not publish machine APIs.
func RegisterAdminRoutes(protectedAPI *gin.RouterGroup, controller *controllers.ClientAdminController) {
	group := protectedAPI.Group("/gb28181/openapi-clients")
	group.GET("", controller.Handler("list"))
	group.POST("", controller.Handler("create"))
	group.GET("/capabilities", controller.Handler("capabilities"))
	group.GET("/capabilities/catalog", controller.Handler("capabilities-catalog"))
	group.GET("/:id", controller.Handler("detail"))
	group.PUT("/:id/scopes", controller.Handler("scopes"))
	group.POST("/:id/rotate-secret", controller.Handler("rotate"))
	group.POST("/:id/enable", controller.Handler("enable"))
	group.POST("/:id/disable", controller.Handler("disable"))
	group.POST("/:id/revoke", controller.Handler("revoke"))
	group.GET("/:id/audits", controller.Handler("audits"))
	group.GET("/:id/revocation-status", controller.Handler("revocation-status"))
}

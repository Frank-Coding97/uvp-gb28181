package routes

import (
	"github.com/gin-gonic/gin"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
)

var deviceController = gbcontrollers.NewDeviceController()

// RegisterRoutes 注册 GB28181 业务路由到已带鉴权的 protected 组
// 在底座 routes.InitRoutes 的 protected 块中调用
func RegisterRoutes(protected *gin.RouterGroup) {
	gb := protected.Group("/gb28181")
	{
		device := gb.Group("/device")
		{
			device.GET("/list", deviceController.List)
			device.GET("/:deviceId", deviceController.GetByDeviceID)
		}
	}
}

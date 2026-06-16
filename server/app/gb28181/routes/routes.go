package routes

import (
	"github.com/gin-gonic/gin"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
)

var deviceController = gbcontrollers.NewDeviceController()
var hookController = gbhandler.NewHookController()

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

// RegisterHookRoutes 注册 ZLMediaKit Hook 回调端点到 engine 根(无 /api 前缀,无鉴权)
// ZLM 以 POST JSON 回调,路径 /index/hook/*
func RegisterHookRoutes(engine *gin.Engine) {
	hook := engine.Group("/index/hook")
	{
		hook.POST("/on_server_started", hookController.OnServerStarted)
		hook.POST("/on_stream_changed", hookController.OnStreamChanged)
		hook.POST("/on_stream_none_reader", hookController.OnStreamNoneReader)
		hook.POST("/on_rtp_server_timeout", hookController.OnRtpServerTimeout)
		hook.POST("/on_publish", hookController.OnPublish)
		hook.POST("/on_play", hookController.OnPlay)
	}
}

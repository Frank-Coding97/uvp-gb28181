package controllers

import (
	"errors"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

// PlayController 国标点播 REST
//   POST   /api/gb28181/play/:deviceId/:channelId   发起点播
//   DELETE /api/gb28181/play/:streamId              停播
type PlayController struct {
	controllers.Common
	svc *play.Service
}

// NewPlayController 装配点播控制器(svc 由 bootstrap 注入)
func NewPlayController(svc *play.Service) *PlayController {
	return &PlayController{svc: svc}
}

// Start 发起点播
// @router POST /api/gb28181/play/:deviceId/:channelId
func (pc *PlayController) Start(c *gin.Context) {
	if pc.svc == nil {
		pc.FailAndAbort(c, "点播服务未启用(GB28181 disabled?)", nil)
		return
	}
	deviceID := c.Param("deviceId")
	channelID := c.Param("channelId")
	if deviceID == "" || channelID == "" {
		pc.FailAndAbort(c, "deviceId/channelId 不能为空", nil)
		return
	}
	res, err := pc.svc.Start(c.Request.Context(), deviceID, channelID)
	if err != nil {
		pc.FailAndAbort(c, mapPlayErr(err), err)
		return
	}
	pc.Success(c, res)
}

// Stop 停播
// @router DELETE /api/gb28181/play/:streamId
func (pc *PlayController) Stop(c *gin.Context) {
	if pc.svc == nil {
		pc.FailAndAbort(c, "点播服务未启用", nil)
		return
	}
	streamID := c.Param("streamId")
	if streamID == "" {
		pc.FailAndAbort(c, "streamId 不能为空", nil)
		return
	}
	if err := pc.svc.Stop(c.Request.Context(), streamID); err != nil {
		pc.FailAndAbort(c, "停播失败", err)
		return
	}
	pc.SuccessWithMessage(c, "已停播")
}

// mapPlayErr 把 service 错误翻译成更友好的消息
func mapPlayErr(err error) string {
	switch {
	case errors.Is(err, play.ErrDeviceNotFound):
		return "设备不存在"
	case errors.Is(err, play.ErrDeviceOffline):
		return "设备离线,无法点播"
	case errors.Is(err, play.ErrChannelNotFound):
		return "通道不存在"
	case errors.Is(err, play.ErrStreamNotReady):
		return "流就绪超时,设备未推流"
	default:
		return "点播失败"
	}
}

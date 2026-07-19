package controllers

import (
	"fmt"
	"net"
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type ptzRequest struct {
	Action string `json:"action" binding:"required"`
	Speed  int    `json:"speed" binding:"required"`
}

// ControlPTZ sends one GB28181 DeviceControl/PTZCmd to a channel's device.
// POST /device-mgmt/channel/:id/ptz
func (dc *DeviceMgmtController) ControlPTZ(c *gin.Context) {
	if dc.ptzSender == nil {
		c.JSON(503, gin.H{"code": 503, "message": "SIP UAC 未就绪,无法下发云台控制"})
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "通道 ID 不合法", err)
		return
	}
	var request ptzRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "请求体不合法", err)
		return
	}
	action, err := manscdp.ParsePTZAction(request.Action)
	if err != nil {
		dc.FailAndAbort(c, "PTZ 动作不合法", err)
		return
	}
	if request.Speed < 1 || request.Speed > 255 {
		dc.FailAndAbort(c, "PTZ 速度需在 1-255 之间", nil)
		return
	}

	var channel gbmodels.GbChannel
	result := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&channel)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询通道失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在或无权限", nil)
		return
	}
	if channel.Status != gbmodels.ChannelStatusOnline {
		dc.FailAndAbort(c, "通道离线,无法下发云台控制", nil)
		return
	}
	if channel.PTZType != 1 && channel.PTZType != 2 && channel.PTZType != 4 {
		dc.FailAndAbort(c, "通道未上报可用云台能力", nil)
		return
	}

	var device gbmodels.GbDevice
	result = db.WithContext(c).Scopes(ownerDeptScope(c)).Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "所属设备不存在或无权限", nil)
		return
	}
	if device.Status != gbmodels.DeviceStatusOnline {
		dc.FailAndAbort(c, "设备离线,无法下发云台控制", nil)
		return
	}
	if device.IP == "" || device.Port <= 0 {
		dc.FailAndAbort(c, "设备来源地址缺失,无法下发", nil)
		return
	}

	sn := dc.nextPTZSN()
	body, err := manscdp.BuildPTZControl(channel.ChannelID, sn, manscdp.PTZCommand{Action: action, Speed: request.Speed})
	if err != nil {
		dc.FailAndAbort(c, "构造云台控制命令失败", err)
		return
	}
	dest := net.JoinHostPort(device.IP, strconv.Itoa(device.Port))
	if err := dc.ptzSender.SendMessage(c, device.DeviceID, dest, device.Transport, body); err != nil {
		dc.FailAndAbort(c, "下发云台控制失败", fmt.Errorf("%w: %v", err, dest))
		return
	}
	dc.Success(c, gin.H{
		"deviceId":  device.DeviceID,
		"channelId": channel.ChannelID,
		"action":    action,
		"speed":     request.Speed,
		"sn":        sn,
		"ok":        true,
	})
}

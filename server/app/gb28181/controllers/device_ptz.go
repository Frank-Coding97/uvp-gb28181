package controllers

import (
	"fmt"
	"net"
	"strconv"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

type ptzRequest struct {
	Action         string `json:"action" binding:"required"`
	Speed          int    `json:"speed" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type ptzExtendedRequest struct {
	Action         string `json:"action"`
	ID             int    `json:"id"`
	Speed          int    `json:"speed"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func parseExtendedAction(value string) (manscdp.PTZExtendedAction, error) {
	switch value {
	case string(manscdp.PTZActionSetPreset), string(manscdp.PTZActionCallPreset), string(manscdp.PTZActionDeletePreset),
		string(manscdp.PTZActionCruiseStart), string(manscdp.PTZActionCruiseStop), string(manscdp.PTZActionCruisePause),
		string(manscdp.PTZActionCruiseResume), string(manscdp.PTZActionCruiseDelete), string(manscdp.PTZActionAuxOn),
		string(manscdp.PTZActionAuxOff), string(manscdp.PTZActionScanStart), string(manscdp.PTZActionScanStop):
		return manscdp.PTZExtendedAction(value), nil
	default:
		return "", fmt.Errorf("不支持的 PTZ 扩展动作: %q", value)
	}
}

// ControlPTZ sends one GB28181 DeviceControl/PTZCmd to a channel's device.
// POST /device-mgmt/channel/:id/ptz
func (dc *DeviceMgmtController) ControlPTZ(c *gin.Context) {
	if dc.ptzSender == nil && dc.ptzService == nil {
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
	if dc.ptzService != nil {
		key := request.IdempotencyKey
		if key == "" {
			key = c.GetHeader("Idempotency-Key")
		}
		target := ptz.Target{
			DeviceID: uint(device.ID), DeviceCode: device.DeviceID,
			ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
			IP: device.IP, Port: device.Port, Transport: device.Transport,
			DeviceOnline:  device.Status == gbmodels.DeviceStatusOnline,
			ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline, PTZType: channel.PTZType,
		}
		op, executeErr := dc.ptzService.Execute(c, target, ptz.Command{
			CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: key,
			Payload: map[string]interface{}{"action": action, "speed": request.Speed},
			Build: func(operationSN int) ([]byte, error) {
				return manscdp.BuildPTZControl(channel.ChannelID, operationSN, manscdp.PTZCommand{Action: action, Speed: request.Speed})
			},
		})
		if executeErr != nil {
			dc.FailAndAbort(c, "下发云台控制失败", executeErr)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{
			"operationId": op.OperationID, "deviceId": device.DeviceID, "channelId": channel.ChannelID,
			"action": action, "speed": request.Speed, "sn": op.SN, "status": op.Status,
		}})
		return
	}
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

// ControlPTZExtended handles preset, cruise, scan and auxiliary commands.
func (dc *DeviceMgmtController) ControlPTZExtended(c *gin.Context) {
	if dc.ptzService == nil {
		c.JSON(503, gin.H{"code": 503, "message": "PTZ Service 未就绪"})
		return
	}
	var request ptzExtendedRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "请求体不合法", err)
		return
	}
	action, err := parseExtendedAction(request.Action)
	if err != nil {
		dc.FailAndAbort(c, "PTZ 扩展动作不合法", err)
		return
	}
	if request.Speed < 0 || request.Speed > 255 {
		dc.FailAndAbort(c, "PTZ 速度需在 0-255 之间", nil)
		return
	}
	id := request.ID
	if id == 0 {
		id, _ = strconv.Atoi(c.Param("presetId"))
	}
	if id <= 0 {
		dc.FailAndAbort(c, "PTZ 编号必须为正数", nil)
		return
	}
	var channel gbmodels.GbChannel
	var device gbmodels.GbDevice
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	channelID, parseErr := strconv.ParseUint(c.Param("id"), 10, 64)
	if parseErr != nil || channelID == 0 {
		dc.FailAndAbort(c, "通道 ID 不合法", parseErr)
		return
	}
	result := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", channelID).Limit(1).Find(&channel)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在或无权限", result.Error)
		return
	}
	result = db.WithContext(c).Scopes(ownerDeptScope(c)).Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "所属设备不存在或无权限", result.Error)
		return
	}
	target := ptz.Target{DeviceID: uint(device.ID), DeviceCode: device.DeviceID, ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline,
		ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline, PTZType: channel.PTZType}
	key := request.IdempotencyKey
	if key == "" {
		key = c.GetHeader("Idempotency-Key")
	}
	op, executeErr := dc.ptzService.Execute(c, target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: key,
		Payload: map[string]interface{}{"action": action, "id": id, "speed": request.Speed},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControl(channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: action, ID: id, Speed: request.Speed})
		},
	})
	if executeErr != nil {
		dc.FailAndAbort(c, "下发 PTZ 扩展控制失败", executeErr)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"operationId": op.OperationID, "channelId": channel.ChannelID, "action": action, "id": id, "sn": op.SN, "status": op.Status}})
}

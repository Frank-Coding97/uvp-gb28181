package controllers

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

type deviceControlRequest struct {
	Action         string                 `json:"action" binding:"required"`
	Confirmed      bool                   `json:"confirmed"`
	AlarmMethod    string                 `json:"alarmMethod"`
	AlarmType      string                 `json:"alarmType"`
	Region         manscdp.DragZoomRegion `json:"region"`
	IdempotencyKey string                 `json:"idempotencyKey"`
}

func (dc *DeviceMgmtController) GetControlCapabilities(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	dc.Success(c, manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType))
}

func (dc *DeviceMgmtController) ControlDevice(c *gin.Context) {
	if dc.ptzService == nil {
		c.JSON(503, gin.H{"code": 503, "message": "设备控制服务未就绪"})
		return
	}
	var request deviceControlRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "设备控制参数不合法", err)
		return
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	if !isAdvancedControlAction(request.Action) {
		dc.FailAndAbort(c, "设备控制动作不合法", nil)
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}

	if request.Action == "teleboot" {
		lock := dc.deviceControlLock(channel.ID)
		lock.Lock()
		defer lock.Unlock()
		if !request.Confirmed {
			dc.FailAndAbort(c, "远程重启需要显式确认", nil)
			return
		}
		if existing, found := dc.recentTeleBoot(c, channel.ID); found {
			dc.deviceControlSuccess(c, existing, request.Action, true)
			return
		}
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = c.GetHeader("Idempotency-Key")
	}
	op, err := dc.ptzService.Execute(c.Request.Context(), target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: request.Action, IdempotencyKey: key,
		Payload: map[string]interface{}{"action": request.Action},
		Build: func(sn int) ([]byte, error) {
			return buildAdvancedControl(channel.ChannelID, sn, request)
		},
	})
	if err != nil {
		dc.FailAndAbort(c, "下发设备控制失败", err)
		return
	}
	dc.deviceControlSuccess(c, op, request.Action, false)
}

func (dc *DeviceMgmtController) deviceControlLock(channelID uint) *sync.Mutex {
	value, _ := dc.deviceControlLocks.LoadOrStore(channelID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func isAdvancedControlAction(action string) bool {
	switch action {
	case "iframe", "record_start", "record_stop", "guard_set", "guard_reset", "alarm_reset", "teleboot", "drag_zoom_in", "drag_zoom_out":
		return true
	default:
		return false
	}
}

func buildAdvancedControl(channelID string, sn int, request deviceControlRequest) ([]byte, error) {
	switch request.Action {
	case "iframe":
		return manscdp.BuildIFrameControl(channelID, sn, manscdp.XMLCharsetGB2312)
	case "record_start":
		return manscdp.BuildRecordControl(channelID, sn, manscdp.RecordStart, manscdp.XMLCharsetGB2312)
	case "record_stop":
		return manscdp.BuildRecordControl(channelID, sn, manscdp.RecordStop, manscdp.XMLCharsetGB2312)
	case "guard_set":
		return manscdp.BuildGuardControl(channelID, sn, manscdp.GuardSet, manscdp.XMLCharsetGB2312)
	case "guard_reset":
		return manscdp.BuildGuardControl(channelID, sn, manscdp.GuardReset, manscdp.XMLCharsetGB2312)
	case "alarm_reset":
		return manscdp.BuildAlarmResetControl(channelID, sn, manscdp.AlarmResetOptions{AlarmMethod: request.AlarmMethod, AlarmType: request.AlarmType}, manscdp.XMLCharsetGB2312)
	case "teleboot":
		return manscdp.BuildTeleBootControl(channelID, sn, request.Confirmed, manscdp.XMLCharsetGB2312)
	case "drag_zoom_in", "drag_zoom_out":
		direction := manscdp.DragZoomIn
		if request.Action == "drag_zoom_out" {
			direction = manscdp.DragZoomOut
		}
		return manscdp.BuildDragZoomControl(channelID, sn, manscdp.DragZoomCommand{Direction: direction, Region: request.Region}, manscdp.XMLCharsetGB2312)
	default:
		return nil, fmt.Errorf("不支持的设备控制动作: %q", request.Action)
	}
}

func (dc *DeviceMgmtController) recentTeleBoot(c *gin.Context, channelID uint) (gbmodels.GbPTZOperation, bool) {
	var operation gbmodels.GbPTZOperation
	result := dc.db().WithContext(c.Request.Context()).
		Where("channel_id = ? AND action = ? AND status IN ? AND created_at >= ?", channelID, "teleboot", []gbmodels.PTZOperationStatus{
			gbmodels.PTZOperationSent, gbmodels.PTZOperationAccepted, gbmodels.PTZOperationUnknown,
		}, time.Now().Add(-time.Minute)).
		Order("id DESC").Limit(1).Find(&operation)
	return operation, result.Error == nil && result.RowsAffected > 0
}

func (dc *DeviceMgmtController) deviceControlSuccess(c *gin.Context, operation gbmodels.GbPTZOperation, action string, deduplicated bool) {
	dc.Success(c, gin.H{
		"operationId":  operation.OperationID,
		"action":       action,
		"sn":           operation.SN,
		"status":       operation.Status,
		"deduplicated": deduplicated,
	})
}

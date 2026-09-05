package controllers

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
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

type deviceControlLockKey struct {
	scope    string
	id       uint
	code     string
	resource string
}

type advancedControlResource struct {
	name    string
	label   string
	actions []string
}

func (dc *DeviceMgmtController) GetControlCapabilities(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	dc.Success(c, manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType))
}

func (dc *DeviceMgmtController) ControlDevice(c *gin.Context) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
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

	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	if request.Action == "teleboot" {
		if !request.Confirmed {
			dc.FailAndAbort(c, "远程重启需要显式确认", nil)
			return
		}
		if _, owner := dc.loadMaintenanceDeviceByID(c, target.DeviceID); !owner {
			return
		}
		if !dc.checkDeviceRebootPermission(c, target.DeviceID) {
			return
		}
		actorID, actorDeptID, err := dc.maintenanceActor(c)
		if err != nil {
			dc.FailAndAbort(c, "读取操作者部门失败", err)
			return
		}
		op, deduplicated, err := service.ExecuteDeviceRebootDetailed(c.Request.Context(), ptz.DeviceRebootTarget{
			DeviceID: target.DeviceID, DeviceCode: target.DeviceCode, IP: target.IP, Port: target.Port,
			Transport: target.Transport, DeviceOnline: target.DeviceOnline, Profile: target.Profile,
		}, key, actorID, actorDeptID)
		if err != nil {
			dc.FailAndAbort(c, "下发设备重启失败", err)
			return
		}
		dc.deviceControlSuccess(c, op, request.Action, deduplicated)
		return
	}
	profile := target.Profile
	responsePolicy := profile.ResponseFor(advancedResponseAction(request.Action))
	targetScope := gbmodels.ControlTargetScopeChannel
	targetCode := target.ChannelCode
	if request.Action == "guard_set" || request.Action == "guard_reset" || request.Action == "alarm_reset" {
		targetScope = gbmodels.ControlTargetScopeAlarm
		resolution, err := catalog.ResolveAlarmTarget(
			c.Request.Context(), dc.db(), target.DeviceID, target.DeviceCode, target.ChannelCode,
		)
		if err != nil {
			dc.FailAndAbort(c, "解析报警输入目标失败", err)
			return
		}
		switch resolution.Status {
		case catalog.AlarmTargetResolved:
			if resolution.Target == nil || strings.TrimSpace(resolution.Target.AlarmCode) == "" {
				dc.FailAndAbort(c, "当前通道未关联报警输入，无法执行报警控制", nil)
				return
			}
			targetCode = strings.TrimSpace(resolution.Target.AlarmCode)
		case catalog.AlarmTargetAmbiguous:
			dc.FailAndAbort(c, "当前通道关联多个报警输入，请先绑定唯一报警输入", nil)
			return
		case catalog.AlarmTargetUnavailable:
			// Some 2016 devices expose alarm control only on the registered
			// parent code and publish no catalog node. Keep the alarm scope
			// for operation/audit semantics, but target the parent device.
			targetCode = target.DeviceCode
		default:
			dc.FailAndAbort(c, "当前通道未关联报警输入，无法执行报警控制", nil)
			return
		}
	}
	if resource, found := advancedControlResourceFor(request.Action); found {
		lock := dc.deviceControlResourceLock(target.DeviceID, targetScope, targetCode, resource.name)
		lock.Lock()
		defer lock.Unlock()

		existing, active, err := dc.activeDeviceControlResource(c, target.DeviceID, targetScope, targetCode, resource)
		if err != nil {
			dc.FailAndAbort(c, "检查"+resource.label+"操作状态失败", err)
			return
		}
		if active {
			if key != "" && existing.Action == request.Action && existing.IdempotencyKey == key {
				dc.deviceControlSuccess(c, existing, request.Action, true)
				return
			}
			dc.FailAndAbort(c, resource.label+"操作正在处理中", nil)
			return
		}
	}
	op, err := service.Execute(c.Request.Context(), target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: request.Action, IdempotencyKey: key,
		Profile: profile, TargetScope: targetScope, TargetCode: targetCode,
		ResponseRequired: responsePolicy.ResponseRequired,
		Payload: map[string]interface{}{
			"action": request.Action, "alarmMethod": request.AlarmMethod,
			"alarmType": request.AlarmType, "confirmed": request.Confirmed,
			"region": request.Region,
		},
		Build: func(sn int) ([]byte, error) {
			return buildAdvancedControl(profile, targetCode, sn, request)
		},
	})
	if err != nil {
		dc.FailAndAbort(c, "下发设备控制失败", err)
		return
	}
	dc.deviceControlSuccess(c, op, request.Action, false)
}

func advancedResponseAction(action string) protocol.Action {
	switch action {
	case "record_start", "record_stop":
		return protocol.ActionRecord
	case "guard_set", "guard_reset":
		return protocol.ActionGuard
	case "alarm_reset":
		return protocol.ActionAlarm
	case "teleboot":
		return protocol.ActionTeleBoot
	case "drag_zoom_in", "drag_zoom_out":
		return protocol.ActionDragZoom
	case "iframe":
		return protocol.ActionIFrame
	default:
		return protocol.Action(action)
	}
}

func (dc *DeviceMgmtController) deviceControlLock(channelID uint) *sync.Mutex {
	value, _ := dc.deviceControlLocks.LoadOrStore(deviceControlLockKey{scope: gbmodels.ControlTargetScopeChannel, id: channelID}, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (dc *DeviceMgmtController) deviceControlResourceLock(deviceID uint, scope, targetCode, resource string) *sync.Mutex {
	value, _ := dc.deviceControlLocks.LoadOrStore(deviceControlLockKey{
		scope: scope, id: deviceID, code: targetCode, resource: resource,
	}, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func advancedControlResourceFor(action string) (advancedControlResource, bool) {
	switch action {
	case "record_start", "record_stop":
		return advancedControlResource{name: "record", label: "录像", actions: []string{"record_start", "record_stop"}}, true
	case "guard_set", "guard_reset":
		return advancedControlResource{name: "guard", label: "布撤防", actions: []string{"guard_set", "guard_reset"}}, true
	default:
		return advancedControlResource{}, false
	}
}

func (dc *DeviceMgmtController) activeDeviceControlResource(
	c *gin.Context,
	deviceID uint,
	targetScope string,
	targetCode string,
	resource advancedControlResource,
) (gbmodels.GbPTZOperation, bool, error) {
	var operation gbmodels.GbPTZOperation
	now := time.Now()
	result := dc.db().WithContext(c.Request.Context()).
		Where("device_id = ? AND target_scope = ? AND target_code = ?", deviceID, targetScope, targetCode).
		Where("action IN ?", resource.actions).
		Where(`status IN ? OR (status = ? AND transport_deadline_at IS NOT NULL AND transport_deadline_at > ?)`,
			[]gbmodels.PTZOperationStatus{gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent},
			gbmodels.PTZOperationUnknown, now).
		Order("id DESC").Limit(1).Find(&operation)
	return operation, result.RowsAffected > 0, result.Error
}

func isAdvancedControlAction(action string) bool {
	switch action {
	case "iframe", "record_start", "record_stop", "guard_set", "guard_reset", "alarm_reset", "teleboot", "drag_zoom_in", "drag_zoom_out":
		return true
	default:
		return false
	}
}

func buildAdvancedControl(profile protocol.Profile, targetCode string, sn int, request deviceControlRequest) ([]byte, error) {
	switch request.Action {
	case "iframe":
		return manscdp.BuildIFrameControlWithProfile(profile, targetCode, sn)
	case "record_start":
		return manscdp.BuildRecordControlWithProfile(profile, targetCode, sn, manscdp.RecordStart)
	case "record_stop":
		return manscdp.BuildRecordControlWithProfile(profile, targetCode, sn, manscdp.RecordStop)
	case "guard_set":
		return manscdp.BuildGuardControlWithProfile(profile, targetCode, sn, manscdp.GuardSet)
	case "guard_reset":
		return manscdp.BuildGuardControlWithProfile(profile, targetCode, sn, manscdp.GuardReset)
	case "alarm_reset":
		return manscdp.BuildAlarmResetControlWithProfile(profile, targetCode, sn, manscdp.AlarmResetOptions{AlarmMethod: request.AlarmMethod, AlarmType: request.AlarmType})
	case "teleboot":
		return manscdp.BuildTeleBootControlWithProfile(profile, targetCode, sn, request.Confirmed)
	case "drag_zoom_in", "drag_zoom_out":
		direction := manscdp.DragZoomIn
		if request.Action == "drag_zoom_out" {
			direction = manscdp.DragZoomOut
		}
		return manscdp.BuildDragZoomControlWithProfile(profile, targetCode, sn, manscdp.DragZoomCommand{Direction: direction, Region: request.Region})
	default:
		return nil, fmt.Errorf("不支持的设备控制动作: %q", request.Action)
	}
}

func (dc *DeviceMgmtController) deviceControlSuccess(c *gin.Context, operation gbmodels.GbPTZOperation, action string, deduplicated bool) {
	deadline := operation.DeadlineAt
	if deadline == nil {
		deadline = operation.TransportDeadlineAt
	}
	if deadline == nil {
		deadline = operation.QueueDeadlineAt
	}
	dc.Success(c, gin.H{
		"operationId":      operation.OperationID,
		"action":           action,
		"sn":               operation.SN,
		"status":           operation.Status,
		"responseRequired": operation.ResponseRequired,
		"deadlineAt":       deadline,
		"targetScope":      operation.TargetScope,
		"targetCode":       operation.TargetCode,
		"profileVersion":   operation.ProfileVersion,
		"deduplicated":     deduplicated,
	})
}

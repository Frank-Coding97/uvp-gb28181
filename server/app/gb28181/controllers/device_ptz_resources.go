package controllers

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type presetResourceRequest struct {
	PresetID       int    `json:"presetId"`
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type cruiseResourceRequest struct {
	Action         string `json:"action" binding:"required"`
	TrackID        int    `json:"trackId" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type auxiliaryResourceRequest struct {
	Action         string `json:"action" binding:"required"`
	AuxiliaryID    int    `json:"auxiliaryId" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type homePositionResourceRequest struct {
	Enabled        bool   `json:"enabled"`
	ResetTime      int    `json:"resetTime"`
	PresetID       int    `json:"presetId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func (dc *DeviceMgmtController) loadPTZTarget(c *gin.Context, channel *gbmodels.GbChannel) (ptz.Target, bool) {
	if channel == nil {
		return ptz.Target{}, false
	}
	var device gbmodels.GbDevice
	result := dc.db().WithContext(c).Scopes(ownerDeptScope(c)).Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		dc.FailAndAbort(c, "所属设备不存在或无权限", result.Error)
		return ptz.Target{}, false
	}
	return ptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID, ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline: device.Status == gbmodels.DeviceStatusOnline, ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline,
		PTZType: channel.PTZType, AllowNoPTZ: true,
	}, true
}

func (dc *DeviceMgmtController) executePTZExtendedResource(c *gin.Context, action manscdp.PTZExtendedAction, id int, name, idempotencyKey string) {
	if dc.ptzService == nil {
		c.JSON(503, gin.H{"code": 503, "message": "PTZ Service 未就绪"})
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	if id <= 0 || id > 255 {
		dc.FailAndAbort(c, "PTZ 编号必须在 1-255 之间", nil)
		return
	}
	if idempotencyKey == "" {
		idempotencyKey = c.GetHeader("Idempotency-Key")
	}
	payload := map[string]interface{}{"action": action, "id": id}
	if name != "" {
		payload["name"] = name
	}
	op, err := dc.ptzService.Execute(c, target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: idempotencyKey,
		Payload: payload,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControl(channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: action, ID: id})
		},
	})
	if err != nil {
		dc.FailAndAbort(c, "下发 PTZ 资源控制失败", err)
		return
	}
	// 预置位设/删属于国标里"设备权威"的资源变更:主流程乐观入库让 UI 立即响应后,
	// 后台异步下发一次 PresetQuery,把设备真实状态同步过来。persistQueryCache 会 UPSERT
	// gb_ptz_preset 并按 SumNum 对账——设备端实际没存住或已删除的会被自动纠正。
	if action == manscdp.PTZActionSetPreset || action == manscdp.PTZActionDeletePreset {
		dc.reconcilePresetsAsync(target)
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID, "channelId": channel.ChannelID, "action": action,
		"id": id, "sn": op.SN, "status": op.Status,
	})
}

// reconcilePresetsAsync 用独立 context 后台下发 PresetQuery,不阻塞主响应。
// 独立 idempotency_key 保证多次调用能各自建 operation 记录,不会跟主操作冲突。
func (dc *DeviceMgmtController) reconcilePresetsAsync(target ptz.Target) {
	if dc.ptzService == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := dc.ptzService.Refresh(ctx, target, ptz.QueryPreset, 0, "reconcile-"+uuid.NewString()); err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Warn("预置位对账查询下发失败",
					zap.Uint("channelId", target.ChannelID),
					zap.String("channelCode", target.ChannelCode),
					zap.Error(err))
			}
		}
	}()
}

func (dc *DeviceMgmtController) CreatePTZPreset(c *gin.Context) {
	var request presetResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.PresetID <= 0 {
		dc.FailAndAbort(c, "预置位参数不合法", err)
		return
	}
	dc.executePTZExtendedResource(c, manscdp.PTZActionSetPreset, request.PresetID, request.Name, request.IdempotencyKey)
}

func (dc *DeviceMgmtController) CallPTZPreset(c *gin.Context) {
	presetID, err := strconv.Atoi(c.Param("presetId"))
	if err != nil || presetID <= 0 {
		dc.FailAndAbort(c, "预置位编号不合法", err)
		return
	}
	dc.executePTZExtendedResource(c, manscdp.PTZActionCallPreset, presetID, "", "")
}

func (dc *DeviceMgmtController) DeletePTZPreset(c *gin.Context) {
	presetID, err := strconv.Atoi(c.Param("presetId"))
	if err != nil || presetID <= 0 {
		dc.FailAndAbort(c, "预置位编号不合法", err)
		return
	}
	dc.executePTZExtendedResource(c, manscdp.PTZActionDeletePreset, presetID, "", "")
}

func (dc *DeviceMgmtController) ControlPTZCruise(c *gin.Context) {
	var request cruiseResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "巡航参数不合法", err)
		return
	}
	var action manscdp.PTZExtendedAction
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "start":
		action = manscdp.PTZActionCruiseStart
	case "stop":
		action = manscdp.PTZActionCruiseStop
	case "pause":
		action = manscdp.PTZActionCruisePause
	case "resume", "continue":
		action = manscdp.PTZActionCruiseResume
	case "delete":
		action = manscdp.PTZActionCruiseDelete
	default:
		dc.FailAndAbort(c, "巡航动作不合法", nil)
		return
	}
	dc.executePTZExtendedResource(c, action, request.TrackID, "", request.IdempotencyKey)
}

func (dc *DeviceMgmtController) ControlPTZAux(c *gin.Context) {
	var request auxiliaryResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "辅助开关参数不合法", err)
		return
	}
	var action manscdp.PTZExtendedAction
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "on", "open", "enable":
		action = manscdp.PTZActionAuxOn
	case "off", "close", "disable":
		action = manscdp.PTZActionAuxOff
	default:
		dc.FailAndAbort(c, "辅助开关动作不合法", nil)
		return
	}
	dc.executePTZExtendedResource(c, action, request.AuxiliaryID, "", request.IdempotencyKey)
}

func (dc *DeviceMgmtController) UpdatePTZHomePosition(c *gin.Context) {
	if dc.ptzService == nil {
		c.JSON(503, gin.H{"code": 503, "message": "PTZ Service 未就绪"})
		return
	}
	var request homePositionResourceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "看守位参数不合法", err)
		return
	}
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return
	}
	key := request.IdempotencyKey
	if key == "" {
		key = c.GetHeader("Idempotency-Key")
	}
	op, err := dc.ptzService.Execute(c, target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: "home_position", IdempotencyKey: key,
		Payload: map[string]interface{}{"enabled": request.Enabled, "resetTime": request.ResetTime, "presetId": request.PresetID},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionControl(channel.ChannelID, sn, manscdp.HomePositionControl{Enabled: request.Enabled, ResetTime: request.ResetTime, PresetID: request.PresetID})
		},
	})
	if err != nil {
		dc.FailAndAbort(c, "下发看守位控制失败", err)
		return
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID, "channelId": channel.ChannelID, "action": "home_position", "sn": op.SN, "status": op.Status,
	})
}

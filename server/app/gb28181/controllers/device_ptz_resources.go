package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/dbresolver"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type presetResourceRequest struct {
	PresetID       int    `json:"presetId"`
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type cruiseResourceRequest struct {
	Action         string `json:"action" binding:"required"`
	TrackID        *int   `json:"trackId"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// cruiseTrackCreateRequest:一次 POST 建立整条巡航路径。
// 后端按序下发:0x85 清空(若 ReplaceExisting)→ N × 0x84 加站 → 0x86 速度 → 0x87 停留。
type cruiseTrackCreateRequest struct {
	TrackID         *int                   `json:"trackId"`
	Name            string                 `json:"name"`
	Speed           int                    `json:"speed"`    // 0-4095,0 表示不下发速度指令(沿用设备默认)
	DwellSec        int                    `json:"dwellSec"` // 0-4095 秒,0 表示不下发停留指令
	Stops           []cruiseTrackStopInput `json:"stops" binding:"required,min=1"`
	ReplaceExisting bool                   `json:"replaceExisting"` // 先 0x85(P2=0) 清空再加,避免与设备现有点位混叠
	IdempotencyKey  string                 `json:"idempotencyKey"`
}

type cruiseTrackStopInput struct {
	PresetID int `json:"presetId" binding:"required"`
}

type wiperControlRequest struct {
	Action         string `json:"action" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type homePositionResourceRequest struct {
	Enabled        *bool   `json:"enabled"`
	ResetTime      *int    `json:"resetTime"`
	PresetID       *int    `json:"presetId"`
	IdempotencyKey *string `json:"idempotencyKey"`
}

type homePositionHTTPFailure struct {
	status  int
	code    ptz.ErrorCode
	message string
	err     error
}

func homePositionFailure(status int, code ptz.ErrorCode, message string, err error) *homePositionHTTPFailure {
	return &homePositionHTTPFailure{status: status, code: code, message: message, err: err}
}

func decodeHomePositionResourceRequest(c *gin.Context) (homePositionResourceRequest, *homePositionHTTPFailure) {
	var request homePositionResourceRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "看守位参数不合法", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("请求体包含多个 JSON 值")
		}
		return request, homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "看守位参数不合法", err)
	}
	if request.Enabled == nil {
		return request, homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "enabled 必须显式提供", nil)
	}
	if *request.Enabled {
		if request.ResetTime == nil || *request.ResetTime < 10 || *request.ResetTime > 3600 {
			return request, homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "启用看守位时 resetTime 必须在 10-3600 之间", nil)
		}
		if request.PresetID == nil || *request.PresetID < 0 || *request.PresetID > 255 {
			return request, homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "启用看守位时 presetId 必须在 0-255 之间", nil)
		}
	}
	return request, nil
}

func homePositionIdempotencyKey(request homePositionResourceRequest, headerValue string) (string, *homePositionHTTPFailure) {
	headerValue = strings.TrimSpace(headerValue)
	bodyValue := ""
	if request.IdempotencyKey != nil {
		bodyValue = strings.TrimSpace(*request.IdempotencyKey)
	}
	if headerValue != "" && bodyValue != "" && headerValue != bodyValue {
		return "", homePositionFailure(http.StatusUnprocessableEntity, ptz.ErrorCodeHomePositionInvalidArgument, "Header 与请求体的幂等键不一致", nil)
	}
	if headerValue != "" {
		return headerValue, nil
	}
	return bodyValue, nil
}

func writeHomePositionFailure(c *gin.Context, failure *homePositionHTTPFailure) {
	if failure == nil {
		return
	}
	if failure.err != nil && app.ZapLog != nil {
		app.ZapLog.Warn(failure.message, zap.String("errorCode", string(failure.code)), zap.Error(failure.err))
	}
	data := gin.H{"errorCode": string(failure.code)}
	if app.Response != nil {
		app.Response.Fail(c, failure.message, failure.status, 1, data)
		return
	}
	c.AbortWithStatusJSON(failure.status, gin.H{"code": 1, "message": failure.message, "data": data})
}

func (dc *DeviceMgmtController) loadHomePositionChannel(c *gin.Context) (*gbmodels.GbChannel, *homePositionHTTPFailure) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return nil, homePositionFailure(http.StatusNotFound, ptz.ErrorCodeHomePositionNotFound, "通道不存在或无权限", err)
	}
	db := dc.db()
	if db == nil {
		return nil, homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "数据库未就绪", nil)
	}
	var channel gbmodels.GbChannel
	result := db.WithContext(c.Request.Context()).Clauses(dbresolver.Write).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&channel)
	if result.Error != nil {
		return nil, homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "查询通道失败", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, homePositionFailure(http.StatusNotFound, ptz.ErrorCodeHomePositionNotFound, "通道不存在或无权限", nil)
	}
	return &channel, nil
}

func (dc *DeviceMgmtController) loadHomePositionTarget(c *gin.Context, channel *gbmodels.GbChannel) (ptz.Target, *homePositionHTTPFailure) {
	var device gbmodels.GbDevice
	result := dc.db().WithContext(c.Request.Context()).Clauses(dbresolver.Write).Scopes(ownerDeptScope(c)).
		Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		return ptz.Target{}, homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "查询所属设备失败", result.Error)
	}
	if result.RowsAffected == 0 {
		return ptz.Target{}, homePositionFailure(http.StatusNotFound, ptz.ErrorCodeHomePositionNotFound, "所属设备不存在或无权限", nil)
	}
	return ptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID, ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline: device.Status == gbmodels.DeviceStatusOnline, ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline,
	}, nil
}

func (dc *DeviceMgmtController) loadHomePositionActor(c *gin.Context) (uint, uint, *homePositionHTTPFailure) {
	actorID := dc.GetCurrentUserID(c)
	if actorID == 0 {
		return 0, 0, homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "无法识别看守位操作者", nil)
	}
	var user basemodels.User
	result := dc.db().WithContext(c.Request.Context()).Clauses(dbresolver.Write).Select("id", "dept_id").Where("id = ?", actorID).Limit(1).Find(&user)
	if result.Error != nil || result.RowsAffected != 1 {
		return 0, 0, homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "读取看守位操作者部门失败", result.Error)
	}
	return actorID, user.DeptID, nil
}

func homePositionOperationFailure(err error) *homePositionHTTPFailure {
	var operationError *ptz.OperationError
	if errors.As(err, &operationError) {
		switch operationError.Code {
		case ptz.ErrorCodeHomePositionDeviceOffline:
			return homePositionFailure(http.StatusConflict, operationError.Code, operationError.Message, err)
		case ptz.ErrorCodeHomePositionIdempotencyConflict:
			return homePositionFailure(http.StatusConflict, operationError.Code, operationError.Message, err)
		case ptz.ErrorCodeHomePositionUnavailable:
			return homePositionFailure(http.StatusServiceUnavailable, operationError.Code, operationError.Message, err)
		}
	}
	return homePositionFailure(http.StatusInternalServerError, ptz.ErrorCodeHomePositionInternal, "看守位服务处理失败", err)
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
	}, true
}

func (dc *DeviceMgmtController) executePTZExtendedResource(c *gin.Context, action manscdp.PTZExtendedAction, id int, name, idempotencyKey string) {
	dc.executePTZExtendedResourceAs(c, action, string(action), id, name, idempotencyKey)
}

func (dc *DeviceMgmtController) executePTZExtendedResourceAs(c *gin.Context, protocolAction manscdp.PTZExtendedAction, operationAction string, id int, name, idempotencyKey string) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
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
	allowZero := protocolAction == manscdp.PTZActionCruiseStart || protocolAction == manscdp.PTZActionCruiseStop || protocolAction == manscdp.PTZActionCruiseDelete || protocolAction == manscdp.PTZActionCruiseDeletePath
	if id < 0 || id > 255 || (!allowZero && id == 0) {
		if allowZero {
			dc.FailAndAbort(c, "巡航组号必须在 0-255 之间", nil)
		} else {
			dc.FailAndAbort(c, "PTZ 编号必须在 1-255 之间", nil)
		}
		return
	}
	if idempotencyKey == "" {
		idempotencyKey = c.GetHeader("Idempotency-Key")
	}
	payload := map[string]interface{}{"action": operationAction, "id": id}
	if operationAction != string(protocolAction) {
		payload["protocolAction"] = protocolAction
	}
	if name != "" {
		payload["name"] = name
	}
	lock := dc.deviceControlLock(channel.ID)
	lock.Lock()
	defer lock.Unlock()
	op, err := service.Execute(c, target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: operationAction, IdempotencyKey: idempotencyKey,
		Payload: payload,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControl(channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: protocolAction, ID: id})
		},
	})
	if err != nil {
		dc.FailAndAbort(c, "下发 PTZ 资源控制失败", err)
		return
	}
	// 预置位设/删属于国标里"设备权威"的资源变更:主流程乐观入库让 UI 立即响应后,
	// 后台异步下发一次 PresetQuery,把设备真实状态同步过来。persistQueryCache 会 UPSERT
	// gb_ptz_preset 并按 SumNum 对账——设备端实际没存住或已删除的会被自动纠正。
	if protocolAction == manscdp.PTZActionSetPreset || protocolAction == manscdp.PTZActionDeletePreset {
		dc.reconcilePresetsAsync(target)
	}
	if protocolAction == manscdp.PTZActionCruiseDelete || protocolAction == manscdp.PTZActionCruiseDeletePath {
		if db := dc.db(); db != nil {
			if err := db.WithContext(c).Where("channel_id = ? AND track_id = ?", channel.ID, id).Delete(&gbmodels.GbPTZCruiseTrack{}).Error; err != nil && app.ZapLog != nil {
				app.ZapLog.Warn("删除巡航本地缓存失败",
					zap.Uint("channelId", channel.ID),
					zap.Int("trackId", id),
					zap.Error(err))
			}
		}
		dc.reconcileCruiseAsync(target, id, false)
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID, "channelId": channel.ChannelID, "action": operationAction,
		"id": id, "sn": op.SN, "status": op.Status,
	})
}

// reconcilePresetsAsync 用独立 context 后台下发 PresetQuery,不阻塞主响应。
// 独立 idempotency_key 保证多次调用能各自建 operation 记录,不会跟主操作冲突。
func (dc *DeviceMgmtController) reconcilePresetsAsync(target ptz.Target) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		return
	}
	go func(service *ptz.Service) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := service.Refresh(ctx, target, ptz.QueryPreset, 0, "reconcile-"+uuid.NewString()); err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Warn("预置位对账查询下发失败",
					zap.Uint("channelId", target.ChannelID),
					zap.String("channelCode", target.ChannelCode),
					zap.Error(err))
			}
		}
	}(service)
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
	if request.TrackID == nil {
		dc.FailAndAbort(c, "巡航组号不能为空", nil)
		return
	}
	var action manscdp.PTZExtendedAction
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "start":
		action = manscdp.PTZActionCruiseStart
	case "stop":
		action = manscdp.PTZActionCruiseStop
	case "delete":
		action = manscdp.PTZActionCruiseDeletePath
	default:
		dc.FailAndAbort(c, "巡航动作不合法", nil)
		return
	}
	dc.executePTZExtendedResource(c, action, *request.TrackID, "", request.IdempotencyKey)
}

func (dc *DeviceMgmtController) ControlPTZWiper(c *gin.Context) {
	var request wiperControlRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "雨刷控制参数不合法", err)
		return
	}
	var action manscdp.PTZExtendedAction
	var operationAction string
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "on", "open", "enable":
		action = manscdp.PTZActionAuxOn
		operationAction = "wiper_on"
	case "off", "close", "disable":
		action = manscdp.PTZActionAuxOff
		operationAction = "wiper_off"
	default:
		dc.FailAndAbort(c, "雨刷控制动作不合法", nil)
		return
	}
	dc.executePTZExtendedResourceAs(c, action, operationAction, manscdp.PTZAuxiliaryIDWiper, "", request.IdempotencyKey)
}

// CreateCruiseTrack 按顺序下发 GB/T 28181 附录 A.3 巡航配置指令建立一条巡航路径。
// 每条子指令都走 ptzService.Execute,写各自的 gb_ptz_operation 记录 —— 中间失败时
// 返回已成功的 stop 数,不做设备端回滚(0x85 删除整轨的设备行为需由后续对账确认)。
// 完成后异步 CruiseTrackListQuery 拉取真实状态回填 gb_ptz_cruise_track.detail_json。
func (dc *DeviceMgmtController) CreateCruiseTrack(c *gin.Context) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		c.JSON(503, gin.H{"code": 503, "message": "PTZ Service 未就绪"})
		return
	}
	var request cruiseTrackCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "巡航轨迹参数不合法", err)
		return
	}
	if request.TrackID == nil || *request.TrackID < 0 || *request.TrackID > 255 {
		dc.FailAndAbort(c, "巡航组号必须在 0-255 之间", nil)
		return
	}
	trackID := *request.TrackID
	if len(request.Stops) == 0 || len(request.Stops) > 32 {
		dc.FailAndAbort(c, "巡航站点数量必须在 1-32 之间", nil)
		return
	}
	for i, stop := range request.Stops {
		if stop.PresetID <= 0 || stop.PresetID > 255 {
			dc.FailAndAbort(c, "巡航站点 "+strconv.Itoa(i+1)+" 的预置位编号不合法", nil)
			return
		}
	}
	if request.Speed < 0 || request.Speed > 4095 {
		dc.FailAndAbort(c, "巡航速度必须在 0-4095 之间", nil)
		return
	}
	if request.DwellSec < 0 || request.DwellSec > 4095 {
		dc.FailAndAbort(c, "巡航停留时间必须在 0-4095 秒之间", nil)
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
	// 一条巡航由多条 DeviceControl 组成;锁住整批，避免同一通道的另一批命令插入中间。
	batchLock := dc.deviceControlLock(channel.ID)
	batchLock.Lock()
	defer batchLock.Unlock()
	baseKey := strings.TrimSpace(request.IdempotencyKey)
	if baseKey == "" {
		baseKey = c.GetHeader("Idempotency-Key")
	}
	if baseKey == "" {
		baseKey = "cruise-create-" + uuid.NewString()
	}

	dispatch := func(step string, seq int, action manscdp.PTZExtendedAction, cmd manscdp.PTZExtendedCommand) (gbmodels.GbPTZOperation, error) {
		return service.Execute(c, target, ptz.Command{
			CmdType:        manscdp.CmdDeviceControl,
			Action:         string(action),
			IdempotencyKey: baseKey + "-" + step + "-" + strconv.Itoa(seq),
			Payload:        map[string]interface{}{"action": action, "trackId": cmd.ID, "presetId": cmd.SubID, "value16": cmd.Value16, "step": step, "seq": seq},
			Build: func(sn int) ([]byte, error) {
				cmd.Action = action
				return manscdp.BuildExtendedPTZControl(channel.ChannelID, sn, cmd)
			},
		})
	}

	steps := make([]gin.H, 0, len(request.Stops)+3)
	completedStops := 0

	if request.ReplaceExisting {
		op, err := dispatch("clear", 0, manscdp.PTZActionCruiseDeletePath, manscdp.PTZExtendedCommand{ID: trackID})
		steps = append(steps, gin.H{"step": "clear", "operationId": op.OperationID, "sn": op.SN, "status": op.Status})
		if err != nil {
			dc.reconcileCruiseAsync(target, trackID, true)
			dc.respondCruiseCreate(c, channel, request, steps, completedStops, "clear_failed", err.Error(), false)
			return
		}
	}

	for i, stop := range request.Stops {
		op, err := dispatch("add_stop", i+1, manscdp.PTZActionCruiseAddStop, manscdp.PTZExtendedCommand{ID: trackID, SubID: stop.PresetID})
		steps = append(steps, gin.H{"step": "add_stop", "operationId": op.OperationID, "sn": op.SN, "status": op.Status, "presetId": stop.PresetID, "index": i + 1})
		if err != nil {
			dc.reconcileCruiseAsync(target, trackID, true)
			dc.respondCruiseCreate(c, channel, request, steps, completedStops, "add_stop_failed", err.Error(), false)
			return
		}
		completedStops = i + 1
	}

	if request.Speed > 0 {
		op, err := dispatch("set_speed", 0, manscdp.PTZActionCruiseSetSpeed, manscdp.PTZExtendedCommand{ID: trackID, Value16: request.Speed})
		steps = append(steps, gin.H{"step": "set_speed", "operationId": op.OperationID, "sn": op.SN, "status": op.Status, "speed": request.Speed})
		if err != nil {
			dc.reconcileCruiseAsync(target, trackID, true)
			dc.respondCruiseCreate(c, channel, request, steps, completedStops, "set_speed_failed", err.Error(), false)
			return
		}
	}
	if request.DwellSec > 0 {
		op, err := dispatch("set_dwell", 0, manscdp.PTZActionCruiseSetDwell, manscdp.PTZExtendedCommand{ID: trackID, Value16: request.DwellSec})
		steps = append(steps, gin.H{"step": "set_dwell", "operationId": op.OperationID, "sn": op.SN, "status": op.Status, "dwellSec": request.DwellSec})
		if err != nil {
			dc.reconcileCruiseAsync(target, trackID, true)
			dc.respondCruiseCreate(c, channel, request, steps, completedStops, "set_dwell_failed", err.Error(), false)
			return
		}
	}

	// 只有全部控制指令都成功发送后才写入待对账记录,避免中途失败留下幽灵轨迹。
	if err := dc.upsertOptimisticCruise(c, target, request); err != nil && app.ZapLog != nil {
		app.ZapLog.Warn("巡航待对账记录写入失败",
			zap.Uint("channelId", target.ChannelID),
			zap.Int("trackId", trackID),
			zap.Error(err))
	}
	// 异步对账,主流程立即返回 —— HTTP 成功只表示控制指令已发送
	dc.reconcileCruiseAsync(target, trackID, true)
	dc.respondCruiseCreate(c, channel, request, steps, completedStops, "sent", "", false)
}

// upsertOptimisticCruise 在整批字节流指令发送成功后写入待对账记录,让前端能立即看到新条目。
// 使用 (channel_id, track_id) 冲突覆盖:如果用户 replaceExisting 或者同编号重建,原记录被更新。
// updated_at 用当前时间,让 loadCruises 排序时该记录浮到前面。
func (dc *DeviceMgmtController) upsertOptimisticCruise(c *gin.Context, target ptz.Target, request cruiseTrackCreateRequest) error {
	if dc.db == nil {
		return nil
	}
	enabled := false
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "巡航 " + strconv.Itoa(*request.TrackID)
	}
	// detail_json 保留客户端提交的 stops/speed/dwell,便于对账未回前 UI 展示细节
	detail, _ := json.Marshal(map[string]interface{}{
		"trackId":  *request.TrackID,
		"name":     name,
		"stops":    request.Stops,
		"speed":    request.Speed,
		"dwellSec": request.DwellSec,
		"source":   "reconcile-pending",
	})
	track := gbmodels.GbPTZCruiseTrack{
		DeviceID: target.DeviceID, ChannelID: target.ChannelID, TrackID: *request.TrackID,
		Name: name, Enabled: &enabled, DetailJSON: string(detail),
		RawSummary: "reconcile-pending", UpdatedAt: time.Now(),
	}
	return dc.db().WithContext(c).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "track_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"device_id", "name", "enabled", "detail_json", "raw_summary", "updated_at"}),
	}).Create(&track).Error
}

// reconcileCruiseAsync 用独立 context 后台下发列表查询,创建/失败批次再查询对应轨迹详情。
// 删除只查列表;创建与部分失败同时查详情,用设备真实点位覆盖客户端提交的待对账数据。
func (dc *DeviceMgmtController) reconcileCruiseAsync(target ptz.Target, trackID int, includeDetail bool) {
	service := dc.ptzServiceSnapshot()
	if service == nil {
		return
	}
	go func(service *ptz.Service) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := service.Refresh(ctx, target, ptz.QueryCruiseTrackList, 0, "reconcile-cruise-"+uuid.NewString()); err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Warn("巡航轨迹对账查询下发失败",
					zap.Uint("channelId", target.ChannelID),
					zap.String("channelCode", target.ChannelCode),
					zap.Error(err))
			}
		}
		if !includeDetail {
			return
		}
		if _, err := service.Refresh(ctx, target, ptz.QueryCruiseTrack, trackID, "reconcile-cruise-detail-"+uuid.NewString()); err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Warn("巡航轨迹详情对账查询下发失败",
					zap.Uint("channelId", target.ChannelID),
					zap.String("channelCode", target.ChannelCode),
					zap.Int("trackId", trackID),
					zap.Error(err))
			}
		}
	}(service)
}

func (dc *DeviceMgmtController) respondCruiseCreate(c *gin.Context, channel *gbmodels.GbChannel, request cruiseTrackCreateRequest, steps []gin.H, completed int, status, errMsg string, reconciled bool) {
	body := gin.H{
		"channelId":          channel.ChannelID,
		"trackId":            *request.TrackID,
		"totalStops":         len(request.Stops),
		"completedStops":     completed,
		"status":             status,
		"reconciled":         reconciled,
		"reconcileScheduled": true,
		"steps":              steps,
	}
	if errMsg != "" {
		body["error"] = errMsg
		c.JSON(200, gin.H{"code": 4200, "message": "巡航建立部分失败", "data": body})
		return
	}
	dc.Success(c, body)
}

func (dc *DeviceMgmtController) UpdatePTZHomePosition(c *gin.Context) {
	request, failure := decodeHomePositionResourceRequest(c)
	if failure != nil {
		writeHomePositionFailure(c, failure)
		return
	}
	idempotencyKey, failure := homePositionIdempotencyKey(request, c.GetHeader("Idempotency-Key"))
	if failure != nil {
		writeHomePositionFailure(c, failure)
		return
	}
	channel, failure := dc.loadHomePositionChannel(c)
	if failure != nil {
		writeHomePositionFailure(c, failure)
		return
	}
	target, failure := dc.loadHomePositionTarget(c, channel)
	if failure != nil {
		writeHomePositionFailure(c, failure)
		return
	}
	actorID, actorDeptID, failure := dc.loadHomePositionActor(c)
	if failure != nil {
		writeHomePositionFailure(c, failure)
		return
	}
	service := dc.ptzServiceSnapshot()
	if service == nil {
		writeHomePositionFailure(c, homePositionFailure(http.StatusServiceUnavailable, ptz.ErrorCodeHomePositionUnavailable, "PTZ Service 未就绪", nil))
		return
	}

	enabled := *request.Enabled
	payload := map[string]interface{}{"enabled": enabled}
	var resetTime, presetID *int
	if enabled {
		resetTime, presetID = request.ResetTime, request.PresetID
		payload["resetTime"] = *resetTime
		payload["presetId"] = *presetID
	}
	op, err := service.Execute(c.Request.Context(), target, ptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: "home_position", IdempotencyKey: idempotencyKey,
		Payload: payload, ResponseRequired: true, MaxAttempts: 1,
		ActorID: actorID, ActorDeptID: actorDeptID,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionControl(channel.ChannelID, sn, manscdp.HomePositionControl{
				Enabled: enabled, ResetTime: resetTime, PresetIndex: presetID,
			})
		},
	})
	if err != nil {
		writeHomePositionFailure(c, homePositionOperationFailure(err))
		return
	}
	dc.Success(c, gin.H{
		"operationId": op.OperationID, "channelId": channel.ChannelID, "action": "home_position", "sn": op.SN, "status": op.Status,
	})
}

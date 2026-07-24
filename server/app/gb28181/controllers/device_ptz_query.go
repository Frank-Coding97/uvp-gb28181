package controllers

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

const ptzCacheFreshFor = time.Minute

func ptzFreshness(updatedAt time.Time, exists bool) gbmodels.PTZFreshness {
	if !exists || updatedAt.IsZero() {
		return gbmodels.PTZFreshnessUnknown
	}
	if time.Since(updatedAt) > ptzCacheFreshFor {
		return gbmodels.PTZFreshnessStale
	}
	return gbmodels.PTZFreshnessFresh
}

func (dc *DeviceMgmtController) refreshPTZ(c *gin.Context, channel *gbmodels.GbChannel, kind ptz.QueryKind, trackID int) (string, string, error) {
	if c.Query("refresh") != "true" {
		return "", "", nil
	}
	service := dc.ptzServiceSnapshot()
	if service == nil {
		return "", "", errors.New("PTZ Service 未就绪")
	}
	target, ok := dc.loadPTZTarget(c, channel)
	if !ok {
		return "", "", errors.New("PTZ 目标不可用")
	}
	op, err := service.Refresh(c, target, kind, trackID, c.GetHeader("Idempotency-Key"))
	if err != nil {
		return op.OperationID, "刷新请求未发送成功", err
	}
	return op.OperationID, "", nil
}

func (dc *DeviceMgmtController) addPTZRefresh(c *gin.Context, data gin.H, channel *gbmodels.GbChannel, kind ptz.QueryKind, trackID int) bool {
	operationID, refreshError, err := dc.refreshPTZ(c, channel, kind, trackID)
	if operationID != "" {
		data["refreshOperationId"] = operationID
	}
	if refreshError != "" {
		data["refreshError"] = refreshError
	}
	if err != nil && operationID == "" {
		dc.FailAndAbort(c, "刷新 PTZ 设备资源失败", err)
		return false
	}
	return true
}

func (dc *DeviceMgmtController) ptzChannel(c *gin.Context) (*gbmodels.GbChannel, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "通道 ID 不合法", err)
		return nil, false
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return nil, false
	}
	var channel gbmodels.GbChannel
	result := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&channel)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询通道失败", result.Error)
		return nil, false
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "通道不存在或无权限", nil)
		return nil, false
	}
	return &channel, true
}

func (dc *DeviceMgmtController) ListPTZPresets(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	var list []gbmodels.GbPTZPreset
	if err := dc.db().WithContext(c).Where("channel_id = ? AND status <> ?", channel.ID, gbmodels.PTZPresetDeleted).Order("preset_id").Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询预置位失败", err)
		return
	}
	latest := time.Time{}
	for _, item := range list {
		if item.UpdatedAt.After(latest) {
			latest = item.UpdatedAt
		}
	}
	data := gin.H{"list": list, "freshness": ptzFreshness(latest, len(list) > 0)}
	if !dc.addPTZRefresh(c, data, channel, ptz.QueryPreset, 0) {
		return
	}
	dc.Success(c, data)
}

func (dc *DeviceMgmtController) GetPTZState(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	var state gbmodels.GbPTZState
	result := dc.db().WithContext(c).Where("channel_id = ?", channel.ID).Limit(1).Find(&state)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询精准状态失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		data := gin.H{"state": nil, "freshness": gbmodels.PTZFreshnessUnknown}
		if !dc.addPTZRefresh(c, data, channel, ptz.QueryPreciseStatus, 0) {
			return
		}
		dc.Success(c, data)
		return
	}
	data := gin.H{"state": state, "freshness": ptzFreshness(state.ReceivedAt, true)}
	if !dc.addPTZRefresh(c, data, channel, ptz.QueryPreciseStatus, 0) {
		return
	}
	dc.Success(c, data)
}

func (dc *DeviceMgmtController) GetPTZHomePosition(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	var state gbmodels.GbPTZState
	result := dc.db().WithContext(c).Where("channel_id = ?", channel.ID).Limit(1).Find(&state)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询看守位失败", result.Error)
		return
	}
	data := gin.H{"homePosition": nil, "freshness": gbmodels.PTZFreshnessUnknown}
	if result.RowsAffected > 0 {
		data["homePosition"] = state
		data["freshness"] = ptzFreshness(state.ReceivedAt, true)
	}
	if !dc.addPTZRefresh(c, data, channel, ptz.QueryHomePosition, 0) {
		return
	}
	dc.Success(c, data)
}

func (dc *DeviceMgmtController) ListCruiseTracks(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	var list []gbmodels.GbPTZCruiseTrack
	if err := dc.db().WithContext(c).Where("channel_id = ?", channel.ID).Order("track_id").Find(&list).Error; err != nil {
		dc.FailAndAbort(c, "查询巡航轨迹失败", err)
		return
	}
	latest := time.Time{}
	for _, item := range list {
		if item.UpdatedAt.After(latest) {
			latest = item.UpdatedAt
		}
	}
	data := gin.H{"list": list, "freshness": ptzFreshness(latest, len(list) > 0)}
	if !dc.addPTZRefresh(c, data, channel, ptz.QueryCruiseTrackList, 0) {
		return
	}
	dc.Success(c, data)
}

func (dc *DeviceMgmtController) GetCruiseTrack(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	trackID, err := strconv.Atoi(c.Param("trackId"))
	if err != nil || trackID < 0 || trackID > 255 {
		dc.FailAndAbort(c, "巡航轨迹编号必须在 0-255 之间", err)
		return
	}
	var track gbmodels.GbPTZCruiseTrack
	result := dc.db().WithContext(c).Where("channel_id = ? AND track_id = ?", channel.ID, trackID).Limit(1).Find(&track)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询巡航轨迹失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "巡航轨迹不存在", nil)
		return
	}
	data := gin.H{"track": track, "freshness": ptzFreshness(track.UpdatedAt, true)}
	if !dc.addPTZRefresh(c, data, channel, ptz.QueryCruiseTrack, trackID) {
		return
	}
	dc.Success(c, data)
}

func (dc *DeviceMgmtController) GetPTZOperation(c *gin.Context) {
	operationID := c.Param("operationId")
	if operationID == "" {
		dc.FailAndAbort(c, "操作编号不能为空", nil)
		return
	}
	var operation gbmodels.GbPTZOperation
	result := dc.db().WithContext(c).Where("operation_id = ?", operationID).Limit(1).Find(&operation)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询 PTZ 操作失败", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "PTZ 操作不存在", nil)
		return
	}
	var channel gbmodels.GbChannel
	channelResult := dc.db().WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", operation.ChannelID).Limit(1).Find(&channel)
	if channelResult.Error != nil || channelResult.RowsAffected == 0 {
		dc.FailAndAbort(c, "PTZ 操作不存在", channelResult.Error)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": operation})
}

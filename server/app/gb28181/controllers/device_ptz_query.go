package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

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
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list}})
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
		c.JSON(200, gin.H{"code": 0, "data": nil})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": state})
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
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list}})
}

func (dc *DeviceMgmtController) GetCruiseTrack(c *gin.Context) {
	channel, ok := dc.ptzChannel(c)
	if !ok {
		return
	}
	trackID, err := strconv.Atoi(c.Param("trackId"))
	if err != nil || trackID <= 0 {
		dc.FailAndAbort(c, "巡航轨迹编号不合法", err)
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
	c.JSON(200, gin.H{"code": 0, "data": track})
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

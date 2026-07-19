package controllers

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

type RecordingController struct {
	controllers.Common
	query *recording.QueryService
}

func NewRecordingController(query *recording.QueryService) *RecordingController {
	return &RecordingController{query: query}
}

type recordQueryRequest struct {
	DeviceID   string `json:"deviceId" binding:"required"`
	ChannelID  string `json:"channelId" binding:"required"`
	StartTime  string `json:"startTime" binding:"required"`
	EndTime    string `json:"endTime" binding:"required"`
	Type       string `json:"type"`
	Secrecy    int    `json:"secrecy"`
	RecorderID string `json:"recorderId"`
}

// Query POST /api/gb28181/record/query
func (rc *RecordingController) Query(c *gin.Context) {
	if rc.query == nil {
		rc.FailAndAbort(c, "录像查询服务未启用", nil)
		return
	}
	var request recordQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		rc.FailAndAbort(c, "请求体不合法", err)
		return
	}
	input, err := request.toQueryRequest()
	if err != nil {
		rc.FailAndAbort(c, "录像查询参数不合法", err)
		return
	}
	if !rc.channelVisible(c, input.DeviceID, input.ChannelID) {
		return
	}
	result, err := rc.query.Query(c, input)
	if err != nil {
		rc.FailAndAbort(c, mapRecordQueryError(err), err)
		return
	}
	rc.Success(c, gin.H{
		"list":       result.Records,
		"total":      result.Total,
		"incomplete": result.Incomplete,
	})
}

func (r recordQueryRequest) toQueryRequest() (recording.QueryRequest, error) {
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(r.StartTime))
	if err != nil {
		return recording.QueryRequest{}, err
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(r.EndTime))
	if err != nil {
		return recording.QueryRequest{}, err
	}
	return recording.QueryRequest{
		DeviceID: strings.TrimSpace(r.DeviceID), ChannelID: strings.TrimSpace(r.ChannelID),
		StartTime: start, EndTime: end, Type: r.Type, Secrecy: r.Secrecy, RecorderID: r.RecorderID,
	}, nil
}

func (rc *RecordingController) channelVisible(c *gin.Context, deviceID, channelID string) bool {
	var channel gbmodels.GbChannel
	result := app.DB().WithContext(c).
		Scopes(datascope.OwnerDeptScope(c, "owner_dept_id")).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Limit(1).Find(&channel)
	if result.Error != nil {
		rc.FailAndAbort(c, "查询通道失败", result.Error)
		return false
	}
	if result.RowsAffected == 0 {
		rc.FailAndAbort(c, "通道不存在或无权限", nil)
		return false
	}
	return true
}

func mapRecordQueryError(err error) string {
	switch {
	case errors.Is(err, recording.ErrRecordDeviceNotFound):
		return "设备不存在"
	case errors.Is(err, recording.ErrRecordDeviceOffline):
		return "设备离线,无法查询录像"
	case errors.Is(err, recording.ErrRecordChannelNotFound):
		return "通道不存在"
	case errors.Is(err, recording.ErrRecordQueryRange):
		return "录像查询时间范围非法或超过最大跨度"
	default:
		return "录像查询失败"
	}
}

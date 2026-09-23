package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

const recordQueryWallClockLayout = "2006-01-02T15:04:05"

const (
	recordQueryTargetNotFound = "record_query_target_not_found"
	recordQueryDeviceOffline  = "record_query_device_offline"
	recordQueryInvalid        = "record_query_invalid_argument"
	recordQueryBusy           = "record_query_busy"
	recordQuerySendFailed     = "record_query_send_failed"
	recordQueryUnavailable    = "record_query_unavailable"
	recordQueryTimeout        = "record_query_timeout"
)

type RecordQueryService interface {
	Query(context.Context, recordquery.QueryRequest) (recordquery.QueryResult, error)
}

type recordQueryRuntime struct {
	service RecordQueryService
	config  gbconfig.RecordQueryConfig
	metrics *recordquery.Metrics
}

type recordQueryTarget struct {
	channel gbmodels.GbChannel
	device  gbmodels.GbDevice
}

type recordQueryBody struct {
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime"`
	Type       string `json:"type"`
	Secrecy    int    `json:"secrecy"`
	RecorderID string `json:"recorderId"`
}

type recordQueryItemView struct {
	RecordKey      string  `json:"recordKey"`
	DeviceID       string  `json:"deviceId"`
	Name           *string `json:"name"`
	FilePath       *string `json:"filePath"`
	Address        *string `json:"address"`
	StartTime      string  `json:"startTime"`
	EndTime        string  `json:"endTime"`
	RawStartTime   string  `json:"rawStartTime"`
	RawEndTime     string  `json:"rawEndTime"`
	Secrecy        int     `json:"secrecy"`
	Type           *string `json:"type"`
	RecorderID     *string `json:"recorderId"`
	FileSize       *int64  `json:"fileSize"`
	RecordLocation *string `json:"recordLocation"`
	StreamNumber   *int    `json:"streamNumber"`
}

func (dc *DeviceMgmtController) SetRecordQueryRuntime(service RecordQueryService, cfg gbconfig.RecordQueryConfig, metrics *recordquery.Metrics) {
	dc.recordQueryMu.Lock()
	dc.recordQueryService = service
	dc.recordQueryConfig = cfg
	dc.recordQueryMetrics = metrics
	dc.recordQueryMu.Unlock()
}

func (dc *DeviceMgmtController) recordQueryRuntime() recordQueryRuntime {
	dc.recordQueryMu.RLock()
	defer dc.recordQueryMu.RUnlock()
	return recordQueryRuntime{service: dc.recordQueryService, config: dc.recordQueryConfig, metrics: dc.recordQueryMetrics}
}

func (dc *DeviceMgmtController) GetRecordQueryOptions(c *gin.Context) {
	runtime := dc.recordQueryRuntime()
	if runtime.service == nil || runtime.config.Location == nil {
		writeRecordQueryFailure(c, http.StatusServiceUnavailable, recordQueryUnavailable, "设备录像查询服务未装配")
		return
	}
	target, ok := dc.loadRecordQueryTarget(c)
	if !ok {
		return
	}
	dc.Success(c, gin.H{
		"device": gin.H{
			"id": target.device.ID, "code": target.device.DeviceID, "name": displayName(target.device.Alias, target.device.Name),
			"online": target.device.Status == gbmodels.DeviceStatusOnline && target.channel.Status == gbmodels.ChannelStatusOnline,
		},
		"channel": gin.H{
			"id": target.channel.ID, "code": target.channel.ChannelID, "name": displayName(target.channel.Alias, target.channel.Name),
		},
		"timezone":       runtime.config.Timezone,
		"serverNow":      time.Now().In(runtime.config.Location).Format(time.RFC3339),
		"maxRangeHours":  runtime.config.MaxRangeHours,
		"timeoutSeconds": runtime.config.TimeoutSec,
		"supportedTypes": []string{"all", "manual", "alarm"},
	})
}

func (dc *DeviceMgmtController) QueryDeviceRecords(c *gin.Context) {
	runtime := dc.recordQueryRuntime()
	channelID, idOK := parseRecordQueryChannelID(c.Param("id"))
	audit := map[string]any{"action": "record_query", "channelId": channelID, "result": "invalid_argument"}
	if claims := dc.GetClaims(c); claims != nil {
		audit["userId"] = claims.UserID
	}
	middleware.MarkSensitiveOperation(c, audit)
	if !idOK {
		audit["result"] = "not_found"
		writeRecordQueryFailure(c, http.StatusNotFound, recordQueryTargetNotFound, "通道不存在或无权限")
		return
	}
	if runtime.service == nil || runtime.config.Location == nil {
		audit["result"] = "unavailable"
		writeRecordQueryFailure(c, http.StatusServiceUnavailable, recordQueryUnavailable, "设备录像查询服务未装配")
		return
	}
	target, ok := dc.loadRecordQueryTargetByID(c, channelID)
	if !ok {
		audit["result"] = "not_found"
		return
	}
	if target.device.Status != gbmodels.DeviceStatusOnline || target.channel.Status != gbmodels.ChannelStatusOnline {
		audit["result"] = "device_offline"
		writeRecordQueryFailure(c, http.StatusConflict, recordQueryDeviceOffline, "设备或通道当前离线")
		return
	}
	body, startTime, endTime, err := decodeRecordQueryBody(c, runtime.config)
	if err != nil {
		writeRecordQueryFailure(c, http.StatusUnprocessableEntity, recordQueryInvalid, "录像查询参数不合法")
		return
	}
	claims := dc.GetClaims(c)
	if claims == nil || claims.UserID == 0 {
		audit["result"] = "not_found"
		writeRecordQueryFailure(c, http.StatusNotFound, recordQueryTargetNotFound, "通道不存在或无权限")
		return
	}
	audit["startTime"] = body.StartTime
	audit["endTime"] = body.EndTime
	audit["type"] = body.Type

	destination := net.JoinHostPort(strings.TrimSpace(target.device.IP), strconv.Itoa(target.device.Port))
	request := recordquery.QueryRequest{
		OwnerUserID: claims.UserID, ChannelID: target.channel.ID,
		DeviceCode: target.device.DeviceID, ChannelCode: target.channel.ChannelID,
		Destination: destination, Transport: strings.ToUpper(strings.TrimSpace(target.device.Transport)),
		StartTime: startTime, EndTime: endTime, Type: manscdp.RecordInfoQueryType(body.Type),
		Secrecy: body.Secrecy, RecorderID: body.RecorderID,
	}
	startedAt := time.Now()
	finishMetrics := runtime.metrics.Begin()
	result, queryErr := runtime.service.Query(c.Request.Context(), request)
	finishMetrics(result.Status, result.PartialReason, queryErr, len(result.Records), time.Since(startedAt))
	if queryErr != nil {
		if errors.Is(queryErr, context.Canceled) && c.Request.Context().Err() != nil {
			audit["result"] = "canceled"
			return
		}
		status, code, message := mapRecordQueryError(queryErr)
		audit["result"] = strings.TrimPrefix(code, "record_query_")
		writeRecordQueryFailure(c, status, code, message)
		return
	}
	audit["result"] = string(result.Status)
	dc.Success(c, recordQueryResultView(result, runtime.config))
}

func (dc *DeviceMgmtController) loadRecordQueryTarget(c *gin.Context) (*recordQueryTarget, bool) {
	id, ok := parseRecordQueryChannelID(c.Param("id"))
	if !ok {
		writeRecordQueryFailure(c, http.StatusNotFound, recordQueryTargetNotFound, "通道不存在或无权限")
		return nil, false
	}
	return dc.loadRecordQueryTargetByID(c, id)
}

func (dc *DeviceMgmtController) loadRecordQueryTargetByID(c *gin.Context, id uint) (*recordQueryTarget, bool) {
	db := dc.db()
	if db == nil {
		writeRecordQueryFailure(c, http.StatusServiceUnavailable, recordQueryUnavailable, "数据库未就绪")
		return nil, false
	}
	var target recordQueryTarget
	result := db.WithContext(c.Request.Context()).Scopes(visibleScope(c)).Where("id = ?", id).Limit(1).Find(&target.channel)
	if result.Error != nil {
		writeRecordQueryFailure(c, http.StatusServiceUnavailable, recordQueryUnavailable, "查询通道失败")
		return nil, false
	}
	if result.RowsAffected == 0 {
		writeRecordQueryFailure(c, http.StatusNotFound, recordQueryTargetNotFound, "通道不存在或无权限")
		return nil, false
	}
	result = db.WithContext(c.Request.Context()).Scopes(visibleScope(c)).Where("device_id = ?", target.channel.DeviceID).Limit(1).Find(&target.device)
	if result.Error != nil {
		writeRecordQueryFailure(c, http.StatusServiceUnavailable, recordQueryUnavailable, "查询所属设备失败")
		return nil, false
	}
	if result.RowsAffected == 0 {
		writeRecordQueryFailure(c, http.StatusNotFound, recordQueryTargetNotFound, "通道不存在或无权限")
		return nil, false
	}
	return &target, true
}

func parseRecordQueryChannelID(value string) (uint, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return uint(id), err == nil && id > 0
}

func decodeRecordQueryBody(c *gin.Context, cfg gbconfig.RecordQueryConfig) (recordQueryBody, time.Time, time.Time, error) {
	var body recordQueryBody
	if cfg.Location == nil || cfg.MaxRangeHours <= 0 {
		return body, time.Time{}, time.Time{}, errors.New("record query config unavailable")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return body, time.Time{}, time.Time{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return body, time.Time{}, time.Time{}, errors.New("request body must contain one JSON object")
	}
	body.StartTime = strings.TrimSpace(body.StartTime)
	body.EndTime = strings.TrimSpace(body.EndTime)
	body.Type = strings.ToLower(strings.TrimSpace(body.Type))
	body.RecorderID = strings.TrimSpace(body.RecorderID)
	startTime, startErr := time.ParseInLocation(recordQueryWallClockLayout, body.StartTime, cfg.Location)
	endTime, endErr := time.ParseInLocation(recordQueryWallClockLayout, body.EndTime, cfg.Location)
	if startErr != nil || endErr != nil || !endTime.After(startTime) || endTime.Sub(startTime) > time.Duration(cfg.MaxRangeHours)*time.Hour {
		return body, time.Time{}, time.Time{}, errors.New("invalid record query time range")
	}
	if body.Type != "all" && body.Type != "manual" && body.Type != "alarm" {
		return body, time.Time{}, time.Time{}, errors.New("invalid record query type")
	}
	if body.Secrecy != 0 && body.Secrecy != 1 {
		return body, time.Time{}, time.Time{}, errors.New("invalid secrecy")
	}
	if len(body.RecorderID) > 128 {
		return body, time.Time{}, time.Time{}, errors.New("recorder id too long")
	}
	return body, startTime, endTime, nil
}

func recordQueryResultView(result recordquery.QueryResult, cfg gbconfig.RecordQueryConfig) gin.H {
	items := make([]recordQueryItemView, 0, len(result.Records))
	for _, item := range result.Records {
		start, startErr := recordquery.ParseWallClock(item.StartTime, cfg.Location)
		end, endErr := recordquery.ParseWallClock(item.EndTime, cfg.Location)
		if startErr != nil || endErr != nil || !end.After(start) {
			continue
		}
		typeValue := string(item.Type)
		items = append(items, recordQueryItemView{
			RecordKey: item.RecordKey, DeviceID: item.DeviceID,
			Name: optionalString(item.Name), FilePath: optionalString(item.FilePath), Address: optionalString(item.Address),
			StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339),
			RawStartTime: item.StartTime, RawEndTime: item.EndTime, Secrecy: item.Secrecy,
			Type: optionalString(typeValue), RecorderID: optionalString(item.RecorderID), FileSize: item.FileSize,
			RecordLocation: optionalString(item.RecordLocation), StreamNumber: item.StreamNumber,
		})
	}
	partialReason := any(nil)
	if result.Status == recordquery.QueryStatusPartial {
		partialReason = "deadline"
		if result.PartialReason == recordquery.ErrorCodeCapacity {
			partialReason = "capacity"
		}
	}
	elapsed := result.FinishedAt.Sub(result.StartedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return gin.H{
		"queryId": result.QueryID, "status": result.Status, "partialReason": partialReason,
		"declaredTotal": result.DeclaredTotal, "receivedCount": len(items), "incomplete": result.Incomplete,
		"timezone": cfg.Timezone, "elapsedMs": elapsed, "list": items,
	}
}

func mapRecordQueryError(err error) (int, string, string) {
	var queryErr *recordquery.QueryError
	if errors.As(err, &queryErr) {
		switch queryErr.Code {
		case recordquery.ErrorCodeInvalidArgument:
			return http.StatusUnprocessableEntity, recordQueryInvalid, "录像查询参数不合法"
		case recordquery.ErrorCodeBusy, recordquery.ErrorCodeCapacity:
			return http.StatusTooManyRequests, recordQueryBusy, "设备录像查询繁忙"
		case recordquery.ErrorCodeSendFailed:
			return http.StatusBadGateway, recordQuerySendFailed, "录像查询指令发送失败"
		case recordquery.ErrorCodeTimeout:
			return http.StatusGatewayTimeout, recordQueryTimeout, "设备在查询时限内未返回录像目录"
		case recordquery.ErrorCodeUnavailable:
			return http.StatusServiceUnavailable, recordQueryUnavailable, "设备录像查询服务不可用"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, recordQueryTimeout, "设备在查询时限内未返回录像目录"
	}
	return http.StatusServiceUnavailable, recordQueryUnavailable, "设备录像查询服务不可用"
}

func writeRecordQueryFailure(c *gin.Context, status int, code, message string) {
	app.Response.Fail(c, message, status, 1, gin.H{"errorCode": code})
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func displayName(alias, name string) string {
	if alias = strings.TrimSpace(alias); alias != "" {
		return alias
	}
	return strings.TrimSpace(name)
}

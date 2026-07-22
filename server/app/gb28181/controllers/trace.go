package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

var sipMethodPattern = regexp.MustCompile(`^[A-Z][A-Z0-9-]{0,31}$`)

type TraceQueryService interface {
	Health() gbtrace.HealthSnapshot
	ListMessages(context.Context, gbtrace.MessageFilter) (gbtrace.MessagePage, error)
	GetMessage(context.Context, string, bool, gbtrace.DisclosureContext) (gbtrace.MessageDetail, error)
	ListSessions(context.Context, gbtrace.SessionFilter) ([]gbtrace.SessionSummary, error)
	GetSessionStats(context.Context, gbtrace.SessionFilter) (gbtrace.SessionStats, error)
}

// TraceStreamProvider 独立于 query,直接暴露 StreamHub 给 SSE handler。
// 由 bootstrap 用 module.StreamHub() 注入。
type TraceStreamProvider interface {
	Hub() *gbtrace.StreamHub
}

type TraceCaptureService interface {
	Start(context.Context, uint, uint) (gbtrace.CaptureStartResult, error)
	Stop(context.Context, string, uint) (gbtrace.Capture, error)
	Active(context.Context, uint, uint) (*gbtrace.Capture, error)
}

type TraceAdminAccess interface {
	IsSystemAdmin(context.Context, uint) bool
}

type GormTraceAdminAccess struct{ db *gorm.DB }

func NewGormTraceAdminAccess(db *gorm.DB) *GormTraceAdminAccess { return &GormTraceAdminAccess{db: db} }

func (a *GormTraceAdminAccess) IsSystemAdmin(ctx context.Context, userID uint) bool {
	if a == nil || a.db == nil || userID == 0 {
		return false
	}
	var count int64
	err := a.db.WithContext(ctx).Table("sys_user_role AS ur").
		Joins("JOIN sys_role AS r ON r.id = ur.role_id AND r.deleted_at IS NULL").
		Where("ur.user_id = ? AND r.name = ? AND r.status = ?", userID, "系统管理员", 1).
		Count(&count).Error
	return err == nil && count > 0
}

func (a *GormTraceAdminAccess) CanManageCapture(ctx context.Context, userID uint, _ *gbmodels.GbDevice) bool {
	return a.IsSystemAdmin(ctx, userID)
}

type TraceController struct {
	controllers.Common
	query   TraceQueryService
	capture TraceCaptureService
	access  TraceAdminAccess
	stream  TraceStreamProvider
}

func NewTraceController(query TraceQueryService, capture TraceCaptureService, access TraceAdminAccess) *TraceController {
	return &TraceController{query: query, capture: capture, access: access}
}

// SetStreamProvider bootstrap 装配 module 后注入 hub 给 SSE handler
func (tc *TraceController) SetStreamProvider(provider TraceStreamProvider) {
	tc.stream = provider
}

func (tc *TraceController) authorize(c *gin.Context) (uint, bool) {
	claims := tc.GetClaims(c)
	if claims == nil || tc.access == nil || !tc.access.IsSystemAdmin(c.Request.Context(), claims.UserID) {
		app.Response.Fail(c, "仅系统管理员可访问 SIP 日志", http.StatusForbidden)
		return 0, false
	}
	return claims.UserID, true
}

func (tc *TraceController) Health(c *gin.Context) {
	if _, ok := tc.authorize(c); !ok {
		return
	}
	if tc.query == nil {
		tc.Success(c, gbtrace.DisabledHealth())
		return
	}
	tc.Success(c, tc.query.Health())
}

func (tc *TraceController) ListMessages(c *gin.Context) {
	if _, ok := tc.authorize(c); !ok {
		return
	}
	filter, err := parseMessageFilter(c)
	if err != nil {
		app.Response.Fail(c, err.Error(), http.StatusBadRequest)
		return
	}
	if tc.query == nil {
		tc.Success(c, gbtrace.MessagePage{Items: []gbtrace.MessageSummary{}})
		return
	}
	page, err := tc.query.ListMessages(c.Request.Context(), filter)
	if err != nil {
		tc.queryFailure(c, err)
		return
	}
	if page.Items == nil {
		page.Items = []gbtrace.MessageSummary{}
	}
	tc.Success(c, page)
}

func (tc *TraceController) GetMessage(c *gin.Context) {
	userID, ok := tc.authorize(c)
	if !ok {
		return
	}
	eventID := c.Param("id")
	if _, err := uuid.Parse(eventID); err != nil {
		app.Response.Fail(c, "报文 ID 非法", http.StatusBadRequest)
		return
	}
	sensitive, err := parseOptionalBool(c.Query("sensitive"))
	if err != nil {
		app.Response.Fail(c, "sensitive 参数非法", http.StatusBadRequest)
		return
	}
	purpose := strings.TrimSpace(c.Query("purpose"))
	if sensitive && !validText(purpose, 200, true) {
		app.Response.Fail(c, "敏感报文查看必须填写 purpose", http.StatusBadRequest)
		return
	}
	if tc.query == nil {
		app.Response.Fail(c, "SIP Trace 功能未启用", http.StatusNotFound)
		return
	}
	if sensitive {
		middleware.MarkSensitiveOperation(c, map[string]any{
			"messageId": eventID, "purpose": purpose, "sensitive": true,
		})
	}
	// authorize() 已确认调用者是系统管理员,IsAdmin 恒 true。
	// sensitive 走独立参数,决定是否要脱敏,不再耦合到 IsAdmin。
	detail, err := tc.query.GetMessage(c.Request.Context(), eventID, sensitive, gbtrace.DisclosureContext{
		IsAdmin: true, UserID: strconv.FormatUint(uint64(userID), 10), Purpose: purpose,
	})
	if err != nil {
		if errors.Is(err, gbtrace.ErrTraceMessageNotFound) {
			app.Response.Fail(c, "SIP 报文不存在或已过期", http.StatusNotFound)
			return
		}
		tc.queryFailure(c, err)
		return
	}
	tc.Success(c, detail)
}

// Stream GET /api/gb28181/sip-traces/stream  (SSE)
// 每 15s ping 保活;支持 URL 参数 deviceId/callId/method 过滤;
// sensitive=true 时前置 MarkSensitiveOperation 审计打点,响应体走明文。
func (tc *TraceController) Stream(c *gin.Context) {
	userID, ok := tc.authorize(c)
	if !ok {
		return
	}
	if tc.stream == nil || tc.stream.Hub() == nil {
		app.Response.Fail(c, "SIP 实时日志未启用", http.StatusServiceUnavailable)
		return
	}
	sensitive, err := parseOptionalBool(c.Query("sensitive"))
	if err != nil {
		app.Response.Fail(c, "sensitive 参数非法", http.StatusBadRequest)
		return
	}
	purpose := strings.TrimSpace(c.Query("purpose"))
	if sensitive && !validText(purpose, 200, true) {
		app.Response.Fail(c, "敏感报文订阅必须填写 purpose", http.StatusBadRequest)
		return
	}
	filter := gbtrace.StreamFilter{
		DeviceID: strings.TrimSpace(c.Query("deviceId")),
		CallID:   strings.TrimSpace(c.Query("callId")),
		Method:   strings.ToUpper(strings.TrimSpace(c.Query("method"))),
	}
	if !validText(filter.DeviceID, 128, false) || !validText(filter.CallID, 255, false) {
		app.Response.Fail(c, "设备编码或 Call-ID 非法", http.StatusBadRequest)
		return
	}
	if filter.Method != "" && !sipMethodPattern.MatchString(filter.Method) {
		app.Response.Fail(c, "method 参数非法", http.StatusBadRequest)
		return
	}
	if sensitive {
		middleware.MarkSensitiveOperation(c, map[string]any{
			"streamFilter": filter,
			"purpose":      purpose,
			"sensitive":    true,
			"userId":       userID,
		})
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	ctx := c.Request.Context()
	sub := tc.stream.Hub().Subscribe(ctx, filter, sensitive)
	defer tc.stream.Hub().Unsubscribe(sub.ID)

	// 立刻发一次 ready 事件,让前端知道订阅已建立
	fmt.Fprintf(c.Writer, "event: ready\ndata: {\"subscriptionId\":\"%s\"}\n\n", sub.ID)
	c.Writer.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, alive := <-sub.Events:
			if !alive {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(c.Writer, "event: message\ndata: %s\n\n", data); err != nil {
				return
			}
			c.Writer.Flush()
		case <-ping.C:
			if _, err := fmt.Fprintf(c.Writer, "event: ping\ndata: {\"t\":%d}\n\n", time.Now().Unix()); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

func (tc *TraceController) SessionStats(c *gin.Context) {
	if _, ok := tc.authorize(c); !ok {
		return
	}
	filter, err := parseSessionFilter(c)
	if err != nil {
		app.Response.Fail(c, err.Error(), http.StatusBadRequest)
		return
	}
	if tc.query == nil {
		tc.Success(c, gbtrace.SessionStats{})
		return
	}
	stats, err := tc.query.GetSessionStats(c.Request.Context(), filter)
	if err != nil {
		tc.queryFailure(c, err)
		return
	}
	tc.Success(c, stats)
}

func (tc *TraceController) ListSessions(c *gin.Context) {
	if _, ok := tc.authorize(c); !ok {
		return
	}
	filter, err := parseSessionFilter(c)
	if err != nil {
		app.Response.Fail(c, err.Error(), http.StatusBadRequest)
		return
	}
	if tc.query == nil {
		tc.Success(c, gin.H{"items": []gbtrace.SessionSummary{}})
		return
	}
	items, err := tc.query.ListSessions(c.Request.Context(), filter)
	if err != nil {
		tc.queryFailure(c, err)
		return
	}
	if items == nil {
		items = []gbtrace.SessionSummary{}
	}
	tc.Success(c, gin.H{"items": items})
}

func (tc *TraceController) ListSessionMessages(c *gin.Context) {
	if _, ok := tc.authorize(c); !ok {
		return
	}
	callID := strings.TrimSpace(c.Param("callId"))
	if !validText(callID, 255, true) {
		app.Response.Fail(c, "Call-ID 非法", http.StatusBadRequest)
		return
	}
	filter, err := parseMessageFilter(c)
	if err != nil {
		app.Response.Fail(c, err.Error(), http.StatusBadRequest)
		return
	}
	filter.CallID = callID
	if tc.query == nil {
		tc.Success(c, gbtrace.MessagePage{Items: []gbtrace.MessageSummary{}})
		return
	}
	page, err := tc.query.ListMessages(c.Request.Context(), filter)
	if err != nil {
		tc.queryFailure(c, err)
		return
	}
	if page.Items == nil {
		page.Items = []gbtrace.MessageSummary{}
	}
	tc.Success(c, page)
}

func (tc *TraceController) StartCapture(c *gin.Context) {
	userID, ok := tc.authorize(c)
	if !ok {
		return
	}
	deviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || deviceID == 0 {
		app.Response.Fail(c, "设备 ID 非法", http.StatusBadRequest)
		return
	}
	if tc.capture == nil {
		app.Response.Fail(c, "SIP Trace 功能未启用", http.StatusServiceUnavailable, 1, gbtrace.DisabledHealth())
		return
	}
	result, err := tc.capture.Start(c.Request.Context(), uint(deviceID), userID)
	if err != nil {
		tc.captureFailure(c, err)
		return
	}
	now := time.Now().UTC()
	tc.Success(c, gin.H{"capture": result.Capture, "reused": result.Reused, "status": result.Capture.StatusAt(now), "filter": result.Capture.WorkbenchFilter(now)})
}

func (tc *TraceController) ActiveCapture(c *gin.Context) {
	userID, ok := tc.authorize(c)
	if !ok {
		return
	}
	deviceID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || deviceID == 0 {
		app.Response.Fail(c, "设备 ID 非法", http.StatusBadRequest)
		return
	}
	if tc.capture == nil {
		tc.Success(c, gin.H{"status": gbtrace.CaptureStatusDisabled})
		return
	}
	capture, err := tc.capture.Active(c.Request.Context(), uint(deviceID), userID)
	if err != nil {
		tc.captureFailure(c, err)
		return
	}
	if capture == nil {
		tc.Success(c, gin.H{"status": gbtrace.CaptureStatusEnded, "capture": nil})
		return
	}
	now := time.Now().UTC()
	tc.Success(c, gin.H{"capture": capture, "status": capture.StatusAt(now), "filter": capture.WorkbenchFilter(now)})
}

func (tc *TraceController) StopCapture(c *gin.Context) {
	userID, ok := tc.authorize(c)
	if !ok {
		return
	}
	captureID := c.Param("id")
	if _, err := uuid.Parse(captureID); err != nil {
		app.Response.Fail(c, "诊断时段 ID 非法", http.StatusBadRequest)
		return
	}
	if tc.capture == nil {
		app.Response.Fail(c, "SIP Trace 功能未启用", http.StatusServiceUnavailable, 1, gbtrace.DisabledHealth())
		return
	}
	capture, err := tc.capture.Stop(c.Request.Context(), captureID, userID)
	if err != nil {
		tc.captureFailure(c, err)
		return
	}
	tc.Success(c, gin.H{"capture": capture, "status": capture.StatusAt(time.Now().UTC()), "filter": capture.WorkbenchFilter(time.Now().UTC())})
}

func (tc *TraceController) queryFailure(c *gin.Context, err error) {
	health := gbtrace.DisabledHealth()
	if tc.query != nil {
		health = tc.query.Health()
	}
	// 具体错误打到 gin 日志方便排查(nginx / 中间件不 buffer 它)
	fmt.Fprintf(gin.DefaultErrorWriter, "[sip-trace] query failure: %v (health=%s lastError=%s)\n",
		err, health.State, health.LastError)
	// 其他错误(schema/权限/查询失败等)如实回 500 + err 消息,前端能看到根因
	// unavailable 类错误也走这里但降级到 503 + 结构化提示
	msg := "SIP 日志存储查询失败"
	status := http.StatusInternalServerError
	if errors.Is(err, gbtrace.ErrTraceStoreUnavailable) || errors.Is(err, gbtrace.ErrTraceQueryUnavailable) {
		msg = "SIP 日志采集尚未启动或存储暂未就绪"
		status = http.StatusServiceUnavailable
	} else if err != nil {
		msg = fmt.Sprintf("SIP 日志存储查询失败: %s", err.Error())
	}
	app.Response.Fail(c, msg, status, 1, health)
}

func (tc *TraceController) captureFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gbtrace.ErrCaptureForbidden):
		app.Response.Fail(c, "无权管理该设备的诊断时段", http.StatusForbidden)
	case errors.Is(err, gbtrace.ErrCaptureDeviceNotFound), errors.Is(err, gbtrace.ErrCaptureNotFound):
		app.Response.Fail(c, "设备或诊断时段不存在", http.StatusNotFound)
	default:
		app.Response.Fail(c, "诊断时段操作失败", http.StatusInternalServerError)
	}
}

func parseMessageFilter(c *gin.Context) (gbtrace.MessageFilter, error) {
	from, to, err := parseTimeRange(c, gbtrace.MaxTraceQueryRange)
	if err != nil {
		return gbtrace.MessageFilter{}, err
	}
	filter := gbtrace.MessageFilter{From: from, To: to, DeviceID: strings.TrimSpace(c.Query("deviceId")), CallID: strings.TrimSpace(c.Query("callId")), Keyword: strings.TrimSpace(c.Query("keyword")), Cursor: c.Query("cursor")}
	deviceIDs := strings.TrimSpace(c.Query("deviceIds"))
	if deviceIDs != "" {
		for _, value := range strings.Split(deviceIDs, ",") {
			value = strings.TrimSpace(value)
			if !validText(value, 128, true) {
				return gbtrace.MessageFilter{}, errors.New("设备编码或 Call-ID 非法")
			}
			filter.DeviceIDs = append(filter.DeviceIDs, value)
		}
	}
	if (filter.DeviceID != "" && !validText(filter.DeviceID, 128, false)) || !validText(filter.CallID, 255, false) {
		return gbtrace.MessageFilter{}, errors.New("设备编码或 Call-ID 非法")
	}
	if !validText(filter.Keyword, 128, false) {
		return gbtrace.MessageFilter{}, errors.New("关键词非法")
	}
	if direction := c.Query("direction"); direction != "" {
		filter.Direction = gbtrace.Direction(direction)
		if filter.Direction != gbtrace.DirectionInbound && filter.Direction != gbtrace.DirectionOutbound {
			return gbtrace.MessageFilter{}, errors.New("direction 参数非法")
		}
	}
	if method := strings.TrimSpace(c.Query("method")); method != "" {
		method = strings.ToUpper(method)
		if !sipMethodPattern.MatchString(method) {
			return gbtrace.MessageFilter{}, errors.New("method 参数非法")
		}
		filter.Method = method
	}
	if value := c.Query("statusCode"); value != "" {
		status, parseErr := strconv.ParseUint(value, 10, 16)
		if parseErr != nil || uint16(status) < gbtrace.SIPStatusMin || uint16(status) > gbtrace.SIPStatusMax {
			return gbtrace.MessageFilter{}, errors.New("statusCode 参数非法")
		}
		filter.StatusCode = uint16(status)
	}
	parseStatus := func(key string) (uint16, error) {
		value := c.Query(key)
		if value == "" {
			return 0, nil
		}
		status, parseErr := strconv.ParseUint(value, 10, 16)
		if parseErr != nil || uint16(status) < gbtrace.SIPStatusMin || uint16(status) > gbtrace.SIPStatusMax {
			return 0, errors.New("status code range parameter is invalid")
		}
		return uint16(status), nil
	}
	filter.StatusMin, err = parseStatus("statusMin")
	if err != nil {
		return gbtrace.MessageFilter{}, err
	}
	filter.StatusMax, err = parseStatus("statusMax")
	if err != nil {
		return gbtrace.MessageFilter{}, err
	}
	if filter.StatusMin != 0 && filter.StatusMax != 0 && filter.StatusMin > filter.StatusMax {
		return gbtrace.MessageFilter{}, errors.New("status code range is invalid")
	}
	if value := c.Query("limit"); value != "" {
		limit, parseErr := strconv.Atoi(value)
		if parseErr != nil || limit <= 0 || limit > gbtrace.MaxMessagePageSize {
			return gbtrace.MessageFilter{}, errors.New("limit 参数非法")
		}
		filter.Limit = limit
	}
	if filter.Cursor != "" {
		if _, err := gbtrace.DecodeMessageCursor(filter.Cursor); err != nil {
			return gbtrace.MessageFilter{}, err
		}
	}
	return filter, nil
}

func parseSessionFilter(c *gin.Context) (gbtrace.SessionFilter, error) {
	from, to, err := parseTimeRange(c, gbtrace.MaxSessionQueryRange)
	if err != nil {
		return gbtrace.SessionFilter{}, err
	}
	filter := gbtrace.SessionFilter{From: from, To: to, DeviceID: strings.TrimSpace(c.Query("deviceId")), CallID: strings.TrimSpace(c.Query("callId")), Keyword: strings.TrimSpace(c.Query("keyword"))}
	if deviceIDs := strings.TrimSpace(c.Query("deviceIds")); deviceIDs != "" {
		for _, value := range strings.Split(deviceIDs, ",") {
			value = strings.TrimSpace(value)
			if !validText(value, 128, true) {
				return gbtrace.SessionFilter{}, errors.New("设备编码或 Call-ID 非法")
			}
			filter.DeviceIDs = append(filter.DeviceIDs, value)
		}
	}
	if !validText(filter.DeviceID, 128, false) || !validText(filter.CallID, 255, false) {
		return gbtrace.SessionFilter{}, errors.New("设备编码或 Call-ID 非法")
	}
	if !validText(filter.Keyword, 128, false) {
		return gbtrace.SessionFilter{}, errors.New("关键词非法")
	}
	filter.Anomaly, err = parseOptionalBool(c.Query("anomaly"))
	if err != nil {
		return gbtrace.SessionFilter{}, errors.New("anomaly 参数非法")
	}
	if value := c.Query("limit"); value != "" {
		limit, parseErr := strconv.Atoi(value)
		if parseErr != nil || limit <= 0 || limit > gbtrace.MaxSessionPageSize {
			return gbtrace.SessionFilter{}, errors.New("limit 参数非法")
		}
		filter.Limit = limit
	}
	return filter, nil
}

func parseTimeRange(c *gin.Context, maximum time.Duration) (time.Time, time.Time, error) {
	from, fromErr := time.Parse(time.RFC3339Nano, c.Query("from"))
	to, toErr := time.Parse(time.RFC3339Nano, c.Query("to"))
	if fromErr != nil || toErr != nil || !from.Before(to) || to.Sub(from) > maximum {
		return time.Time{}, time.Time{}, errors.New("时间范围非法")
	}
	return from.UTC(), to.UTC(), nil
}

func parseOptionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func validText(value string, max int, required bool) bool {
	if required && value == "" {
		return false
	}
	if len(value) > max {
		return false
	}
	return !strings.ContainsAny(value, "\r\n\x00")
}

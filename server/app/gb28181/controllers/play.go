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
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbdashboard "uvplatform.cn/uvp-gb28181/app/gb28181/dashboard"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

// PlayController 国标点播 REST
//
//	POST   /api/gb28181/play/:deviceId/:channelId   发起点播
//	DELETE /api/gb28181/play/:streamId              停播
type PlayController struct {
	controllers.Common
	svc               PlayService
	recordingStarter  PlaybackRecordingStarter
	attemptStore      PlayAttemptStore
	lifecycleBeginner PlayLifecycleBeginner
	feedbackSigner    ClientFeedbackSigner
	feedbackStore     ClientFeedbackStore
	lifecycleQuery    PlayLifecycleQueryStore
}

type PlayService interface {
	Start(context.Context, string, string) (*play.Result, error)
	// Stop 的 deviceID/channelID 只用于日志定位;调用方没有值时传空。
	Stop(context.Context, string, string, string) error
}

type AuthorizedPlayService interface {
	StartAuthorized(context.Context, play.AuthorizedRequest) (*play.Result, error)
}

type FixedPlaybackAuthorizationService interface {
	AuthorizeFixedPlayback(context.Context, play.AuthorizedRequest) (*play.Result, error)
}

type PlaybackRecordingStarter interface {
	BeginPlayback(context.Context, string) error
}

type PlayAttemptStore interface {
	Begin(context.Context, uint, string, string) (string, error)
	Finish(context.Context, string, string, string, int64, bool) error
}

type PlayLifecycleBeginner interface {
	Begin(context.Context, uint, string, string) (string, error)
}

type ClientFeedbackSigner interface {
	IssueClientFeedback(playauth.ClientFeedbackBinding) (playauth.ClientFeedbackGrant, error)
	VerifyClientFeedback(string, uint, string, string) (playauth.ClientFeedbackClaims, error)
}

type ClientFeedbackStore interface {
	AppendClientEvent(context.Context, string, uint, string, string, play.LifecycleEvent) error
}

type PlayLifecycleQueryStore interface {
	List(context.Context, gbdashboard.PlayLifecycleQuery, gbdashboard.QueryScope) (gbdashboard.PlayLifecyclePage, error)
	Detail(context.Context, string, gbdashboard.QueryScope) (gbdashboard.PlayLifecycleDetail, error)
}

type PlayControllerOption func(*PlayController)

func WithPlaybackRecordingStarter(starter PlaybackRecordingStarter) PlayControllerOption {
	return func(controller *PlayController) { controller.recordingStarter = starter }
}

func WithPlayAttemptStore(store PlayAttemptStore) PlayControllerOption {
	return func(controller *PlayController) { controller.attemptStore = store }
}

func WithPlayLifecycleBeginner(store PlayLifecycleBeginner) PlayControllerOption {
	return func(controller *PlayController) { controller.lifecycleBeginner = store }
}

func WithClientFeedbackRuntime(signer ClientFeedbackSigner, store ClientFeedbackStore) PlayControllerOption {
	return func(controller *PlayController) {
		controller.feedbackSigner = signer
		controller.feedbackStore = store
	}
}

func WithPlayLifecycleQueryStore(store PlayLifecycleQueryStore) PlayControllerOption {
	return func(controller *PlayController) { controller.lifecycleQuery = store }
}

// NewPlayController 装配点播控制器(svc 由 bootstrap 注入)
func NewPlayController(svc PlayService, opts ...PlayControllerOption) *PlayController {
	controller := &PlayController{svc: svc}
	for _, option := range opts {
		option(controller)
	}
	return controller
}

// Start 发起点播
// @Router /api/gb28181/play/{deviceId}/{channelId} [post]
func (pc *PlayController) Start(c *gin.Context) {
	deviceID := c.Param("deviceId")
	channelID := c.Param("channelId")
	audit := playbackAuthorizationAudit(c, "realtime_play_authorization_issue", deviceID, channelID)
	if pc.svc == nil {
		audit["result"] = "unavailable"
		pc.FailAndAbort(c, "点播服务未启用(GB28181 disabled?)", nil)
		return
	}
	if deviceID == "" || channelID == "" {
		audit["result"] = "invalid_argument"
		pc.FailAndAbort(c, "deviceId/channelId 不能为空", nil)
		return
	}
	playRequest, visible := pc.authorizedChannel(c, deviceID, channelID)
	if !visible {
		audit["result"] = "denied"
		return
	}
	attemptID := ""
	attemptOutcome := "failure"
	attemptFailureStage := "play_service"
	var attemptResult *play.Result
	if pc.lifecycleBeginner != nil {
		var lifecycleErr error
		attemptID, lifecycleErr = pc.lifecycleBeginner.Begin(c.Request.Context(), pc.GetCurrentUserID(c), deviceID, channelID)
		if lifecycleErr != nil {
			app.Log(c.Request.Context()).Warn("记录点播 lifecycle 开始失败", zap.String("event", "play.lifecycle_begin_failed"), zap.String("device_id", deviceID), zap.String("channel_id", channelID), logging.Error(lifecycleErr))
		}
		playRequest.LifecycleID = attemptID
	} else if pc.attemptStore != nil {
		var attemptErr error
		attemptID, attemptErr = pc.attemptStore.Begin(c.Request.Context(), pc.GetCurrentUserID(c), deviceID, channelID)
		if attemptErr != nil {
			app.Log(c.Request.Context()).Warn("记录点播 attempt 开始失败", zap.String("event", "play.attempt_begin_failed"), zap.String("device_id", deviceID), zap.String("channel_id", channelID), logging.Error(attemptErr))
		}
	}
	defer func() {
		if pc.lifecycleBeginner != nil || pc.attemptStore == nil || attemptID == "" {
			return
		}
		nodeID, reused := int64(0), false
		if attemptResult != nil {
			reused = attemptResult.Reused
			if attemptResult.Node != nil {
				nodeID = attemptResult.Node.ID
			}
		}
		if err := pc.attemptStore.Finish(context.WithoutCancel(c.Request.Context()), attemptID, attemptOutcome, attemptFailureStage, nodeID, reused); err != nil {
			app.Log(c.Request.Context()).Warn("记录点播 attempt 结果失败", zap.String("event", "play.attempt_finish_failed"), zap.String("device_id", deviceID), zap.String("channel_id", channelID), logging.Error(err))
		}
	}()
	var res *play.Result
	var err error
	if authorized, ok := pc.svc.(AuthorizedPlayService); ok {
		res, err = authorized.StartAuthorized(c.Request.Context(), playRequest)
	} else if gbconfig.CurrentPlayAuthSettings().Enabled {
		err = play.ErrPlayAuthorizationUnavailable
	} else {
		res, err = pc.svc.Start(c.Request.Context(), deviceID, channelID)
	}
	if err != nil {
		audit["result"] = playAuthorizationAuditResult(err)
		if errors.Is(err, play.ErrPlayTimeout) {
			pc.FailAndAbort(c, mapPlayErr(err), err, http.StatusGatewayTimeout)
			return
		}
		pc.FailAndAbort(c, mapPlayErr(err), err)
		return
	}
	attemptResult = res
	attemptOutcome = "success"
	attemptFailureStage = ""
	if res != nil && res.LifecycleID == "" {
		res.LifecycleID = playRequest.LifecycleID
	}
	if res != nil && res.LifecycleID != "" && pc.feedbackSigner != nil && pc.feedbackStore != nil {
		grant, feedbackErr := pc.feedbackSigner.IssueClientFeedback(playauth.ClientFeedbackBinding{
			UserID: pc.GetCurrentUserID(c), DeviceID: deviceID, ChannelID: channelID, LifecycleID: res.LifecycleID,
			AllowedEvents: []string{play.EventFirstFrame, play.EventPlayerError},
		})
		if feedbackErr != nil {
			app.Log(c.Request.Context()).Warn("签发播放客户端反馈凭据失败",
				zap.String("event", "play.client_feedback_issue_failed"), zap.String("lifecycle_id", res.LifecycleID))
		} else {
			res.ClientFeedbackToken = grant.Token
			res.ClientFeedbackExpiresAt = grant.ExpiresAt.Unix()
		}
	}
	play.ApplyPlaybackSelection(res, res.DefaultProtocol, isSecurePlaybackRequest(c.Request))
	if pc.recordingStarter != nil && res != nil && res.StreamID != "" {
		if err := pc.recordingStarter.BeginPlayback(c.Request.Context(), res.StreamID); err != nil {
			app.Log(c.Request.Context()).Warn("点播成功后启动云端录像失败", zap.String("event", "play.recording_start_failed"), zap.String("stream_id", res.StreamID), logging.Error(err))
		}
	}
	finishPlaybackAuthorizationAudit(audit, res)
	pc.Success(c, res)
}

type clientFeedbackRequest struct {
	Event           string `json:"event"`
	Code            string `json:"code,omitempty"`
	ClientElapsedMS int64  `json:"clientElapsedMs"`
}

func (pc *PlayController) ClientEvent(c *gin.Context) {
	lifecycleID := c.Param("lifecycleId")
	if pc.feedbackSigner == nil || pc.feedbackStore == nil || lifecycleID == "" {
		pc.clientFeedbackUnavailable(c)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	var request clientFeedbackRequest
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		request.ClientElapsedMS < 0 || request.ClientElapsedMS > int64((24*time.Hour)/time.Millisecond) ||
		!validClientFeedback(request.Event, request.Code) {
		pc.clientFeedbackUnavailable(c)
		return
	}
	userID := pc.GetCurrentUserID(c)
	claims, err := pc.feedbackSigner.VerifyClientFeedback(
		c.GetHeader("X-Playback-Feedback-Token"), userID, lifecycleID, request.Event,
	)
	if err != nil || !pc.feedbackChannelVisible(c, claims.DeviceID, claims.ChannelID) {
		pc.clientFeedbackUnavailable(c)
		return
	}
	event := play.LifecycleEvent{
		Stage: play.StageClient, EventName: request.Event, Source: play.SourceClient,
		FactState: play.FactConfirmed,
	}
	if request.ClientElapsedMS > 0 {
		metadata, marshalErr := json.Marshal(map[string]int64{"clientElapsedMs": request.ClientElapsedMS})
		if marshalErr != nil {
			pc.clientFeedbackUnavailable(c)
			return
		}
		event.MetadataJSON = metadata
	}
	if request.Event == play.EventPlayerError {
		event.FactState = play.FactFailed
		event.ReasonCode = request.Code
	}
	if err := pc.feedbackStore.AppendClientEvent(c.Request.Context(), lifecycleID, userID, claims.DeviceID, claims.ChannelID, event); err != nil {
		pc.clientFeedbackUnavailable(c)
		return
	}
	pc.Success(c, gin.H{"accepted": true})
}

func validClientFeedback(event, code string) bool {
	switch event {
	case play.EventFirstFrame:
		return code == ""
	case play.EventPlayerError:
		return code == play.ReasonPlayerError || code == play.ReasonPlayerTimeout
	default:
		return false
	}
}

func (pc *PlayController) feedbackChannelVisible(c *gin.Context, deviceID, channelID string) bool {
	db := app.DB()
	if db == nil {
		return false
	}
	lookup := db.WithContext(c.Request.Context())
	var count int64
	err := lookup.Table("gb_channel").
		Joins("JOIN gb_device feedback_root ON feedback_root.device_id = gb_channel.device_id AND feedback_root.deleted_at IS NULL").
		Scopes(datascope.VisibilityScopeWithDB(c, lookup, "gb_channel.owner_dept_id", "gb_channel.device_id")).
		Where("gb_channel.device_id = ? AND gb_channel.channel_id = ? AND gb_channel.deleted_at IS NULL", deviceID, channelID).
		Count(&count).Error
	return err == nil && count == 1
}

func (pc *PlayController) clientFeedbackUnavailable(c *gin.Context) {
	pc.FailAndAbort(c, "客户端反馈不可用", playauth.ErrClientFeedbackUnavailable)
}

func (pc *PlayController) LifecycleList(c *gin.Context) {
	if pc.lifecycleQuery == nil {
		pc.FailAndAbort(c, "播放日志服务不可用", nil)
		return
	}
	query, err := parsePlayLifecycleQuery(c)
	if err != nil {
		pc.FailAndAbort(c, "播放日志查询参数不合法", err)
		return
	}
	page, err := pc.lifecycleQuery.List(c.Request.Context(), query, pc.lifecycleVisibilityScope(c))
	if err != nil {
		pc.FailAndAbort(c, "查询播放日志失败", err)
		return
	}
	pc.Success(c, page)
}

func (pc *PlayController) LifecycleDetail(c *gin.Context) {
	if pc.lifecycleQuery == nil {
		pc.FailAndAbort(c, "播放日志服务不可用", nil)
		return
	}
	detail, err := pc.lifecycleQuery.Detail(c.Request.Context(), c.Param("lifecycleId"), pc.lifecycleVisibilityScope(c))
	if err != nil {
		if errors.Is(err, gbdashboard.ErrPlayLifecycleNotFound) {
			pc.FailAndAbort(c, "播放日志不存在", nil)
			return
		}
		pc.FailAndAbort(c, "查询播放日志失败", err)
		return
	}
	pc.Success(c, detail)
}

func (pc *PlayController) lifecycleVisibilityScope(c *gin.Context) gbdashboard.QueryScope {
	return datascope.VisibilityScope(c, "gb_device.owner_dept_id", "gb_device.device_id")
}

func parsePlayLifecycleQuery(c *gin.Context) (gbdashboard.PlayLifecycleQuery, error) {
	query := gbdashboard.PlayLifecycleQuery{
		Page:       parsePositiveInt(c.DefaultQuery("page", "1"), 1),
		PageSize:   parsePositiveInt(c.DefaultQuery("pageSize", "10"), 10),
		DeviceCode: c.Query("deviceCode"), ChannelCode: c.Query("channelCode"), StreamID: c.Query("streamId"),
		LifecycleState: c.Query("lifecycleState"), MediaState: c.Query("mediaState"),
		ClientState: c.Query("clientState"), FailureStage: c.Query("failureStage"),
	}
	if raw := c.Query("nodeId"); raw != "" {
		nodeID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || nodeID <= 0 {
			return query, errors.New("invalid nodeId")
		}
		query.NodeID = nodeID
	}
	for raw, target := range map[string]**time.Time{"from": &query.From, "to": &query.To} {
		if value := c.Query(raw); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return query, errors.New("invalid " + raw)
			}
			*target = &parsed
		}
	}
	return query, nil
}

// Authorize returns a fresh short-lived fixed playback URL without starting
// the device. Login and Casbin run before this protected controller; channel
// visibility is checked before the playback service can select a node.
func (pc *PlayController) Authorize(c *gin.Context) {
	deviceID := c.Param("deviceId")
	channelID := c.Param("channelId")
	audit := playbackAuthorizationAudit(c, "fixed_play_authorization_issue", deviceID, channelID)
	service, ok := pc.svc.(FixedPlaybackAuthorizationService)
	if pc.svc == nil || !ok {
		audit["result"] = "unavailable"
		pc.FailAndAbort(c, "固定播放地址预授权不可用", nil)
		return
	}
	if deviceID == "" || channelID == "" {
		audit["result"] = "invalid_argument"
		pc.FailAndAbort(c, "deviceId/channelId 不能为空", nil)
		return
	}
	playRequest, visible := pc.authorizedChannel(c, deviceID, channelID)
	if !visible {
		audit["result"] = "denied"
		return
	}
	result, err := service.AuthorizeFixedPlayback(
		c.Request.Context(), playRequest,
	)
	if err != nil {
		audit["result"] = playAuthorizationAuditResult(err)
		pc.FailAndAbort(c, "固定播放地址预授权失败", err)
		return
	}
	play.ApplyPlaybackSelection(result, result.DefaultProtocol, isSecurePlaybackRequest(c.Request))
	finishPlaybackAuthorizationAudit(audit, result)
	pc.Success(c, result)
}

func playbackAuthorizationAudit(c *gin.Context, action, deviceID, channelID string) map[string]any {
	audit := map[string]any{
		"action":    action,
		"deviceId":  deviceID,
		"channelId": channelID,
		"result":    "denied",
	}
	if claims := common.GetClaims(c); claims != nil {
		audit["userId"] = claims.UserID
	}
	middleware.MarkSensitiveOperation(c, audit)
	return audit
}

func finishPlaybackAuthorizationAudit(audit map[string]any, result *play.Result) {
	if audit == nil || result == nil {
		return
	}
	if result.AuthorizationExpiresAt > 0 {
		audit["result"] = "issued"
	} else {
		audit["result"] = "not_enabled"
	}
	if result.Node != nil {
		audit["nodeId"] = result.Node.ID
	}
	if result.AuthorizationCorrelationID != "" {
		audit["correlationId"] = result.AuthorizationCorrelationID
	}
}

func playAuthorizationAuditResult(err error) string {
	if errors.Is(err, play.ErrPlayAuthorizationUnavailable) {
		return "unavailable"
	}
	return "failed"
}

func requestPlaybackSourceIP(request *http.Request) string {
	if request == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(request.RemoteAddr)
}

func isSecurePlaybackRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwarded, "https")
}

// Stop 停播
//
// 响应 data 结构:{released bool, streamId string}。点播 service 会在释放实时流前
// 统一收尾本次云端录像。
//
// @Router /api/gb28181/play/{streamId} [delete]
func (pc *PlayController) Stop(c *gin.Context) {
	if pc.svc == nil {
		pc.FailAndAbort(c, "点播服务未启用", nil)
		return
	}
	streamID := c.Param("streamId")
	if streamID == "" {
		pc.FailAndAbort(c, "streamId 不能为空", nil)
		return
	}
	ch, visible := pc.streamVisible(c, streamID)
	if !visible {
		return
	}
	// 通道行已经查出来了,顺带作为停播日志的定位字段(无额外查询)
	deviceID, channelID := "", ""
	if ch != nil {
		deviceID, channelID = ch.DeviceID, ch.ChannelID
	}
	if err := pc.svc.Stop(c.Request.Context(), streamID, deviceID, channelID); err != nil {
		pc.FailAndAbort(c, "停止流失败", err)
		return
	}
	pc.Success(c, gin.H{
		"released": true,
		"streamId": streamID,
	}, "已停止直播")
}

// mapPlayErr 把 service 错误翻译成更友好的消息
func mapPlayErr(err error) string {
	switch {
	case errors.Is(err, play.ErrDeviceNotFound):
		return "设备不存在"
	case errors.Is(err, play.ErrDeviceOffline):
		return "设备离线,无法点播"
	case errors.Is(err, play.ErrChannelNotFound):
		return "通道不存在"
	case errors.Is(err, play.ErrStreamNotReady):
		return "流就绪超时,设备未推流"
	case errors.Is(err, play.ErrPlayTimeout):
		return "点播超时"
	default:
		return "点播失败"
	}
}

// authorizedChannel reads personnel visibility and the root device epoch in
// the same statement. Reading the epoch later could upgrade a stale permission
// check after an ownership transfer.
func (pc *PlayController) authorizedChannel(c *gin.Context, deviceID, channelID string) (play.AuthorizedRequest, bool) {
	var rows []struct {
		AccessEpoch        *int64
		ChannelOwnerDeptID uint
		RootOwnerDeptID    uint
	}
	db := app.DB()
	if db == nil {
		pc.FailAndAbort(c, "查询通道失败", nil)
		return play.AuthorizedRequest{}, false
	}
	lookup := db.WithContext(c.Request.Context())
	result := lookup.Table("gb_channel").
		Select("play_root.access_epoch, gb_channel.owner_dept_id AS channel_owner_dept_id, play_root.owner_dept_id AS root_owner_dept_id").
		Joins("JOIN gb_device play_root ON play_root.device_id = gb_channel.device_id AND play_root.deleted_at IS NULL").
		Scopes(datascope.VisibilityScopeWithDB(c, lookup, "gb_channel.owner_dept_id", "gb_channel.device_id")).
		Where("gb_channel.device_id = ? AND gb_channel.channel_id = ? AND gb_channel.deleted_at IS NULL", deviceID, channelID).
		Limit(2).Find(&rows)
	if result.Error != nil {
		pc.FailAndAbort(c, "查询通道失败", result.Error)
		return play.AuthorizedRequest{}, false
	}
	if result.RowsAffected != 1 || len(rows) != 1 {
		pc.FailAndAbort(c, "通道不存在", nil)
		return play.AuthorizedRequest{}, false
	}
	if rows[0].AccessEpoch == nil || *rows[0].AccessEpoch <= 0 || rows[0].ChannelOwnerDeptID != rows[0].RootOwnerDeptID {
		app.Log(c.Request.Context()).Warn("后台播放设备安全投影不一致", zap.String("event", "gb28181.play.security_projection_mismatch"), zap.String("device_id", deviceID), zap.String("channel_id", channelID))
		pc.FailAndAbort(c, "通道不存在", nil)
		return play.AuthorizedRequest{}, false
	}
	return play.AuthorizedRequest{DeviceID: deviceID, ChannelID: channelID, DeviceEpoch: *rows[0].AccessEpoch,
		ClientIP: requestPlaybackSourceIP(c.Request)}, true
}

// streamVisible 校验"流存在且可见"。校验语义与返回 false 的三种拒绝理由一字未变,
// 只是把本来就要查的整行通道返回给调用方,供停播日志携带 device_id/channel_id,
// 避免为了两个日志字段再查一次库。
func (pc *PlayController) streamVisible(c *gin.Context, streamID string) (*gbmodels.GbChannel, bool) {
	var ch gbmodels.GbChannel
	result := app.DB().WithContext(c.Request.Context()).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("stream_id = ?", streamID).
		Limit(1).
		Find(&ch)
	if result.Error != nil {
		pc.FailAndAbort(c, "查询流失败", result.Error)
		return nil, false
	}
	if result.RowsAffected == 0 {
		pc.FailAndAbort(c, "流不存在或无权停播", nil)
		return nil, false
	}
	return &ch, true
}

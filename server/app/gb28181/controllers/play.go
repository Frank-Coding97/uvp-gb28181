package controllers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// PlayController 国标点播 REST
//
//	POST   /api/gb28181/play/:deviceId/:channelId   发起点播
//	DELETE /api/gb28181/play/:streamId              停播
type PlayController struct {
	controllers.Common
	svc             PlayService
	retentionPolicy StreamRetentionPolicy
}

type PlayService interface {
	Start(context.Context, string, string) (*play.Result, error)
	Stop(context.Context, string) error
}

type AuthorizedPlayService interface {
	StartAuthorized(context.Context, string, string, string) (*play.Result, error)
}

type FixedPlaybackAuthorizationService interface {
	AuthorizeFixedPlayback(context.Context, string, string, string) (*play.Result, error)
}

type StreamRetentionPolicy interface {
	ShouldKeepStream(context.Context, string) (bool, error)
}

type PlayControllerOption func(*PlayController)

func WithStreamRetentionPolicy(policy StreamRetentionPolicy) PlayControllerOption {
	return func(controller *PlayController) { controller.retentionPolicy = policy }
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
	if !pc.channelVisible(c, deviceID, channelID) {
		audit["result"] = "denied"
		return
	}
	var res *play.Result
	var err error
	if authorized, ok := pc.svc.(AuthorizedPlayService); ok {
		res, err = authorized.StartAuthorized(c.Request.Context(), deviceID, channelID, requestPlaybackSourceIP(c.Request))
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
	play.ApplyPlaybackSelection(res, res.DefaultProtocol, isSecurePlaybackRequest(c.Request))
	finishPlaybackAuthorizationAudit(audit, res)
	pc.Success(c, res)
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
	if !pc.channelVisible(c, deviceID, channelID) {
		audit["result"] = "denied"
		return
	}
	result, err := service.AuthorizeFixedPlayback(
		c.Request.Context(), deviceID, channelID, requestPlaybackSourceIP(c.Request),
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
// 响应 data 结构:{released bool, streamId string, reason string?}
//   - released=true:通道级停流(BYE + CloseRtpServer + Unbind + ClearStream 全套已执行),前端应刷新列表把"直播中"清掉
//   - released=false:仅结束当前观看者的会话,通道流仍在(通常因 CloudRecordingEnabled),前端应展示 info 提示不刷新
//   - reason:仅 released=false 时给出,当前枚举 "cloud_recording_active";供日志/前端调试,不参与 UI 分支
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
	if !pc.streamVisible(c, streamID) {
		return
	}
	if pc.shouldKeepStream(c, streamID) {
		pc.Success(c, gin.H{
			"released": false,
			"streamId": streamID,
			"reason":   "cloud_recording_active",
		}, "已停止观看,通道云端录制仍在继续")
		return
	}
	if err := pc.svc.Stop(c.Request.Context(), streamID); err != nil {
		pc.FailAndAbort(c, "停止流失败", err)
		return
	}
	pc.Success(c, gin.H{
		"released": true,
		"streamId": streamID,
	}, "已停止直播")
}

// shouldKeepStream 决定是否保留上游流。未注入 policy 视作 keep=false(单元测试 / gb28181 disabled
// 场景走真停);policy 查询报错视作 keep=true 保守派——录制中的流误停不可逆,而会话残留 5min
// reconciler 会兜底清理。
func (pc *PlayController) shouldKeepStream(c *gin.Context, streamID string) bool {
	if pc.retentionPolicy == nil {
		return false
	}
	keep, err := pc.retentionPolicy.ShouldKeepStream(c.Request.Context(), streamID)
	if err != nil {
		if app.ZapLog != nil {
			app.ZapLog.Warn("retention policy 查询失败,保守视作保留流",
				zap.String("streamId", streamID), zap.Error(err))
		}
		return true
	}
	return keep
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

func (pc *PlayController) channelVisible(c *gin.Context, deviceID, channelID string) bool {
	var ch gbmodels.GbChannel
	result := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Limit(1).
		Find(&ch)
	if result.Error != nil {
		pc.FailAndAbort(c, "查询通道失败", result.Error)
		return false
	}
	if result.RowsAffected == 0 {
		pc.FailAndAbort(c, "通道不存在", nil)
		return false
	}
	return true
}

func (pc *PlayController) streamVisible(c *gin.Context, streamID string) bool {
	var ch gbmodels.GbChannel
	result := app.DB().WithContext(c).
		Scopes(datascope.VisibilityScope(c, "owner_dept_id", "device_id")).
		Where("stream_id = ?", streamID).
		Limit(1).
		Find(&ch)
	if result.Error != nil {
		pc.FailAndAbort(c, "查询流失败", result.Error)
		return false
	}
	if result.RowsAffected == 0 {
		pc.FailAndAbort(c, "流不存在或无权停播", nil)
		return false
	}
	return true
}

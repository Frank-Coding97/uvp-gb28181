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
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

const (
	playbackNotFound = "playback_not_found"
	playbackExpired  = "playback_expired"
	playbackInvalid  = "playback_invalid_argument"
	playbackBusy     = "playback_busy"
	playbackControl  = "playback_control_failed"
	playbackFailure  = "playback_unavailable"
)

type playbackCreateBody struct {
	RecordKey     string `json:"recordKey"`
	PlayFrom      string `json:"playFrom"`
	DownloadSpeed uint32 `json:"downloadSpeed"`
}

type playbackActionBody struct {
	Action          string  `json:"action"`
	PositionSeconds float64 `json:"positionSeconds"`
	Scale           float64 `json:"scale"`
}

func (dc *DeviceMgmtController) CreatePlaybackSession(c *gin.Context) {
	dc.createHistoricalSession(c, gbplayback.ModePlayback)
}

func (dc *DeviceMgmtController) CreateDownloadSession(c *gin.Context) {
	dc.createHistoricalSession(c, gbplayback.ModeDownload)
}

func (dc *DeviceMgmtController) createHistoricalSession(c *gin.Context, mode gbplayback.Mode) {
	service, snapshots := dc.playbackRuntime()
	claims := dc.GetClaims(c)
	action := "playback_create"
	if mode == gbplayback.ModeDownload {
		action = "record_download_create"
	}
	audit := map[string]any{"action": action, "result": "invalid_argument"}
	if claims != nil {
		audit["userId"] = claims.UserID
	}
	middleware.MarkSensitiveOperation(c, audit)
	if service == nil || snapshots == nil {
		writePlaybackFailure(c, http.StatusServiceUnavailable, playbackFailure, "设备录像回放服务未装配", "unavailable", "service_unavailable")
		return
	}
	channelID, ok := parseRecordQueryChannelID(c.Param("id"))
	if !ok || claims == nil || claims.UserID == 0 {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	target, ok := dc.loadRecordQueryTargetByID(c, channelID)
	if !ok {
		return
	}
	if target.device.Status != gbmodels.DeviceStatusOnline || target.channel.Status != gbmodels.ChannelStatusOnline {
		writePlaybackFailure(c, http.StatusConflict, playbackFailure, "设备或通道当前离线", "device_offline", "device_offline")
		return
	}
	var body playbackCreateBody
	if err := decodePlaybackJSON(c, &body); err != nil || strings.TrimSpace(body.RecordKey) == "" {
		writePlaybackFailure(c, http.StatusUnprocessableEntity, playbackInvalid, "回放参数不合法", "invalid_argument", "invalid_argument")
		return
	}
	if mode == gbplayback.ModeDownload {
		if body.DownloadSpeed == 0 {
			body.DownloadSpeed = 4
		}
		if body.DownloadSpeed != 1 && body.DownloadSpeed != 2 && body.DownloadSpeed != 4 && body.DownloadSpeed != 8 {
			writePlaybackFailure(c, http.StatusUnprocessableEntity, playbackInvalid, "下载倍速不合法", "invalid_argument", "invalid_argument")
			return
		}
	}
	playFrom, err := parsePlaybackTime(body.PlayFrom, dc.recordQueryConfigSnapshot().Location)
	if err != nil {
		writePlaybackFailure(c, http.StatusUnprocessableEntity, playbackInvalid, "回放时间不合法", "invalid_argument", "invalid_argument")
		return
	}
	if key := strings.TrimSpace(c.GetHeader("Idempotency-Key")); key == "" || len(key) > 128 {
		writePlaybackFailure(c, http.StatusUnprocessableEntity, playbackInvalid, "缺少有效幂等键", "invalid_argument", "invalid_argument")
		return
	} else {
		snapshot, resolveErr := snapshots.Resolve(recordquery.ResolveRequest{RecordKey: body.RecordKey, OwnerUserID: claims.UserID, ChannelID: channelID, PlayFrom: playFrom})
		if resolveErr != nil {
			status, code, stage, message := mapPlaybackSnapshotError(resolveErr)
			writePlaybackFailure(c, status, code, message, stage, string(code))
			return
		}
		result, createErr := service.Create(c.Request.Context(), gbplayback.CreateRequest{OwnerID: strconv.FormatUint(uint64(claims.UserID), 10), DeviceID: target.device.DeviceID,
			ChannelID: strconv.FormatUint(uint64(channelID), 10), SIPChannelID: snapshot.ChannelCode,
			RecordKey: snapshot.RecordKey, IdempotencyKey: key,
			PreferredNodeID: target.device.ZLMNodeID,
			Destination:     net.JoinHostPort(target.device.IP, strconv.Itoa(target.device.Port)), Transport: target.device.Transport,
			TCPMode:         strings.Contains(strings.ToUpper(target.channel.StreamTransport), "TCP"),
			DefaultProtocol: gbconfig.CurrentDefaultPlaybackProtocol(), Secure: isSecurePlaybackRequest(c.Request),
			SegmentStart: snapshot.SegmentStart, SegmentEnd: snapshot.SegmentEnd, PlayFrom: playFrom,
			Mode: mode, DownloadSpeed: body.DownloadSpeed})
		if createErr != nil {
			status, errorCode, stage, msg := mapPlaybackServiceError(createErr)
			writePlaybackFailure(c, status, errorCode, msg, stage, string(errorCode))
			return
		}
		audit["result"] = string(result.Session.State)
		if result.Existing {
			audit["result"] = "idempotent"
		}
		dc.Success(c, playbackSessionView(result.Session))
	}
}

func (dc *DeviceMgmtController) GetPlaybackSession(c *gin.Context) {
	service, _ := dc.playbackRuntime()
	claims := dc.GetClaims(c)
	middleware.MarkSensitiveOperation(c, map[string]any{"action": "playback_get", "sessionId": c.Param("sessionId"), "result": "not_found"})
	if service == nil || claims == nil || claims.UserID == 0 {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	if _, ok := parseRecordQueryChannelID(c.Param("id")); !ok {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	channelID, ok := parseRecordQueryChannelID(c.Param("id"))
	if !ok {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	if _, ok := dc.loadRecordQueryTargetByID(c, channelID); !ok {
		return
	}
	session, ok := service.GetForOwner(c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10))
	if !ok || session.ChannelID != c.Param("id") {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	dc.Success(c, playbackSessionView(session))
}

func (dc *DeviceMgmtController) ActionPlaybackSession(c *gin.Context) {
	service, _ := dc.playbackRuntime()
	claims := dc.GetClaims(c)
	middleware.MarkSensitiveOperation(c, map[string]any{"action": "playback_control", "sessionId": c.Param("sessionId"), "result": "invalid_argument"})
	if service == nil || claims == nil || claims.UserID == 0 {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	channelID, ok := parseRecordQueryChannelID(c.Param("id"))
	if !ok {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	if _, ok := dc.loadRecordQueryTargetByID(c, channelID); !ok {
		return
	}
	if session, found := service.GetForOwner(c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10)); !found || session.ChannelID != strconv.FormatUint(uint64(channelID), 10) {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	var body playbackActionBody
	if err := decodePlaybackJSON(c, &body); err != nil || !validPlaybackAction(body) {
		writePlaybackFailure(c, http.StatusUnprocessableEntity, playbackInvalid, "回放控制参数不合法", "control", "invalid_argument")
		return
	}
	session, err := service.Action(c.Request.Context(), c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10), gbplayback.ActionRequest{Action: body.Action, PositionSeconds: body.PositionSeconds, Scale: body.Scale})
	if err != nil {
		status, code, stage, message := mapPlaybackServiceError(err)
		writePlaybackFailure(c, status, code, message, stage, string(code))
		return
	}
	dc.Success(c, playbackSessionView(session))
}

func (dc *DeviceMgmtController) DeletePlaybackSession(c *gin.Context) {
	service, _ := dc.playbackRuntime()
	claims := dc.GetClaims(c)
	middleware.MarkSensitiveOperation(c, map[string]any{"action": "playback_stop", "sessionId": c.Param("sessionId"), "result": "not_found"})
	if service == nil || claims == nil || claims.UserID == 0 {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	channelID, ok := parseRecordQueryChannelID(c.Param("id"))
	if !ok {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	if _, ok := dc.loadRecordQueryTargetByID(c, channelID); !ok {
		return
	}
	if session, found := service.GetForOwner(c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10)); !found || session.ChannelID != strconv.FormatUint(uint64(channelID), 10) {
		writePlaybackFailure(c, http.StatusNotFound, playbackNotFound, "回放会话不存在", "not_found", "not_found")
		return
	}
	err := service.StopForOwner(c.Request.Context(), c.Param("sessionId"), strconv.FormatUint(uint64(claims.UserID), 10), "user stop")
	if err != nil {
		status, code, stage, message := mapPlaybackServiceError(err)
		writePlaybackFailure(c, status, code, message, stage, string(code))
		return
	}
	dc.Success(c, gin.H{"state": gbplayback.StateStopped})
}

func (dc *DeviceMgmtController) recordQueryConfigSnapshot() gbconfig.RecordQueryConfig {
	runtime := dc.recordQueryRuntime()
	return runtime.config
}

func decodePlaybackJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func parsePlaybackTime(value string, location *time.Location) (time.Time, error) {
	if location == nil || strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New("playback time missing")
	}
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(location), nil
	}
	return time.ParseInLocation(recordQueryWallClockLayout, value, location)
}

func validPlaybackAction(body playbackActionBody) bool {
	switch body.Action {
	case "pause", "resume":
		return true
	case "seek":
		return body.PositionSeconds >= 0
	case "scale":
		return body.Scale == 0.25 || body.Scale == 0.5 || body.Scale == 1 || body.Scale == 2 || body.Scale == 4
	default:
		return false
	}
}

func playbackSessionView(session *gbplayback.Session) gin.H {
	if session == nil {
		return gin.H{}
	}
	return gin.H{"sessionId": session.ID, "mode": session.Mode, "downloadSpeed": session.DownloadSpeed, "state": session.State, "channelId": session.ChannelID, "recordKey": session.RecordKey,
		"segmentStart": session.SegmentStart.Format(time.RFC3339), "segmentEnd": session.SegmentEnd.Format(time.RFC3339),
		"positionSeconds": session.PositionSeconds, "scale": session.Scale, "hasAudio": session.HasAudio, "media": gin.H{"urls": session.MediaURLs,
			"defaultProtocol": session.DefaultProtocol, "protocol": session.Protocol, "url": session.URL, "zlmWebrtc": session.ZLMWebRTC},
		"expiresAt": session.Deadline.Format(time.RFC3339), "errorStage": session.ErrorStage, "errorCode": session.ErrorCode}
}

func mapPlaybackSnapshotError(err error) (int, string, string, string) {
	if errors.Is(err, recordquery.ErrSnapshotExpired) {
		return http.StatusGone, playbackExpired, "expired", "录像段已过期，请重新查询"
	}
	if errors.Is(err, recordquery.ErrSnapshotBoundary) || errors.Is(err, recordquery.ErrSnapshotTampered) {
		return http.StatusUnprocessableEntity, playbackInvalid, "invalid_argument", "录像段参数不合法"
	}
	return http.StatusNotFound, playbackNotFound, "not_found", "录像段不存在或无权限"
}

func mapPlaybackServiceError(err error) (int, string, string, string) {
	var serviceErr *gbplayback.ServiceError
	if errors.As(err, &serviceErr) {
		switch serviceErr.Stage {
		case "media_wait":
			return http.StatusGatewayTimeout, playbackFailure, serviceErr.Stage, "回放媒体等待超时"
		case "node":
			return http.StatusBadGateway, playbackFailure, serviceErr.Stage, "无可用媒体节点"
		case "rtp":
			return http.StatusBadGateway, playbackFailure, serviceErr.Stage, "回放媒体端口申请失败"
		case "invite":
			return http.StatusBadGateway, playbackFailure, serviceErr.Stage, "设备回放信令建立失败"
		default:
			return http.StatusBadGateway, playbackFailure, serviceErr.Stage, "设备录像回放失败"
		}
	}
	switch {
	case errors.Is(err, gbplayback.ErrPlaybackBusy):
		return http.StatusTooManyRequests, playbackBusy, "busy", "当前通道已有回放会话"
	case errors.Is(err, gbplayback.ErrPlaybackNotFound):
		return http.StatusNotFound, playbackNotFound, "not_found", "回放会话不存在"
	case errors.Is(err, gbplayback.ErrInvalidSession), errors.Is(err, gbplayback.ErrInvalidTransition):
		return http.StatusUnprocessableEntity, playbackInvalid, "control", "回放控制参数或状态不合法"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, playbackFailure, "media_wait", "回放媒体等待超时"
	default:
		return http.StatusBadGateway, playbackControl, "control", "回放控制失败"
	}
}

func writePlaybackFailure(c *gin.Context, status int, code, message, stage, stable string) {
	app.Response.Fail(c, message, status, 1, gin.H{"errorCode": code, "errorStage": stage, "stableCode": stable})
}

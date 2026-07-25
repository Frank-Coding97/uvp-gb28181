package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type TalkSessionService interface {
	Create(context.Context, talk.CreateRequest) (*talk.CreateResult, error)
	Get(context.Context, string) (*gbmodels.GbTalkSession, error)
	Renew(context.Context, string) (*gbmodels.GbTalkSession, error)
	Cleanup(context.Context, string, gbmodels.TalkSessionState, string) error
}

type TalkController struct {
	controllers.Common
	service TalkSessionService
	dbFunc  func() *gorm.DB
}

type TalkSessionView struct {
	SessionID string                    `json:"sessionId"`
	Mode      gbmodels.TalkSessionMode  `json:"mode"`
	State     gbmodels.TalkSessionState `json:"state"`
	ExpiresAt time.Time                 `json:"expiresAt"`
	StartedAt *time.Time                `json:"startedAt,omitempty"`
	EndedAt   *time.Time                `json:"endedAt,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

func NewTalkController(service TalkSessionService) *TalkController {
	return &TalkController{service: service, dbFunc: func() *gorm.DB { return app.DB() }}
}

func (c *TalkController) SetDB(dbFunc func() *gorm.DB) { c.dbFunc = dbFunc }

func (c *TalkController) Create(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var request struct {
		Mode gbmodels.TalkSessionMode `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil || !request.Mode.Valid() {
		response.Fail(ctx, "mode 仅支持 broadcast 或 talk", http.StatusBadRequest)
		return
	}
	channel, device, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	actorID := c.GetCurrentUserID(ctx)
	result, err := c.service.Create(ctx.Request.Context(), talk.CreateRequest{
		Channel: channel, Device: device, ActorID: actorID, ActorDeptID: c.actorDeptID(ctx, actorID),
		Mode: request.Mode,
	})
	if err != nil {
		c.writeServiceError(ctx, err)
		return
	}
	c.Success(ctx, result)
}

func (c *TalkController) Get(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	channel, _, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	session, ok := c.loadOwnedSession(ctx, channel.ID)
	if !ok {
		return
	}
	if session.State == gbmodels.TalkSessionActive {
		var err error
		session, err = c.service.Renew(ctx.Request.Context(), session.SessionID)
		if err != nil {
			c.writeServiceError(ctx, err)
			return
		}
	}
	c.Success(ctx, talkSessionView(session))
}

func (c *TalkController) Delete(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	channel, _, ok := c.loadTarget(ctx)
	if !ok {
		return
	}
	session, ok := c.loadOwnedSession(ctx, channel.ID)
	if !ok {
		return
	}
	if !session.State.IsTerminal() {
		if err := c.service.Cleanup(ctx.Request.Context(), session.SessionID, gbmodels.TalkSessionEnded, "user stopped"); err != nil {
			c.writeServiceError(ctx, err)
			return
		}
		session.State = gbmodels.TalkSessionEnded
	}
	c.Success(ctx, gin.H{"sessionId": session.SessionID, "state": session.State})
}

func (c *TalkController) ready(ctx *gin.Context) bool {
	if c == nil || c.service == nil || c.dbFunc == nil || c.dbFunc() == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "语音对讲服务未装配"})
		return false
	}
	return true
}

func (c *TalkController) loadTarget(ctx *gin.Context) (*gbmodels.GbChannel, *gbmodels.GbDevice, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(ctx, "通道 ID 不合法", http.StatusBadRequest)
		return nil, nil, false
	}
	db := c.dbFunc().WithContext(ctx.Request.Context())
	var channel gbmodels.GbChannel
	result := db.Scopes(ownerDeptScope(ctx)).Where("id = ?", uint(id)).Limit(1).Find(&channel)
	if result.Error != nil {
		response.Fail(ctx, "查询通道失败", http.StatusInternalServerError)
		return nil, nil, false
	}
	if result.RowsAffected == 0 {
		response.Fail(ctx, "通道不存在", http.StatusNotFound)
		return nil, nil, false
	}
	var device gbmodels.GbDevice
	result = db.Scopes(ownerDeptScope(ctx)).Where("device_id = ?", channel.DeviceID).Limit(1).Find(&device)
	if result.Error != nil {
		response.Fail(ctx, "查询设备失败", http.StatusInternalServerError)
		return nil, nil, false
	}
	if result.RowsAffected == 0 {
		response.Fail(ctx, "设备不存在", http.StatusNotFound)
		return nil, nil, false
	}
	return &channel, &device, true
}

func (c *TalkController) loadOwnedSession(ctx *gin.Context, channelID uint) (*gbmodels.GbTalkSession, bool) {
	session, err := c.service.Get(ctx.Request.Context(), ctx.Param("sessionId"))
	if err != nil {
		c.writeServiceError(ctx, err)
		return nil, false
	}
	if session == nil || session.ChannelID != channelID {
		response.Fail(ctx, "对讲会话不存在", http.StatusNotFound)
		return nil, false
	}
	return session, true
}

func (c *TalkController) actorDeptID(ctx *gin.Context, actorID uint) uint {
	if actorID == 0 {
		return 0
	}
	var user basemodels.User
	if err := c.dbFunc().WithContext(ctx.Request.Context()).Select("dept_id").Where("id = ?", actorID).Limit(1).Find(&user).Error; err != nil {
		return 0
	}
	return user.DeptID
}

func (c *TalkController) writeServiceError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, talk.ErrLeaseConflict):
		response.Fail(ctx, "通道正在对讲中", http.StatusConflict)
	case errors.Is(err, talk.ErrBroadcastNotImplemented):
		response.Fail(ctx, "标准语音广播尚未实现", http.StatusNotImplemented)
	case errors.Is(err, talk.ErrInvalidTalkSessionMode):
		response.Fail(ctx, "mode 仅支持 broadcast 或 talk", http.StatusBadRequest)
	case errors.Is(err, talk.ErrTalkTargetOffline):
		response.Fail(ctx, "设备或通道离线", http.StatusConflict)
	case errors.Is(err, talk.ErrTalkNodeUnavailable), errors.Is(err, talk.ErrSecurePublishUnavailable), errors.Is(err, talk.ErrTalkActivationUnavailable):
		response.Fail(ctx, "语音对讲媒体服务不可用", http.StatusServiceUnavailable)
	case errors.Is(err, context.DeadlineExceeded):
		response.Fail(ctx, "语音对讲操作超时", http.StatusGatewayTimeout)
	default:
		response.Fail(ctx, "语音对讲操作失败", http.StatusInternalServerError)
	}
}

func talkSessionView(session *gbmodels.GbTalkSession) TalkSessionView {
	return TalkSessionView{
		SessionID: session.SessionID, Mode: session.Mode, State: session.State, ExpiresAt: session.ExpiresAt,
		StartedAt: session.StartedAt, EndedAt: session.EndedAt, Error: session.Error,
	}
}

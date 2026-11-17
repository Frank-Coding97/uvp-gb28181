package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	basecontrollers "uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/middleware"
)

type VideoShareManager interface {
	Status(context.Context, uint, uint) (videoshare.Link, error)
	SetEnabled(context.Context, uint, uint, bool, uint64) (videoshare.Link, error)
	AcquireLink(context.Context, uint, uint, uint64) (videoshare.Link, error)
	Regenerate(context.Context, uint, uint, uint64) (videoshare.Link, error)
}

type VideoShareController struct {
	basecontrollers.Common
	manager VideoShareManager
}

func NewVideoShareController(manager VideoShareManager) *VideoShareController {
	return &VideoShareController{manager: manager}
}

type videoShareMutation struct {
	Enabled  *bool  `json:"enabled" binding:"required"`
	Revision uint64 `json:"revision"`
}

func (vc *VideoShareController) Status(c *gin.Context) {
	channelID, actorID, ok := vc.requestIdentity(c)
	if !ok {
		return
	}
	link, err := vc.manager.Status(c.Request.Context(), channelID, actorID)
	if err != nil {
		vc.writeVideoShareError(c, err)
		return
	}
	link.Signature = ""
	vc.Success(c, link)
}

func (vc *VideoShareController) SetEnabled(c *gin.Context) {
	channelID, actorID, ok := vc.requestIdentity(c)
	if !ok {
		return
	}
	var body videoShareMutation
	if err := c.ShouldBindJSON(&body); err != nil || body.Enabled == nil {
		vc.Fail(c, "分享参数不合法", err, http.StatusBadRequest)
		return
	}
	link, err := vc.manager.SetEnabled(c.Request.Context(), channelID, actorID, *body.Enabled, body.Revision)
	if err != nil {
		vc.writeVideoShareError(c, err)
		return
	}
	vc.Success(c, link)
}

func (vc *VideoShareController) AcquireLink(c *gin.Context) {
	channelID, actorID, ok := vc.requestIdentity(c)
	if !ok {
		return
	}
	var body struct {
		Revision uint64 `json:"revision" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		vc.Fail(c, "分享参数不合法", err, http.StatusBadRequest)
		return
	}
	link, err := vc.manager.AcquireLink(c.Request.Context(), channelID, actorID, body.Revision)
	if err != nil {
		vc.writeVideoShareError(c, err)
		return
	}
	vc.Success(c, link)
}

func (vc *VideoShareController) Regenerate(c *gin.Context) {
	channelID, actorID, ok := vc.requestIdentity(c)
	if !ok {
		return
	}
	var body struct {
		Revision uint64 `json:"revision" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		vc.Fail(c, "分享参数不合法", err, http.StatusBadRequest)
		return
	}
	link, err := vc.manager.Regenerate(c.Request.Context(), channelID, actorID, body.Revision)
	if err != nil {
		vc.writeVideoShareError(c, err)
		return
	}
	vc.Success(c, link)
}

func (vc *VideoShareController) requestIdentity(c *gin.Context) (uint, uint, bool) {
	if vc == nil || vc.manager == nil {
		vc.Fail(c, "视频分享服务尚未装配", nil, http.StatusServiceUnavailable)
		return 0, 0, false
	}
	channelID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || channelID == 0 {
		vc.Fail(c, "通道不存在", err, http.StatusBadRequest)
		return 0, 0, false
	}
	actorID := vc.GetCurrentUserID(c)
	if actorID == 0 {
		vc.Fail(c, "需要登录后管理分享", nil, http.StatusUnauthorized)
		return 0, 0, false
	}
	middleware.MarkSensitiveOperation(c, map[string]any{"event": "video_share.manage", "channel_row_id": uint(channelID), "operator_id": actorID})
	c.Request = c.Request.WithContext(videoshare.WithAuditTraceID(c.Request.Context(), c.Writer.Header().Get("X-Request-ID")))
	return uint(channelID), actorID, true
}

func (vc *VideoShareController) writeVideoShareError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "视频分享操作失败"
	switch {
	case errors.Is(err, videoshare.ErrConflict):
		status, message = http.StatusConflict, "分享状态已变化，请刷新后重试"
	case errors.Is(err, videoshare.ErrNotFound):
		status, message = http.StatusNotFound, "通道不存在"
	case errors.Is(err, videoshare.ErrExpired):
		status, message = http.StatusGone, "分享已失效"
	case errors.Is(err, videoshare.ErrInvalidCredential):
		status, message = http.StatusGone, "分享凭据无效或已失效"
	}
	vc.Fail(c, message, err, status)
}

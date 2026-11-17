package controllers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	basecontrollers "uvplatform.com/uvp-gb28181/app/controllers"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
)

type PublicVideoShareResolver interface {
	ResolvePublic(context.Context, string, string) (videoshare.PublicShare, error)
	CreateViewer(context.Context, string, string) (videoshare.Viewer, error)
	LookupViewer(context.Context, string) (videoshare.ViewerAccess, error)
	CloseViewer(string) error
	RelayHTTPFLV(context.Context, http.ResponseWriter, string)
	RelayHLS(context.Context, http.ResponseWriter, string, string)
}

type PublicVideoShareController struct {
	basecontrollers.Common
	resolver PublicVideoShareResolver
}

func NewPublicVideoShareController(resolver PublicVideoShareResolver) *PublicVideoShareController {
	return &PublicVideoShareController{resolver: resolver}
}

type publicVideoShareResolveRequest struct {
	PublicID  string `json:"publicId" binding:"required"`
	Signature string `json:"signature" binding:"required"`
}

type publicVideoShareViewerRequest struct {
	ViewerToken string `json:"viewerToken" binding:"required"`
}

type publicVideoShareViewerStatus struct {
	ExpiresAt  time.Time `json:"expiresAt"`
	ServerTime time.Time `json:"serverTime"`
}

func (c *PublicVideoShareController) Resolve(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		c.Fail(ctx, "视频分享服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	var body publicVideoShareResolveRequest
	if err := ctx.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.PublicID) == "" || strings.TrimSpace(body.Signature) == "" {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			ctx.Header("Cache-Control", "no-store")
			c.Fail(ctx, "分享请求过大", nil, http.StatusRequestEntityTooLarge)
			return
		}
		c.Fail(ctx, "分享链接无效", nil, http.StatusBadRequest)
		return
	}
	share, err := c.resolver.ResolvePublic(ctx.Request.Context(), body.PublicID, body.Signature)
	if err != nil {
		c.writePublicError(ctx, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	c.Success(ctx, share)
}

func (c *PublicVideoShareController) CreateViewer(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		c.Fail(ctx, "视频分享服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	var body publicVideoShareResolveRequest
	if err := ctx.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.PublicID) == "" || strings.TrimSpace(body.Signature) == "" {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			ctx.Header("Cache-Control", "no-store")
			c.Fail(ctx, "分享请求过大", nil, http.StatusRequestEntityTooLarge)
			return
		}
		c.Fail(ctx, "分享链接无效", nil, http.StatusBadRequest)
		return
	}
	viewer, err := c.resolver.CreateViewer(ctx.Request.Context(), body.PublicID, body.Signature)
	if err != nil {
		c.writePublicError(ctx, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	c.Success(ctx, viewer)
}

func (c *PublicVideoShareController) ViewerStatus(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		c.Fail(ctx, "视频分享服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	var body publicVideoShareViewerRequest
	if err := ctx.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ViewerToken) == "" {
		c.Fail(ctx, "观看会话无效", nil, http.StatusBadRequest)
		return
	}
	access, err := c.resolver.LookupViewer(ctx.Request.Context(), body.ViewerToken)
	if err != nil {
		c.writePublicError(ctx, err)
		return
	}
	ctx.Header("Cache-Control", "no-store")
	c.Success(ctx, publicVideoShareViewerStatus{ExpiresAt: access.ExpiresAt, ServerTime: time.Now().UTC()})
}

func (c *PublicVideoShareController) CloseViewer(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		c.Fail(ctx, "视频分享服务尚未装配", nil, http.StatusServiceUnavailable)
		return
	}
	if err := c.resolver.CloseViewer(ctx.Param("viewerToken")); err != nil {
		c.writePublicError(ctx, err)
		return
	}
	ctx.Status(http.StatusNoContent)
}

func (c *PublicVideoShareController) ServeHTTPFLV(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		http.Error(ctx.Writer, "video share service unavailable", http.StatusServiceUnavailable)
		return
	}
	c.resolver.RelayHTTPFLV(ctx.Request.Context(), ctx.Writer, ctx.Param("viewerToken"))
}

func (c *PublicVideoShareController) ServeHLS(ctx *gin.Context) {
	if c == nil || c.resolver == nil {
		http.Error(ctx.Writer, "video share service unavailable", http.StatusServiceUnavailable)
		return
	}
	if ctx.Request.Header.Get("Range") != "" {
		http.Error(ctx.Writer, "range requests are not supported", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	c.resolver.RelayHLS(ctx.Request.Context(), ctx.Writer, ctx.Param("viewerToken"), ctx.Param("resourceID"))
}

func (c *PublicVideoShareController) writePublicError(ctx *gin.Context, err error) {
	status, message := http.StatusServiceUnavailable, "暂时无法验证分享，请稍后重试"
	switch {
	case errors.Is(err, videoshare.ErrNotFound), errors.Is(err, videoshare.ErrExpired), errors.Is(err, videoshare.ErrInvalidCredential):
		status, message = http.StatusGone, "分享链接已失效"
	}
	c.Fail(ctx, message, err, status)
}

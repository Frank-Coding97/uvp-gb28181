package controllers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

type onlineUserService interface {
	ListOnline(context.Context, service.OnlineSessionFilter) ([]service.OnlineSessionView, int64, error)
	ForceLogout(context.Context, string, string, uint) (*service.ForceLogoutResult, error)
}

type SysOnlineUserController struct {
	Common
	sessions onlineUserService
}

func NewSysOnlineUserController() *SysOnlineUserController {
	return &SysOnlineUserController{Common: Common{}}
}

func newSysOnlineUserControllerWithService(sessions onlineUserService) *SysOnlineUserController {
	return &SysOnlineUserController{Common: Common{}, sessions: sessions}
}

func (c *SysOnlineUserController) sessionService() onlineUserService {
	if c.sessions != nil {
		return c.sessions
	}
	sessions, _ := app.SessionValidator.(*service.AuthSessionService)
	return sessions
}

type onlineSessionListRequest struct {
	PageNum      int    `form:"pageNum"`
	PageSize     int    `form:"pageSize"`
	Username     string `form:"username"`
	DepartmentID uint   `form:"departmentId"`
	ClientIP     string `form:"clientIp"`
	Status       string `form:"status"`
}

func (c *SysOnlineUserController) Heartbeat(ctx *gin.Context) {
	c.Success(ctx, gin.H{"online": true})
}

func (c *SysOnlineUserController) List(ctx *gin.Context) {
	var req onlineSessionListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		c.FailAndAbort(ctx, "在线用户查询参数错误", err, http.StatusBadRequest)
	}
	sessions := c.sessionService()
	if sessions == nil {
		c.FailAndAbort(ctx, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	list, total, err := sessions.ListOnline(ctx.Request.Context(), service.OnlineSessionFilter{
		PageNum: req.PageNum, PageSize: req.PageSize, Username: req.Username,
		DepartmentID: req.DepartmentID, ClientIP: req.ClientIP, Status: req.Status,
	})
	if err != nil {
		status := http.StatusServiceUnavailable
		if strings.Contains(err.Error(), "invalid online session status") {
			status = http.StatusBadRequest
		}
		c.FailAndAbort(ctx, "查询在线用户失败", err, status)
	}
	currentSID := ""
	if claims := common.GetClaims(ctx); claims != nil {
		currentSID = claims.SID
	}
	c.Success(ctx, gin.H{"list": list, "total": total, "currentSid": currentSID})
}

type forceLogoutRequest struct {
	SID string `json:"sid" binding:"required"`
}

func (c *SysOnlineUserController) ForceLogout(ctx *gin.Context) {
	var req forceLogoutRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.FailAndAbort(ctx, "会话标识不能为空", err, http.StatusBadRequest)
	}
	claims := common.GetClaims(ctx)
	if claims == nil {
		c.FailAndAbort(ctx, "登录状态无效", service.ErrSessionUnavailable, http.StatusUnauthorized)
	}
	sessions := c.sessionService()
	if sessions == nil {
		c.FailAndAbort(ctx, "认证会话服务不可用", service.ErrSessionStore, http.StatusServiceUnavailable)
	}
	result, err := sessions.ForceLogout(ctx.Request.Context(), req.SID, claims.SID, claims.UserID)
	metadata := map[string]any{"targetSession": maskSessionID(req.SID), "result": "failed"}
	if result != nil {
		metadata["targetUsername"] = result.Username
		metadata["targetIP"] = result.ClientIP
		if result.Revoked {
			metadata["result"] = "revoked"
		} else {
			metadata["result"] = "already_offline"
		}
	}
	middleware.MarkSensitiveOperation(ctx, metadata)
	if err != nil {
		status := http.StatusServiceUnavailable
		message := "强制下线失败"
		switch {
		case errors.Is(err, service.ErrCurrentSessionForceLogout):
			status, message = http.StatusBadRequest, "不能强退当前会话"
		case errors.Is(err, service.ErrSessionUnavailable):
			status, message = http.StatusNotFound, "会话已离线"
		}
		c.FailAndAbort(ctx, message, err, status)
	}
	if !result.Revoked {
		c.SuccessWithMessage(ctx, "会话已离线", gin.H{"offline": true})
		return
	}
	c.SuccessWithMessage(ctx, "强制下线成功", gin.H{"offline": true})
}

func maskSessionID(sid string) string {
	const visible = 8
	if len(sid) <= visible {
		return "***"
	}
	return sid[:visible] + "..."
}

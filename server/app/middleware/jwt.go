package middleware

import (
	"errors"
	"net/http"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/common"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// JWTAuthMiddleware JWT认证中间件
func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if app.TokenService == nil || app.SessionValidator == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "认证会话服务不可用"})
			c.Abort()
			return
		}
		tokenString, err := common.GetAccessToken(c)
		if err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Error("Get access token failed", zap.Error(err))
			}
			// 401 未认证
			c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			c.Abort()
			return
		}

		// 验证token
		claims, err := app.TokenService.ValidateTokenWithCache(tokenString)
		if err != nil {
			if app.ZapLog != nil {
				app.ZapLog.Error("Invalid token", zap.Error(err))
			}
			// 401 未认证
			c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
			c.Abort()
			return
		}
		if err := app.SessionValidator.ValidateSession(c.Request.Context(), claims.SID, claims.UserID); err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, service.ErrSessionStore) {
				status = http.StatusServiceUnavailable
			}
			c.JSON(status, gin.H{"message": "登录会话无效"})
			c.Abort()
			return
		}
		// 将用户信息存储到上下文中
		c.Set(consts.BindContextKeyName, claims)
		if err := app.SessionValidator.TouchSession(c.Request.Context(), claims.SID); err != nil && app.ZapLog != nil {
			app.ZapLog.Warn("Update session activity failed", zap.Error(err))
		}
		// 继续处理请求
		c.Next()
	}
}

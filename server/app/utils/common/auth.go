package common

import (
	"context"
	"errors"
	"strings"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"

	"github.com/gin-gonic/gin"
)

// 获取access token
func GetAccessToken(c *gin.Context) (string, error) {
	// 首先尝试从 Authorization header 获取 token
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		headerParts := strings.Split(authHeader, " ")
		if len(headerParts) == 2 && headerParts[0] == "Bearer" {
			return headerParts[1], nil
		}
	}

	// 如果 header 中没有找到有效的 token，尝试从 URL 参数中获取
	token := c.Query("token")
	if token != "" {
		return token, nil
	}

	// 如果 header 格式不正确但没有 token 参数，返回格式错误
	if authHeader != "" {
		return "", errors.New("authorization header format must be Bearer {token}")
	}

	// 都没有找到 token
	return "", errors.New("token is required, either in Authorization header (Bearer {token}) or as URL parameter (?token={token})")
}

// GetClaims 从上下文获取 Claims
func GetClaims(c *gin.Context) *app.Claims {
	claims, exists := c.Get(consts.BindContextKeyName)
	if !exists {
		return nil
	}
	// 类型断言
	res, ok := claims.(*app.Claims)
	if !ok {
		return nil
	}
	return res
}

// GetCurrentUserID 获取当前用户ID
func GetCurrentUserID(c *gin.Context) uint {
	claims := GetClaims(c)
	if claims == nil {
		return 0
	}
	return claims.UserID
}

// 获取当前租户ID，去租户化后固定为空
func GetCurrentTenantID(c *gin.Context) uint {
	return 0
}

// 获取当前租户Code，去租户化后固定为空
func GetCurrentTenantCode(c *gin.Context) string {
	return ""
}

// 尝试将context.Context转换成*gin.Context
func TryConvertToGinContext(c context.Context) *gin.Context {
	if c == nil {
		return nil
	}
	ginCtx, ok := c.(*gin.Context)
	if !ok {
		return nil
	}
	return ginCtx
}

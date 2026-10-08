package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/common"
)

// InitialPasswordMiddleware limits an authenticated setup session to profile/logout.
func InitialPasswordMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if (c.Request.Method == http.MethodGet && c.FullPath() == "/api/users/profile") ||
			(c.Request.Method == http.MethodPost && c.FullPath() == "/api/users/logout") {
			c.Next()
			return
		}
		var user models.User
		result := app.DBContext(c.Request.Context()).Select("id", "must_change_password").Where("id = ?", common.GetCurrentUserID(c)).First(&user)
		if result.Error != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 1, "message": "读取初始化状态失败"})
			return
		}
		if user.MustChangePassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "message": "请先修改初始密码", "mustChangePassword": true})
			return
		}
		c.Next()
	}
}

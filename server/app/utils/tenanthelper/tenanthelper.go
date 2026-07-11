package tenanthelper

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TenantScope 兼容旧调用，去租户化后不再追加租户过滤
func TenantScope(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db
	}
}

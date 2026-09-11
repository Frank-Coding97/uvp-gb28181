package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func ownerDeptScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.OwnerDeptScope(c, "owner_dept_id")
}

// visibleScope 设备可见性过滤(归属 OR 共享),用于设备/通道表查询
// (两表均有 20 位设备编码列 device_id,目录树/异常记录等无编码列的资源仍用 ownerDeptScope)。
func visibleScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.VisibilityScope(c, "owner_dept_id", "device_id")
}

func transactionalVisibleScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Scopes(datascope.VisibilityScopeWithDB(c, db, "owner_dept_id", "device_id"))
	}
}

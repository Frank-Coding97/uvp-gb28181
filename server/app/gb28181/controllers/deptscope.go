package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func ownerDeptScope(c *gin.Context) func(*gorm.DB) *gorm.DB {
	return datascope.OwnerDeptScope(c, "owner_dept_id")
}

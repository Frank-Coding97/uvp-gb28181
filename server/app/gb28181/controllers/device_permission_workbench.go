package controllers

import (
	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/assign"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func (dc *DeviceMgmtController) PermissionWorkbenchSummary(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	deptIDs, needFilter := datascope.GetOwnerDeptIDsWithDB(c, db)
	result, err := assign.NewQueryService(db, visibleScope(c)).Summary(c, deptIDs, needFilter)
	if err != nil {
		dc.FailAndAbort(c, "查询设备权限汇总失败", err)
		return
	}
	dc.Success(c, result)
}

func (dc *DeviceMgmtController) ResolvePermissionWorkbenchDevices(c *gin.Context) {
	var body struct {
		DeviceIDs []uint `json:"deviceIds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.DeviceIDs) == 0 {
		dc.FailAndAbort(c, "deviceIds 必填", err)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	result, err := assign.NewQueryService(db, visibleScope(c)).Resolve(c, body.DeviceIDs)
	if err != nil {
		dc.FailAndAbort(c, "解析设备失败", err)
		return
	}
	dc.Success(c, result)
}

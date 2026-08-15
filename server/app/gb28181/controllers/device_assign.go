package controllers

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/assign"
	"uvplatform.cn/uvp-gb28181/app/gb28181/grant"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// deptValidatorFor 把 datascope 的 gin 上下文校验适配为 assign.Service 的 DeptValidator。
func deptValidatorFor(c *gin.Context) assign.DeptValidator {
	return func(_ context.Context, db *gorm.DB) ([]uint, bool, error) {
		deptIDs, needFilter := datascope.GetOwnerDeptIDsWithDB(c, db)
		return deptIDs, needFilter, nil
	}
}

// AssignDevices POST /device-mgmt/assign 批量调整设备归属(逐台事务)。
func (dc *DeviceMgmtController) AssignDevices(c *gin.Context) {
	var body struct {
		DeviceIDs    []uint `json:"deviceIds"`
		TargetDeptID uint   `json:"targetDeptId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "参数不合法", err)
		return
	}
	if len(body.DeviceIDs) == 0 || body.TargetDeptID == 0 {
		dc.FailAndAbort(c, "deviceIds 与 targetDeptId 必填", nil)
		return
	}

	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	svc := assign.NewService(db, deptValidatorFor(c))
	result, err := svc.AssignBatch(c, body.DeviceIDs, body.TargetDeptID)
	if err != nil {
		dc.FailAndAbort(c, "分配失败", err)
		return
	}
	succeeded := 0
	for _, item := range result.Results {
		if item.Success {
			succeeded++
		}
	}
	if succeeded > 0 {
		playauth.BumpRevocation(time.Now())
	}
	dc.Success(c, gin.H{"results": result.Results})
}

// AssignDeptDevices POST /device-mgmt/assign-dept 整部门设备流转。
func (dc *DeviceMgmtController) AssignDeptDevices(c *gin.Context) {
	var body struct {
		SourceDeptID uint `json:"sourceDeptId"`
		TargetDeptID uint `json:"targetDeptId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "参数不合法", err)
		return
	}
	if body.SourceDeptID == 0 || body.TargetDeptID == 0 {
		dc.FailAndAbort(c, "sourceDeptId 与 targetDeptId 必填", nil)
		return
	}

	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}

	// 源部门设备必须在操作者可见范围内(visibleScope)
	var deviceIDs []uint
	if err := db.WithContext(c).Model(&gbmodels.GbDevice{}).
		Scopes(visibleScope(c)).
		Where("owner_dept_id = ?", body.SourceDeptID).
		Pluck("id", &deviceIDs).Error; err != nil {
		dc.FailAndAbort(c, "查询源部门设备失败", err)
		return
	}

	svc := assign.NewService(db, deptValidatorFor(c))
	result, err := svc.AssignBatch(c, deviceIDs, body.TargetDeptID)
	if err != nil {
		dc.FailAndAbort(c, "整部门分配失败", err)
		return
	}
	succeeded := 0
	for _, item := range result.Results {
		if item.Success {
			succeeded++
		}
	}
	if succeeded > 0 {
		playauth.BumpRevocation(time.Now())
	}
	dc.Success(c, gin.H{"total": len(deviceIDs), "succeeded": succeeded, "results": result.Results})
}

// ListGrants GET /device-mgmt/device/:id/grants 设备的共享授权列表。
func (dc *DeviceMgmtController) ListGrants(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		dc.FailAndAbort(c, "设备 ID 不合法", nil)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	list, err := grant.NewRepo(db).ListByDevice(c, uint(id))
	if err != nil {
		dc.FailAndAbort(c, "查询共享授权失败", err)
		return
	}
	dc.Success(c, gin.H{"list": list})
}

// AddGrants POST /device-mgmt/device/:id/grants 批量添加共享授权。
func (dc *DeviceMgmtController) AddGrants(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		dc.FailAndAbort(c, "设备 ID 不合法", nil)
		return
	}
	var body struct {
		Targets []struct {
			Type string `json:"type"`
			ID   uint   `json:"id"`
		} `json:"targets"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "参数不合法", err)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}

	// 设备必须在操作者可见范围内
	var deviceCount int64
	if err := db.WithContext(c).Model(&gbmodels.GbDevice{}).Scopes(visibleScope(c)).
		Where("id = ?", uint(id)).Count(&deviceCount).Error; err != nil {
		dc.FailAndAbort(c, "校验设备失败", err)
		return
	}
	if deviceCount == 0 {
		dc.FailAndAbort(c, "设备不存在或不可见", nil)
		return
	}

	operatorID := currentOperatorID(c)
	targets := make([]grant.GrantTarget, 0, len(body.Targets))
	for _, t := range body.Targets {
		if (t.Type != gbmodels.GrantTargetTypeDept && t.Type != gbmodels.GrantTargetTypeUser) || t.ID == 0 {
			dc.FailAndAbort(c, "共享目标不合法", nil)
			return
		}
		targets = append(targets, grant.GrantTarget{Type: t.Type, ID: t.ID, CreatedBy: operatorID})
	}

	added, skipped, err := grant.NewRepo(db).CreateBatch(c, uint(id), targets)
	if err != nil {
		dc.FailAndAbort(c, "添加共享失败", err)
		return
	}
	dc.Success(c, gin.H{"added": added, "skipped": skipped})
}

// RemoveGrant DELETE /device-mgmt/device/:id/grants/:grantId 取消共享授权。
func (dc *DeviceMgmtController) RemoveGrant(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		dc.FailAndAbort(c, "设备 ID 不合法", nil)
		return
	}
	grantID, err := strconv.Atoi(c.Param("grantId"))
	if err != nil || grantID <= 0 {
		dc.FailAndAbort(c, "授权 ID 不合法", nil)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	if err := grant.NewRepo(db).Remove(c, uint(id), uint(grantID)); err != nil {
		dc.FailAndAbort(c, "取消共享失败", err)
		return
	}
	playauth.BumpRevocation(time.Now())
	dc.Success(c, gin.H{"removed": true})
}

// currentOperatorID 当前操作人 ID(claims)。
func currentOperatorID(c *gin.Context) uint {
	claims := common.GetClaims(c)
	if claims == nil {
		return 0
	}
	return claims.UserID
}

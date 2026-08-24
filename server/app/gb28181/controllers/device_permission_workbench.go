package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/assign"
	"uvplatform.cn/uvp-gb28181/app/gb28181/grant"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

func deptValidatorFor(c *gin.Context) assign.DeptValidator {
	return func(_ context.Context, db *gorm.DB) ([]uint, bool, error) {
		deptIDs, needFilter := datascope.GetOwnerDeptIDsWithDB(c, db)
		return deptIDs, needFilter, nil
	}
}

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

func (dc *DeviceMgmtController) QueryPermissionWorkbenchGrants(c *gin.Context) {
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
	result, err := grant.NewService(db, visibleScope(c)).Query(c, body.DeviceIDs)
	if err != nil {
		dc.Fail(c, "查询共享授权失败", err, http.StatusInternalServerError)
		return
	}
	dc.Success(c, result)
}

func (dc *DeviceMgmtController) SearchPermissionWorkbenchGrantTargets(c *gin.Context) {
	targetType := c.Query("type")
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	if pageErr != nil || pageSizeErr != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		dc.Fail(c, "page 与 pageSize 参数不合法", nil, http.StatusBadRequest)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	access, err := datascope.ResolveOwnerDeptAccessByUserID(c, db, dc.GetCurrentUserID(c))
	if err != nil {
		if errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
			dc.Fail(c, "无可授权的数据范围", err, http.StatusForbidden)
			return
		}
		dc.Fail(c, "解析可授权范围失败", err, http.StatusInternalServerError)
		return
	}
	result, err := grant.NewService(db, nil).SearchTargets(c, targetType, c.Query("q"), page, pageSize, access)
	if err != nil {
		if errors.Is(err, grant.ErrTargetTypeInvalid) {
			dc.Fail(c, err.Error(), err, http.StatusBadRequest)
			return
		}
		dc.Fail(c, "查询共享目标失败", err, http.StatusInternalServerError)
		return
	}
	dc.Success(c, result)
}

func (dc *DeviceMgmtController) ApplyPermissionWorkbenchGrants(c *gin.Context) {
	var request grant.ApplyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.Fail(c, "共享授权请求不合法", err, http.StatusBadRequest)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	access := datascope.OwnerDeptAccess{}
	if request.Mode == grant.ApplyModeAdd {
		var err error
		access, err = datascope.ResolveOwnerDeptAccessByUserID(c, db, dc.GetCurrentUserID(c))
		if err != nil {
			if errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
				dc.Fail(c, "无可授权的数据范围", err, http.StatusForbidden)
				return
			}
			dc.Fail(c, "解析可授权范围失败", err, http.StatusInternalServerError)
			return
		}
	}
	request.CreatedBy = currentOperatorID(c)
	result, err := grant.NewService(db, transactionalVisibleScope(c)).Apply(c, request, access)
	if err != nil {
		if errors.Is(err, grant.ErrApplyRequestInvalid) {
			dc.Fail(c, err.Error(), err, http.StatusBadRequest)
			return
		}
		dc.Fail(c, "应用共享授权失败", err, http.StatusInternalServerError)
		return
	}
	// 当前播放授权没有目标用户身份，不能用全局 BumpRevocation 误伤其他共享方。
	middleware.MarkSensitiveOperation(c, map[string]any{
		"operation":       "grant_apply",
		"mode":            string(request.Mode),
		"targets":         safeGrantTargets(request.Targets),
		"requested":       result.Summary.Requested,
		"changed":         result.Summary.Changed,
		"skipped":         result.Summary.Skipped,
		"failed":          result.Summary.Failed,
		"added":           result.Summary.Added,
		"removed":         result.Summary.Removed,
		"failedDeviceIds": failedGrantDeviceIDs(result.Results),
	})
	dc.Success(c, result)
}

func (dc *DeviceMgmtController) ApplyPermissionWorkbenchAssignments(c *gin.Context) {
	var body struct {
		Items        []assign.AssignmentInput `json:"items"`
		TargetDeptID uint                     `json:"targetDeptId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Items) == 0 || body.TargetDeptID == 0 {
		dc.FailAndAbort(c, "items 与 targetDeptId 必填", err)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	result, err := assign.NewService(db, deptValidatorFor(c)).AssignBatchV2(c, body.Items, body.TargetDeptID)
	if err != nil {
		dc.FailAndAbort(c, "调整设备归属失败", err)
		return
	}
	if result.Summary.Changed > 0 {
		playauth.BumpRevocation(time.Now())
	}
	middleware.MarkSensitiveOperation(c, map[string]any{
		"operation":       "assignment_apply",
		"mode":            "devices",
		"targetDeptId":    body.TargetDeptID,
		"requested":       result.Summary.Requested,
		"changed":         result.Summary.Changed,
		"skipped":         result.Summary.Skipped,
		"failed":          result.Summary.Failed,
		"failedDeviceIds": failedAssignmentDeviceIDs(result.Results),
	})
	dc.Success(c, result)
}

func (dc *DeviceMgmtController) ApplyPermissionWorkbenchDepartmentAssignment(c *gin.Context) {
	var body struct {
		SourceDeptID    uint `json:"sourceDeptId"`
		TargetDeptID    uint `json:"targetDeptId"`
		IncludeChildren bool `json:"includeChildren"`
		ExpectedCount   int  `json:"expectedCount"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TargetDeptID == 0 || body.ExpectedCount < 0 {
		dc.FailAndAbort(c, "sourceDeptId、targetDeptId 与 expectedCount 参数不合法", err)
		return
	}
	if body.SourceDeptID == 0 && body.IncludeChildren {
		dc.Fail(c, "未分配集合不能包含子部门", nil, http.StatusBadRequest)
		return
	}
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	sourceDeptIDs, err := resolveAssignmentSourceDepartments(c, db, body.SourceDeptID, body.IncludeChildren)
	if err != nil {
		dc.Fail(c, err.Error(), err, http.StatusBadRequest)
		return
	}

	query := db.WithContext(c).Model(&gbmodels.GbDevice{}).Scopes(visibleScope(c))
	if body.SourceDeptID == 0 {
		query = query.Where("owner_dept_id = ?", 0)
	} else {
		query = query.Where("owner_dept_id IN ?", sourceDeptIDs)
	}
	var devices []gbmodels.GbDevice
	if err := query.Select("id, owner_dept_id").Order("id ASC").Find(&devices).Error; err != nil {
		dc.FailAndAbort(c, "查询源部门设备失败", err)
		return
	}
	if len(devices) != body.ExpectedCount {
		dc.Fail(c, "设备范围已变化，请刷新预览后重试", nil, http.StatusConflict)
		return
	}
	items := make([]assign.AssignmentInput, 0, len(devices))
	for _, device := range devices {
		items = append(items, assign.AssignmentInput{DeviceID: device.ID, ExpectedOwnerDeptID: device.OwnerDeptID})
	}
	result, err := assign.NewService(db, deptValidatorFor(c)).AssignBatchV2(c, items, body.TargetDeptID)
	if err != nil {
		dc.FailAndAbort(c, "整部门调整归属失败", err)
		return
	}
	if result.Summary.Changed > 0 {
		playauth.BumpRevocation(time.Now())
	}
	middleware.MarkSensitiveOperation(c, map[string]any{
		"operation":       "assignment_apply",
		"mode":            "department",
		"sourceDeptId":    body.SourceDeptID,
		"targetDeptId":    body.TargetDeptID,
		"includeChildren": body.IncludeChildren,
		"requested":       result.Summary.Requested,
		"changed":         result.Summary.Changed,
		"skipped":         result.Summary.Skipped,
		"failed":          result.Summary.Failed,
		"failedDeviceIds": failedAssignmentDeviceIDs(result.Results),
	})
	dc.Success(c, result)
}

func currentOperatorID(c *gin.Context) uint {
	claims := common.GetClaims(c)
	if claims == nil {
		return 0
	}
	return claims.UserID
}

func safeGrantTargets(targets []grant.ApplyTarget) []map[string]any {
	result := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		result = append(result, map[string]any{"type": target.Type, "id": target.ID})
	}
	return result
}

func failedGrantDeviceIDs(results []grant.ApplyResultItem) []uint {
	failed := make([]uint, 0)
	for _, result := range results {
		if result.Status == "failed" {
			failed = append(failed, result.DeviceID)
		}
	}
	return failed
}

func failedAssignmentDeviceIDs(results []assign.AssignmentResultItemV2) []uint {
	failed := make([]uint, 0)
	for _, result := range results {
		if result.Status == assign.AssignmentFailed {
			failed = append(failed, result.DeviceID)
		}
	}
	return failed
}

func resolveAssignmentSourceDepartments(c *gin.Context, db *gorm.DB, sourceDeptID uint, includeChildren bool) ([]uint, error) {
	if sourceDeptID == 0 {
		return []uint{0}, nil
	}
	visibleDeptIDs, needFilter := datascope.GetOwnerDeptIDsWithDB(c, db)
	if needFilter && !uintSliceContains(visibleDeptIDs, sourceDeptID) {
		return nil, assign.ErrDeviceNotVisible
	}
	var departments []basemodels.SysDepartment
	query := db.WithContext(c).Where("status = ? OR status IS NULL", 1)
	if needFilter {
		query = query.Where("id IN ?", visibleDeptIDs)
	}
	if err := query.Find(&departments).Error; err != nil {
		return nil, err
	}
	children := make(map[uint][]uint)
	found := false
	for _, department := range departments {
		if department.ID == sourceDeptID {
			found = true
		}
		if department.ParentID != nil {
			children[*department.ParentID] = append(children[*department.ParentID], department.ID)
		}
	}
	if !found {
		return nil, assign.ErrDeviceNotVisible
	}
	result := []uint{sourceDeptID}
	if !includeChildren {
		return result, nil
	}
	for index := 0; index < len(result); index++ {
		result = append(result, children[result[index]]...)
	}
	return result, nil
}

func uintSliceContains(values []uint, target uint) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

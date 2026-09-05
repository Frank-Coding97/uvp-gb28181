package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/plugin/dbresolver"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

const maintenanceOperationPageSizeMax = 200

var errMaintenanceActorNotFound = errors.New("操作者不存在")

type deviceRebootRequest struct {
	Confirmed      bool   `json:"confirmed"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// MaintenanceOperation is the public, deliberately narrow view of a device
// maintenance operation. It does not expose payload, device/channel
// snapshots, SIP correlation, or response bodies.
type MaintenanceOperation struct {
	OperationID      string                      `json:"operationId"`
	Action           string                      `json:"action"`
	Status           gbmodels.PTZOperationStatus `json:"status"`
	SIPStatus        int                         `json:"sipStatus"`
	ErrorMessage     string                      `json:"errorMessage"`
	ActorID          uint                        `json:"actorId"`
	CreatedAt        time.Time                   `json:"createdAt"`
	SentAt           *time.Time                  `json:"sentAt"`
	CompletedAt      *time.Time                  `json:"completedAt"`
	ResponseRequired bool                        `json:"responseRequired"`
	TargetCode       string                      `json:"targetCode"`
}

func (dc *DeviceMgmtController) loadMaintenanceDevice(c *gin.Context) (*gbmodels.GbDevice, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "设备 ID 不合法", err)
		return nil, false
	}
	return dc.loadMaintenanceDeviceByID(c, uint(id))
}

func (dc *DeviceMgmtController) loadMaintenanceDeviceByID(c *gin.Context, id uint) (*gbmodels.GbDevice, bool) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return nil, false
	}
	if id == 0 {
		dc.FailAndAbort(c, "设备 ID 不合法", nil)
		return nil, false
	}
	var device gbmodels.GbDevice
	result := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&device)
	if result.Error != nil {
		dc.FailAndAbort(c, "查询设备失败", result.Error)
		return nil, false
	}
	if result.RowsAffected == 0 {
		dc.FailAndAbort(c, "设备不存在或无权限", nil)
		return nil, false
	}
	return &device, true
}

func (dc *DeviceMgmtController) requireMaintenanceAuthentication(c *gin.Context) bool {
	if dc.GetCurrentUserID(c) != 0 {
		return true
	}
	dc.FailAndAbort(c, "未登录", nil)
	return false
}

// checkDeviceRebootPermission is a defense-in-depth check for the legacy
// channel control entry. The real route is already protected by JWT/Casbin;
// any direct invocation must satisfy the same authenticated permission check.
func (dc *DeviceMgmtController) checkDeviceRebootPermission(c *gin.Context, deviceID uint) bool {
	userID := dc.GetCurrentUserID(c)
	if userID == 0 {
		dc.FailAndAbort(c, "未登录", nil)
		return false
	}
	if app.ConfigYml != nil {
		for _, skipped := range app.ConfigYml.GetUintSlice("server.notcheckuser") {
			if skipped == userID {
				return true
			}
		}
	}
	if app.CasbinV2 == nil {
		dc.FailAndAbort(c, "设备重启权限服务未就绪", nil)
		return false
	}
	path := fmt.Sprintf("/api/gb28181/device-mgmt/device/%d/reboot", deviceID)
	allowed, err := app.CasbinV2.Enforce(fmt.Sprintf("user_%d", userID), path, http.MethodPost, "*")
	if err != nil {
		dc.FailAndAbort(c, "校验设备重启权限失败", err)
		return false
	}
	if !allowed {
		dc.FailAndAbort(c, "无设备重启权限", nil)
		return false
	}
	return true
}

func (dc *DeviceMgmtController) maintenanceActor(c *gin.Context) (uint, uint, error) {
	actorID := dc.GetCurrentUserID(c)
	if actorID == 0 {
		return 0, 0, nil
	}
	db := dc.db()
	var user basemodels.User
	result := db.WithContext(c).Select("id, dept_id").Where("id = ?", actorID).Limit(1).Find(&user)
	if result.Error != nil {
		return 0, 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, 0, errMaintenanceActorNotFound
	}
	return actorID, user.DeptID, nil
}

// RebootDevice sends a standard DeviceControl/TeleBoot to the registered
// parent device. The device itself is the authorization and availability
// target; no channel must exist or be online.
func (dc *DeviceMgmtController) RebootDevice(c *gin.Context) {
	if !dc.requireMaintenanceAuthentication(c) {
		return
	}
	service := dc.ptzServiceSnapshot()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "设备控制服务未就绪"})
		return
	}
	var request deviceRebootRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "设备重启参数不合法", err)
		return
	}
	if !request.Confirmed {
		dc.FailAndAbort(c, "远程重启需要显式确认", nil)
		return
	}
	device, ok := dc.loadMaintenanceDevice(c)
	if !ok {
		return
	}
	actorID, actorDeptID, err := dc.maintenanceActor(c)
	if err != nil {
		dc.FailAndAbort(c, "读取操作者部门失败", err)
		return
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	operation, deduplicated, err := service.ExecuteDeviceRebootDetailed(c.Request.Context(), ptz.DeviceRebootTarget{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline,
		Profile: profileForDevice(device),
	}, key, actorID, actorDeptID)
	if err != nil {
		dc.FailAndAbort(c, "下发设备重启失败", err)
		return
	}
	dc.deviceControlSuccess(c, operation, "teleboot", deduplicated)
}

// ListMaintenanceOperations returns only TeleBoot records for one authorized
// parent device, including old channel-context operations for history.
func (dc *DeviceMgmtController) ListMaintenanceOperations(c *gin.Context) {
	if !dc.requireMaintenanceAuthentication(c) {
		return
	}
	if _, ok := dc.loadMaintenanceDevice(c); !ok {
		return
	}
	db := dc.db()
	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("pageSize", "10"), 10)
	if pageSize > maintenanceOperationPageSizeMax {
		pageSize = maintenanceOperationPageSizeMax
	}
	deviceID, _ := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	query := db.Clauses(dbresolver.Write).WithContext(c).
		Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND action = ?", deviceID, "teleboot")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		dc.FailAndAbort(c, "查询设备维护记录统计失败", err)
		return
	}
	var operations []gbmodels.GbPTZOperation
	if err := query.Select("operation_id, action, status, sip_status, error_message, device_error, device_id, device_code, actor_id, created_at, sent_at, completed_at, response_required, target_code, queue_deadline_at").
		Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&operations).Error; err != nil {
		dc.FailAndAbort(c, "查询设备维护记录失败", err)
		return
	}
	list := make([]MaintenanceOperation, 0, len(operations))
	now := dc.now()
	for _, operation := range operations {
		status := operation.Status
		if status == gbmodels.PTZOperationQueued {
			queueDeadline := operation.QueueDeadlineAt
			if queueDeadline == nil {
				// Legacy one-way TeleBoot rows predate queue_deadline_at; keep
				// their crash visibility bounded by the same one-minute window.
				fallbackDeadline := operation.CreatedAt.Add(time.Minute)
				queueDeadline = &fallbackDeadline
			}
			if !queueDeadline.After(now) {
				// A process crash after durable creation leaves a queued one-way
				// command. Expose it as unknown without mutating the audit row.
				status = gbmodels.PTZOperationUnknown
			}
		}
		targetCode := strings.TrimSpace(operation.TargetCode)
		if targetCode == "" {
			targetCode = operation.DeviceCode
		}
		errorMessage := operation.ErrorMessage
		if errorMessage == "" {
			errorMessage = operation.DeviceError
		}
		list = append(list, MaintenanceOperation{
			OperationID: operation.OperationID, Action: operation.Action, Status: status,
			SIPStatus: operation.SIPStatus, ErrorMessage: errorMessage, ActorID: operation.ActorID,
			CreatedAt: operation.CreatedAt, SentAt: operation.SentAt, CompletedAt: operation.CompletedAt,
			ResponseRequired: operation.ResponseRequired, TargetCode: targetCode,
		})
	}
	dc.Success(c, gin.H{"list": list, "total": total, "page": page, "pageSize": pageSize})
}

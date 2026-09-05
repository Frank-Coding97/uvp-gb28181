package controllers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/upgrade"
)

type deviceFirmwareUpgradeRequest struct {
	Confirmed      bool   `json:"confirmed"`
	IdempotencyKey string `json:"idempotencyKey"`
	Firmware       string `json:"firmware"`
	FileURL        string `json:"fileUrl"`
	Manufacturer   string `json:"manufacturer"`
}

func (dc *DeviceMgmtController) UpgradeDeviceFirmware(c *gin.Context) {
	if !dc.requireMaintenanceAuthentication(c) {
		return
	}
	service := dc.firmwareUpgradeServiceSnapshot()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "设备升级服务未就绪"})
		return
	}
	var request deviceFirmwareUpgradeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		dc.FailAndAbort(c, "设备固件升级参数不合法", err)
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
	operation, deduplicated, executeErr := service.Execute(c.Request.Context(), upgrade.Target{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline,
		Profile: profileForDevice(device),
	}, upgrade.Request{
		Confirmed: request.Confirmed, IdempotencyKey: key, Firmware: request.Firmware,
		FileURL: request.FileURL, Manufacturer: request.Manufacturer, ActorID: actorID, ActorDeptID: actorDeptID,
	})
	if executeErr != nil {
		status := firmwareUpgradeErrorHTTPStatus(executeErr)
		if operation.OperationID != "" {
			dc.FailAndAbort(c, "下发设备固件升级失败", executeErr, status, 1, operation)
			return
		}
		dc.FailAndAbort(c, "下发设备固件升级失败", executeErr, status)
		return
	}
	operation.Deduplicated = deduplicated
	dc.Success(c, operation)
}

func (dc *DeviceMgmtController) ListFirmwareUpgrades(c *gin.Context) {
	if !dc.requireMaintenanceAuthentication(c) {
		return
	}
	service := dc.firmwareUpgradeServiceSnapshot()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "设备升级服务未就绪"})
		return
	}
	device, ok := dc.loadMaintenanceDevice(c)
	if !ok {
		return
	}
	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("pageSize", "10"), 10)
	if pageSize > maintenanceOperationPageSizeMax {
		pageSize = maintenanceOperationPageSizeMax
	}
	result, err := service.List(c.Request.Context(), device.ID, page, pageSize)
	if err != nil {
		dc.FailAndAbort(c, "查询设备固件升级记录失败", err, firmwareUpgradeErrorHTTPStatus(err))
		return
	}
	dc.Success(c, result)
}

func firmwareUpgradeErrorHTTPStatus(err error) int {
	switch {
	case errors.Is(err, upgrade.ErrServiceUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, upgrade.ErrDeviceUpgradeBusy), errors.Is(err, upgrade.ErrDeviceRebootBusy), errors.Is(err, upgrade.ErrIdempotencyConflict):
		return http.StatusConflict
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	case errors.Is(err, upgrade.ErrInvalidArgument), errors.Is(err, upgrade.ErrProfileUnsupported), errors.Is(err, upgrade.ErrDeviceOffline):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

package devicequery

import (
	"context"
	"strings"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

// maintenanceOperationActions mirrors the platform's device maintenance record
// whitelist (controllers.deviceMaintenanceOperationActions). OpenAPI never
// widens it from here: a newly added maintenance command must first become
// part of the platform's audited maintenance trail, otherwise an external
// caller could read rows the platform deliberately keeps out of that view.
var maintenanceOperationActions = []string{"teleboot", "format_sd"}

// MaintenanceOperation is one externally visible device maintenance command
// (reboot or storage-card format). It carries the platform audit outcome only;
// internal authorization columns and actor identity stay private.
type MaintenanceOperation struct {
	OperationID  string     `json:"operationId"`
	Action       string     `json:"action"`
	Status       string     `json:"status"`
	SIPStatus    int        `json:"sipStatus"`
	ErrorMessage string     `json:"errorMessage"`
	DeviceError  string     `json:"deviceError"`
	TargetCode   string     `json:"targetCode"`
	RequestedAt  time.Time  `json:"requestedAt"`
	SentAt       *time.Time `json:"sentAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

type MaintenanceOperationPage struct {
	Items    []MaintenanceOperation `json:"items"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
	Total    int64                  `json:"total"`
}

// ListDeviceMaintenanceOperationsInScope returns reboot and storage-card-format
// records for one device the client is allowed to address. History is scoped to
// the device row itself, so commands issued from a channel context (legacy
// rows) stay visible to the same external caller that could have triggered them.
func (s *Service) ListDeviceMaintenanceOperationsInScope(ctx context.Context, scope resource.DepartmentScope, deviceCode string, options resource.DeviceListOptions) (MaintenanceOperationPage, error) {
	device, err := s.scopedDevice(ctx, scope, deviceCode)
	if err != nil {
		return MaintenanceOperationPage{}, err
	}
	page, pageSize, err := normalizePage(options, maxPageSize)
	if err != nil {
		return MaintenanceOperationPage{}, err
	}
	base := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND action IN ?", device.ID, maintenanceOperationActions)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return MaintenanceOperationPage{}, resource.ErrResourceUnavailable
	}
	var rows []gbmodels.GbPTZOperation
	if err := base.Select("operation_id, action, status, sip_status, error_message, device_error, device_code, target_code, created_at, sent_at, completed_at, queue_deadline_at").
		Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return MaintenanceOperationPage{}, resource.ErrResourceUnavailable
	}
	items := make([]MaintenanceOperation, 0, len(rows))
	now := s.currentTime()
	for _, row := range rows {
		status := row.Status
		if status == gbmodels.PTZOperationQueued {
			deadline := row.QueueDeadlineAt
			if deadline == nil {
				// Legacy one-way TeleBoot rows predate queue_deadline_at. The same
				// one-minute crash window the platform applies keeps both views equal.
				fallback := row.CreatedAt.Add(time.Minute)
				deadline = &fallback
			}
			if !deadline.After(now) {
				status = gbmodels.PTZOperationUnknown
			}
		}
		targetCode := strings.TrimSpace(row.TargetCode)
		if targetCode == "" {
			targetCode = row.DeviceCode
		}
		items = append(items, MaintenanceOperation{
			OperationID: row.OperationID, Action: row.Action, Status: string(status),
			SIPStatus: row.SIPStatus, ErrorMessage: row.ErrorMessage, DeviceError: row.DeviceError,
			TargetCode: targetCode, RequestedAt: row.CreatedAt, SentAt: row.SentAt, CompletedAt: row.CompletedAt,
		})
	}
	return MaintenanceOperationPage{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

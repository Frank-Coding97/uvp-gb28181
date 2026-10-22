// Package deviceupgrade exposes a narrow OpenAPI boundary for the existing
// GB/T 28181-2022 whole-device software upgrade service.
package deviceupgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/upgrade"
	openapiadapters "uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	UpgradeScope      = "device:firmware-upgrade:execute"
	ReadScope         = "device:firmware-upgrade:read"
	ListScope         = "device:firmware-upgrade:list"
	UpgradeAdapterKey = "device.maintenance.firmware-upgrade.v1"
	ReadAdapterKey    = "device.maintenance.firmware-upgrade-read.v1"
	ListAdapterKey    = "device.maintenance.firmware-upgrade-list.v1"
	// maxUpgradeHistory bounds one history page. Upgrade attempts are rare and
	// the newest window is what an operator reconciles against; a longer view is
	// a platform-report concern, not a machine-client one.
	maxUpgradeHistory  = 50
	AdapterVersion     = "v1"
	maxOperationIDSize = 64
)

var (
	ErrInvalidRequest        = fmt.Errorf("firmware upgrade request is invalid: %w", openapiadapters.ErrInvalidRequest)
	ErrNotFound              = fmt.Errorf("firmware upgrade resource not found: %w", openapiadapters.ErrNotFound)
	ErrFirmwareUnavailable   = fmt.Errorf("approved firmware is unavailable: %w", openapiadapters.ErrNotFound)
	ErrFirmwareNotCompatible = fmt.Errorf("firmware does not match the device: %w", openapiadapters.ErrCapabilityNotSupported)
	ErrProfileUnsupported    = fmt.Errorf("device protocol does not support firmware upgrade: %w", openapiadapters.ErrCapabilityNotSupported)
	ErrDeviceOffline         = fmt.Errorf("device is offline: %w", openapiadapters.ErrDeviceOffline)
	ErrUpgradeBusy           = fmt.Errorf("device maintenance is busy: %w", openapiadapters.ErrConflict)
	ErrConflict              = fmt.Errorf("firmware upgrade idempotency conflict: %w", openapiadapters.ErrConflict)
	ErrUnavailable           = fmt.Errorf("firmware upgrade service unavailable: %w", openapiadapters.ErrUnavailable)
)

type upgradeExecutor interface {
	Execute(context.Context, upgrade.Target, upgrade.Request) (upgrade.Operation, bool, error)
}

type resolvedDevice struct {
	Target       upgrade.Target
	Manufacturer string
	Model        string
}

type Dispatcher struct {
	db       *gorm.DB
	executor upgradeExecutor
	create   catalogruntime.Adapter
	read     catalogruntime.Adapter
	list     catalogruntime.Adapter
}

// NewDispatcher creates code-owned, fixed-path adapters. A nil DB or executor
// leaves the dispatcher unavailable instead of exposing a partial operation.
func NewDispatcher(db *gorm.DB, executor upgradeExecutor) *Dispatcher {
	dispatcher := &Dispatcher{db: db, executor: executor}
	dispatcher.create = operationAdapter{dispatcher: dispatcher, scope: UpgradeScope, create: true}
	dispatcher.read = operationAdapter{dispatcher: dispatcher, scope: ReadScope}
	dispatcher.list = operationAdapter{dispatcher: dispatcher, scope: ListScope, list: true}
	return dispatcher
}

func (dispatcher *Dispatcher) Adapter(key string) catalogruntime.Adapter {
	if dispatcher == nil {
		return nil
	}
	switch key {
	case UpgradeAdapterKey:
		return dispatcher.create
	case ReadAdapterKey:
		return dispatcher.read
	case ListAdapterKey:
		return dispatcher.list
	default:
		return nil
	}
}

func (dispatcher *Dispatcher) Registrations() []catalogruntime.AdapterRegistration {
	if dispatcher == nil {
		return nil
	}
	return []catalogruntime.AdapterRegistration{
		{Key: UpgradeAdapterKey, ContractVersion: AdapterVersion, Adapter: dispatcher.create},
		{Key: ReadAdapterKey, ContractVersion: AdapterVersion, Adapter: dispatcher.read},
		{Key: ListAdapterKey, ContractVersion: AdapterVersion, Adapter: dispatcher.list},
	}
}

type operationAdapter struct {
	dispatcher *Dispatcher
	scope      string
	create     bool
	list       bool
}

func (adapter operationAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if adapter.dispatcher == nil || adapter.dispatcher.db == nil || adapter.dispatcher.executor == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != adapter.scope {
		return nil, ErrInvalidRequest
	}
	request, ok := invocation.Value.(openapiadapters.Request)
	if !ok || request.ClientID <= 0 || !validDepartmentScope(request.ResourceScope) {
		return nil, ErrInvalidRequest
	}
	deviceCode := strings.TrimSpace(invocation.Params["deviceId"])
	if !validGBCode(deviceCode) {
		return nil, ErrNotFound
	}
	device, err := adapter.dispatcher.resolveDevice(ctx, request.ResourceScope, deviceCode)
	if err != nil {
		return nil, err
	}
	if adapter.create {
		return adapter.dispatcher.execute(ctx, invocation, request, device)
	}
	if adapter.list {
		return adapter.dispatcher.listOperations(ctx, invocation, request, device)
	}
	return adapter.dispatcher.readOperation(ctx, invocation, request, device)
}

func (dispatcher *Dispatcher) execute(ctx context.Context, invocation catalogruntime.Invocation, request openapiadapters.Request, device resolvedDevice) (OperationView, error) {
	if strings.ToUpper(invocation.Method) != http.MethodPost || len(invocation.Query) != 0 {
		return OperationView{}, ErrInvalidRequest
	}
	input, err := decodeInput(invocation.Body)
	if err != nil || !input.Confirmed || !validFirmwareID(input.FirmwareID) {
		return OperationView{}, ErrInvalidRequest
	}
	if device.Target.Profile.Version != protocol.Version2022 {
		return OperationView{}, ErrProfileUnsupported
	}
	if !device.Target.DeviceOnline {
		return OperationView{}, ErrDeviceOffline
	}
	if err := dispatcher.requireApprovedFirmware(ctx, request.ResourceScope, input.FirmwareID, device); err != nil {
		return OperationView{}, err
	}
	idempotencyKey, err := scopedUpgradeIdempotencyKey(request.ClientID, request.IdempotencyKey)
	if err != nil {
		return OperationView{}, ErrInvalidRequest
	}
	operation, deduplicated, executeErr := dispatcher.executor.Execute(ctx, device.Target, upgrade.Request{
		Confirmed: true, IdempotencyKey: idempotencyKey, FirmwareID: input.FirmwareID,
		ActorID: 0, ActorDeptID: request.ResourceScope.OwnerDeptID,
	})
	if executeErr != nil {
		return OperationView{}, normalizeUpgradeError(executeErr)
	}
	view := viewFromOperation(operation)
	view.Deduplicated = deduplicated || operation.Deduplicated
	return view, nil
}

func (dispatcher *Dispatcher) readOperation(ctx context.Context, invocation catalogruntime.Invocation, request openapiadapters.Request, device resolvedDevice) (OperationView, error) {
	if strings.ToUpper(invocation.Method) != http.MethodGet || len(invocation.Body) != 0 || len(invocation.Query) != 0 {
		return OperationView{}, ErrInvalidRequest
	}
	operationID := strings.TrimSpace(invocation.Params["operationId"])
	if !validOperationID(operationID) {
		return OperationView{}, ErrInvalidRequest
	}
	var operation gbmodels.GbDeviceFirmwareUpgrade
	result := dispatcher.db.WithContext(ctx).
		Where("operation_id = ? AND device_id = ? AND device_code = ? AND actor_dept_id = ? AND idempotency_key LIKE ?",
			operationID, device.Target.DeviceID, device.Target.DeviceCode, request.ResourceScope.OwnerDeptID,
			upgradeClientIdempotencyPrefix(request.ClientID)+"%").
		Limit(2).Find(&operation)
	if result.Error != nil {
		return OperationView{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return OperationView{}, ErrNotFound
	}
	return viewFromRow(operation), nil
}

// UpgradeOperationList is the newest-window history of upgrade attempts this
// client submitted for one device. It is ordered newest first so a reconciler
// sees the attempt it just launched without scanning.
type UpgradeOperationList struct {
	Items        []OperationView `json:"items"`
	TotalRecords int             `json:"totalRecords"`
}

func (dispatcher *Dispatcher) listOperations(ctx context.Context, invocation catalogruntime.Invocation, request openapiadapters.Request, device resolvedDevice) (UpgradeOperationList, error) {
	if strings.ToUpper(invocation.Method) != http.MethodGet || len(invocation.Body) != 0 || len(invocation.Query) != 0 {
		return UpgradeOperationList{}, ErrInvalidRequest
	}
	var rows []gbmodels.GbDeviceFirmwareUpgrade
	result := dispatcher.db.WithContext(ctx).
		Where("device_id = ? AND device_code = ? AND actor_dept_id = ? AND idempotency_key LIKE ?",
			device.Target.DeviceID, device.Target.DeviceCode, request.ResourceScope.OwnerDeptID,
			upgradeClientIdempotencyPrefix(request.ClientID)+"%").
		Order("created_at DESC, operation_id DESC").Limit(maxUpgradeHistory).
		Find(&rows)
	if result.Error != nil {
		return UpgradeOperationList{}, ErrUnavailable
	}
	items := make([]OperationView, 0, len(rows))
	for _, row := range rows {
		items = append(items, viewFromRow(row))
	}
	return UpgradeOperationList{Items: items, TotalRecords: len(items)}, nil
}

func (dispatcher *Dispatcher) resolveDevice(ctx context.Context, scope resource.DepartmentScope, deviceCode string) (resolvedDevice, error) {
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, dispatcher.db, scope)
	if err != nil {
		if errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return resolvedDevice{}, ErrInvalidRequest
		}
		return resolvedDevice{}, ErrNotFound
	}
	var devices []gbmodels.GbDevice
	if err := dispatcher.db.WithContext(ctx).
		Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceCode, departmentIDs).
		Limit(2).Find(&devices).Error; err != nil {
		return resolvedDevice{}, ErrUnavailable
	}
	if len(devices) != 1 {
		return resolvedDevice{}, ErrNotFound
	}
	device := devices[0]
	return resolvedDevice{
		Target: upgrade.Target{
			DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
			Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline,
			Profile: protocol.ProfileFor(protocol.Version(device.EffectiveVersion)),
		},
		Manufacturer: strings.TrimSpace(device.Manufacturer), Model: strings.TrimSpace(device.Model),
	}, nil
}

func (dispatcher *Dispatcher) requireApprovedFirmware(ctx context.Context, scope resource.DepartmentScope, firmwareID string, device resolvedDevice) error {
	var firmware gbmodels.GbFirmwareRepository
	result := dispatcher.db.WithContext(ctx).
		Where("firmware_id = ? AND dept_id = ? AND status = ?", firmwareID, scope.OwnerDeptID, gbmodels.FirmwareStatusPublished).
		Limit(2).Find(&firmware)
	if result.Error != nil {
		return ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return ErrFirmwareUnavailable
	}
	if strings.TrimSpace(firmware.Manufacturer) == "" || device.Manufacturer == "" || !strings.EqualFold(strings.TrimSpace(firmware.Manufacturer), device.Manufacturer) {
		return ErrFirmwareNotCompatible
	}
	modelPattern := strings.TrimSpace(firmware.ModelPattern)
	if modelPattern == "" {
		return nil
	}
	compiled, err := regexp.Compile(modelPattern)
	if err != nil {
		return ErrFirmwareUnavailable
	}
	if device.Model == "" || !compiled.MatchString(device.Model) {
		return ErrFirmwareNotCompatible
	}
	return nil
}

type upgradeInput struct {
	Confirmed  bool   `json:"confirmed"`
	FirmwareID string `json:"firmwareId"`
}

func decodeInput(body []byte) (upgradeInput, error) {
	var input upgradeInput
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(trimmed) {
		return input, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return input, err
	}
	if duplicateJSONMember(trimmed) {
		return input, ErrInvalidRequest
	}
	return input, nil
}

func duplicateJSONMember(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := make(map[string]struct{}, 2)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return false
		}
		name, ok := token.(string)
		if !ok {
			return false
		}
		if _, exists := seen[name]; exists {
			return true
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
	}
	return false
}

func scopedUpgradeIdempotencyKey(clientID int64, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if clientID <= 0 || raw == "" || len(raw) > 128 || !utf8.ValidString(raw) {
		return "", ErrInvalidRequest
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", ErrInvalidRequest
		}
	}
	digest := sha256.Sum256([]byte(raw))
	return upgradeClientIdempotencyPrefix(clientID) + hex.EncodeToString(digest[:]), nil
}

func upgradeClientIdempotencyPrefix(clientID int64) string {
	return fmt.Sprintf("openapi-client:%d:", clientID)
}

func validGBCode(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validFirmwareID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func validOperationID(value string) bool {
	if value == "" || len(value) > maxOperationIDSize {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validDepartmentScope(scope resource.DepartmentScope) bool {
	return scope.OwnerDeptID > 0 && (scope.DataScope == resource.DataScopeDepartment || scope.DataScope == resource.DataScopeDepartmentAndChildren)
}

func normalizeUpgradeError(err error) error {
	switch {
	case errors.Is(err, upgrade.ErrInvalidArgument):
		return ErrInvalidRequest
	case errors.Is(err, upgrade.ErrProfileUnsupported):
		return ErrProfileUnsupported
	case errors.Is(err, upgrade.ErrDeviceOffline):
		return ErrDeviceOffline
	case errors.Is(err, upgrade.ErrDeviceUpgradeBusy), errors.Is(err, upgrade.ErrDeviceRebootBusy):
		return ErrUpgradeBusy
	case errors.Is(err, upgrade.ErrIdempotencyConflict):
		return ErrConflict
	case errors.Is(err, upgrade.ErrServiceUnavailable):
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}

// OperationView separates the outbound SIP handoff from the device's command
// and final upgrade state. A 2xx SIP response only advances submissionStatus.
type OperationView struct {
	OperationID      string                         `json:"operationId"`
	Status           gbmodels.FirmwareUpgradeStatus `json:"status"`
	SubmissionStatus string                         `json:"submissionStatus"`
	DeviceStatus     string                         `json:"deviceStatus"`
	Firmware         string                         `json:"firmware"`
	CurrentFirmware  string                         `json:"currentFirmware"`
	Manufacturer     string                         `json:"manufacturer"`
	SIPStatus        int                            `json:"sipStatus"`
	ErrorCode        string                         `json:"errorCode"`
	FailedReason     string                         `json:"failedReason"`
	CreatedAt        time.Time                      `json:"createdAt"`
	SentAt           *time.Time                     `json:"sentAt,omitempty"`
	AcceptedAt       *time.Time                     `json:"acceptedAt,omitempty"`
	CompletedAt      *time.Time                     `json:"completedAt,omitempty"`
	DeadlineAt       *time.Time                     `json:"deadlineAt,omitempty"`
	Deduplicated     bool                           `json:"deduplicated,omitempty"`
}

func viewFromOperation(operation upgrade.Operation) OperationView {
	return operationView(operation.OperationID, operation.Status, operation.Firmware, operation.CurrentFirmware,
		operation.Manufacturer, operation.SIPStatus, operation.ErrorCode, operation.FailedReason,
		operation.CreatedAt, operation.SentAt, operation.AcceptedAt, operation.CompletedAt, operation.DeadlineAt)
}

func viewFromRow(operation gbmodels.GbDeviceFirmwareUpgrade) OperationView {
	return operationView(operation.OperationID, operation.Status, operation.Firmware, operation.CurrentFirmware,
		operation.Manufacturer, operation.SIPStatus, operation.ErrorCode, operation.FailedReason,
		operation.CreatedAt, operation.SentAt, operation.AcceptedAt, operation.CompletedAt, operation.DeadlineAt)
}

func operationView(id string, status gbmodels.FirmwareUpgradeStatus, firmware, currentFirmware, manufacturer string,
	sipStatus int, errorCode, failedReason string, createdAt time.Time, sentAt, acceptedAt, completedAt, deadlineAt *time.Time) OperationView {
	submissionStatus := "pending"
	if sipStatus >= 200 && sipStatus < 300 {
		submissionStatus = "sent"
	}
	if status == gbmodels.FirmwareUpgradeRejected && sipStatus >= 300 {
		submissionStatus = "rejected"
	} else if status == gbmodels.FirmwareUpgradeUnknown {
		submissionStatus = "unknown"
	}
	deviceStatus := "pending"
	switch status {
	case gbmodels.FirmwareUpgradeAccepted:
		deviceStatus = "accepted"
	case gbmodels.FirmwareUpgradeSucceeded:
		deviceStatus = "succeeded"
	case gbmodels.FirmwareUpgradeFailed:
		deviceStatus = "failed"
	case gbmodels.FirmwareUpgradeRejected:
		deviceStatus = "rejected"
	case gbmodels.FirmwareUpgradeUnknown:
		deviceStatus = "unknown"
	}
	return OperationView{
		OperationID: id, Status: status, SubmissionStatus: submissionStatus, DeviceStatus: deviceStatus,
		Firmware: firmware, CurrentFirmware: currentFirmware, Manufacturer: manufacturer, SIPStatus: sipStatus,
		ErrorCode: errorCode, FailedReason: failedReason,
		CreatedAt: createdAt,
		SentAt:    sentAt, AcceptedAt: acceptedAt, CompletedAt: completedAt, DeadlineAt: deadlineAt,
	}
}

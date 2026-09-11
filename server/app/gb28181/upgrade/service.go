package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

const (
	firmwareMaxBytes     = 255
	manufacturerMaxBytes = 255
	fileURLMaxBytes      = 2048
	upgradeDeadline      = 10 * time.Minute
)

var (
	ErrServiceUnavailable  = errors.New("设备升级服务未就绪")
	ErrInvalidArgument     = errors.New("设备升级参数不合法")
	ErrProfileUnsupported  = errors.New("设备不支持 GB/T 28181-2022 设备升级")
	ErrDeviceOffline       = errors.New("设备离线,无法执行固件升级")
	ErrDeviceUpgradeBusy   = errors.New("设备已有升级任务进行中")
	ErrDeviceRebootBusy    = errors.New("设备重启任务进行中")
	ErrIdempotencyConflict = errors.New("幂等键已用于不同设备升级请求")
	ErrProtocolResponse    = errors.New("设备升级协议应答不合法")
)

// SNAllocator is shared with PTZ so all outbound DeviceControl SN values
// remain unique within the running platform process.
type SNAllocator interface {
	NextSN() int
}

type snFloorEnsurer interface {
	EnsureSNFloor(int) error
}

// TrackedSender is the narrow UAC contract needed by an upgrade request.
// Keeping it here avoids coupling the durable upgrade state machine to the
// PTZ scheduler package while still accepting the platform UAC implementation.
type TrackedSender interface {
	SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error)
}

// Target is an immutable snapshot of the registered parent device used by one
// upgrade request. Upgrade is whole-device and therefore has no channel ID.
type Target struct {
	DeviceID     uint
	DeviceCode   string
	IP           string
	Port         int
	Transport    string
	DeviceOnline bool
	Profile      protocol.Profile
}

type Request struct {
	Confirmed      bool
	IdempotencyKey string
	Firmware       string
	FileURL        string
	Manufacturer   string
	ActorID        uint
	ActorDeptID    uint
}

type Service struct {
	db          *gorm.DB
	sender      TrackedSender
	allocator   SNAllocator
	now         func() time.Time
	lockMu      sync.Mutex
	locks       map[uint]*sync.Mutex
	lifecycleMu sync.RWMutex
	retired     bool
}

// NewService creates the durable upgrade service. allocator must be the live
// PTZ service's shared SN allocator; refusing a nil allocator keeps upgrade
// and PTZ DeviceControl sequence numbers in one allocation scope.
func NewService(db *gorm.DB, sender TrackedSender, allocator SNAllocator, now func() time.Time) (*Service, error) {
	if db == nil || allocator == nil {
		return nil, ErrServiceUnavailable
	}
	if now == nil {
		now = time.Now
	}
	var maxSN int64
	if err := db.Clauses(dbresolver.Write).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Select("COALESCE(MAX(sn), 0)").Scan(&maxSN).Error; err != nil {
		return nil, fmt.Errorf("%w: 读取设备升级 SN 失败: %v", ErrServiceUnavailable, err)
	}
	if maxSN < 0 || uint64(maxSN) >= uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: 设备升级 SN 非法", ErrServiceUnavailable)
	}
	if maxSN > 0 {
		ensurer, ok := allocator.(snFloorEnsurer)
		if !ok {
			return nil, fmt.Errorf("%w: 共享 SN 分配器不支持恢复设备升级序列", ErrServiceUnavailable)
		}
		if err := ensurer.EnsureSNFloor(int(maxSN)); err != nil {
			return nil, fmt.Errorf("%w: 恢复设备升级 SN 序列失败: %v", ErrServiceUnavailable, err)
		}
	}
	service := &Service{db: db, sender: sender, allocator: allocator, now: now, locks: make(map[uint]*sync.Mutex)}
	return service, nil
}

// Retire stops this runtime generation from accepting new work and waits for
// every in-flight Execute or inbound response handler to finish. The caller
// must detach the SIP router before calling Retire so no new device message
// can enter while the old generation is draining.
func (s *Service) Retire() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.retired = true
	s.lifecycleMu.Unlock()
}

func (s *Service) deviceLock(deviceID uint) *sync.Mutex {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if lock, ok := s.locks[deviceID]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.locks[deviceID] = lock
	return lock
}

func (s *Service) nextSN() int {
	if s == nil || s.allocator == nil {
		return 0
	}
	return s.allocator.NextSN()
}

func normalizeIdempotencyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return uuid.NewString(), nil
	}
	if !utf8.ValidString(key) || len(key) > 128 {
		return "", fmt.Errorf("%w: 幂等键长度必须为 1-128 字节且是有效 UTF-8", ErrInvalidArgument)
	}
	for _, ch := range key {
		if unicode.IsControl(ch) {
			return "", fmt.Errorf("%w: 幂等键不能包含控制字符", ErrInvalidArgument)
		}
	}
	return key, nil
}

func validateRequest(target Target, request Request) error {
	if !request.Confirmed {
		return fmt.Errorf("%w: 设备升级需要显式确认", ErrInvalidArgument)
	}
	if target.DeviceID == 0 || strings.TrimSpace(target.DeviceCode) == "" {
		return fmt.Errorf("%w: 设备目标不完整", ErrInvalidArgument)
	}
	if target.Profile.Version != protocol.Version2022 {
		return ErrProfileUnsupported
	}
	firmware := strings.TrimSpace(request.Firmware)
	if firmware == "" || len(firmware) > firmwareMaxBytes {
		return fmt.Errorf("%w: Firmware 长度必须为 1-%d 字节", ErrInvalidArgument, firmwareMaxBytes)
	}
	manufacturer := strings.TrimSpace(request.Manufacturer)
	if manufacturer == "" || len(manufacturer) > manufacturerMaxBytes {
		return fmt.Errorf("%w: Manufacturer 长度必须为 1-%d 字节", ErrInvalidArgument, manufacturerMaxBytes)
	}
	fileURL := strings.TrimSpace(request.FileURL)
	if fileURL == "" || len(fileURL) > fileURLMaxBytes {
		return fmt.Errorf("%w: FileURL 长度必须为 1-%d 字节", ErrInvalidArgument, fileURLMaxBytes)
	}
	parsed, err := url.Parse(fileURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%w: FileURL 必须是无用户信息的 HTTP(S) URL", ErrInvalidArgument)
	}
	if net.ParseIP(parsed.Hostname()) == nil && strings.TrimSpace(parsed.Hostname()) == "" {
		return fmt.Errorf("%w: FileURL 主机不能为空", ErrInvalidArgument)
	}
	return nil
}

func activeUpgradeStatuses() []gbmodels.FirmwareUpgradeStatus {
	return []gbmodels.FirmwareUpgradeStatus{
		gbmodels.FirmwareUpgradeQueued,
		gbmodels.FirmwareUpgradeSent,
		gbmodels.FirmwareUpgradeAccepted,
		gbmodels.FirmwareUpgradeUnknown,
	}
}

func hasRecentDeviceReboot(tx *gorm.DB, deviceID uint, now time.Time) (bool, error) {
	var count int64
	err := tx.Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND action = ? AND created_at >= ? AND status IN ?", deviceID, "teleboot", now.Add(-time.Minute), []gbmodels.PTZOperationStatus{
			gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationAccepted, gbmodels.PTZOperationUnknown,
		}).Count(&count).Error
	return count > 0, err
}

func operationMatches(row gbmodels.GbDeviceFirmwareUpgrade, request Request) bool {
	return row.Firmware == strings.TrimSpace(request.Firmware) &&
		row.FileURL == strings.TrimSpace(request.FileURL) &&
		row.Manufacturer == strings.TrimSpace(request.Manufacturer)
}

func (s *Service) findByIdempotency(ctx context.Context, deviceID uint, key string) (gbmodels.GbDeviceFirmwareUpgrade, bool, error) {
	var row gbmodels.GbDeviceFirmwareUpgrade
	result := s.db.Clauses(dbresolver.Write).WithContext(ctx).Where("device_id = ? AND idempotency_key = ?", deviceID, key).Limit(1).Find(&row)
	return row, result.RowsAffected == 1, result.Error
}

func findByIdempotencyTx(tx *gorm.DB, deviceID uint, key string) (gbmodels.GbDeviceFirmwareUpgrade, bool, error) {
	var row gbmodels.GbDeviceFirmwareUpgrade
	result := tx.Where("device_id = ? AND idempotency_key = ?", deviceID, key).Limit(1).Find(&row)
	return row, result.RowsAffected == 1, result.Error
}

func expireDueForDevice(tx *gorm.DB, deviceID uint, now time.Time) error {
	return tx.Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("device_id = ? AND status IN ? AND deadline_at IS NOT NULL AND deadline_at <= ?", deviceID, []gbmodels.FirmwareUpgradeStatus{
			gbmodels.FirmwareUpgradeQueued, gbmodels.FirmwareUpgradeSent, gbmodels.FirmwareUpgradeAccepted,
		}, now).
		Updates(map[string]interface{}{"status": gbmodels.FirmwareUpgradeUnknown, "error_code": "UPGRADE_TIMEOUT", "error_message": "等待设备升级结果超时", "updated_at": now}).Error
}

// Execute persists one request before sending it. A SIP 2xx advances the
// operation to sent; the business response and final result are handled by
// OnMessage asynchronously.
func (s *Service) Execute(ctx context.Context, target Target, request Request) (Operation, bool, error) {
	if s == nil || s.db == nil {
		return Operation{}, false, ErrServiceUnavailable
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.retired {
		return Operation{}, false, ErrServiceUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRequest(target, request); err != nil {
		return Operation{}, false, err
	}
	key, err := normalizeIdempotencyKey(request.IdempotencyKey)
	if err != nil {
		return Operation{}, false, err
	}
	request.IdempotencyKey = key
	request.Firmware = strings.TrimSpace(request.Firmware)
	request.FileURL = strings.TrimSpace(request.FileURL)
	request.Manufacturer = strings.TrimSpace(request.Manufacturer)
	target.DeviceCode = strings.TrimSpace(target.DeviceCode)
	lock := s.deviceLock(target.DeviceID)
	lock.Lock()
	var (
		operation      Operation
		deduplicated   bool
		dispatchBody   []byte
		dispatchTarget Target
		createdRow     gbmodels.GbDeviceFirmwareUpgrade
	)
	err = s.db.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		device, lockErr := gbmodels.LockGBDeviceForMaintenance(tx, target.DeviceID, target.DeviceCode)
		if lockErr != nil {
			return lockErr
		}
		if err := expireDueForDevice(tx, device.ID, s.now()); err != nil {
			return err
		}
		if existing, found, findErr := findByIdempotencyTx(tx, device.ID, key); findErr != nil {
			return findErr
		} else if found {
			if !operationMatches(existing, request) {
				operation = toOperation(existing)
				return ErrIdempotencyConflict
			}
			operation = toOperation(existing)
			operation.Deduplicated = true
			deduplicated = true
			return nil
		}
		if !target.DeviceOnline || device.Status != gbmodels.DeviceStatusOnline {
			return ErrDeviceOffline
		}
		if strings.TrimSpace(device.IP) == "" || device.Port <= 0 {
			return fmt.Errorf("%w: 设备来源地址缺失", ErrInvalidArgument)
		}
		if recent, rebootErr := hasRecentDeviceReboot(tx, device.ID, s.now()); rebootErr != nil {
			return rebootErr
		} else if recent {
			return ErrDeviceRebootBusy
		}
		var active int64
		if err := tx.Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
			Where("device_id = ? AND status IN ?", device.ID, activeUpgradeStatuses()).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrDeviceUpgradeBusy
		}
		sn := s.nextSN()
		if sn <= 0 {
			return ErrServiceUnavailable
		}
		sessionID := uuid.NewString()
		body, buildErr := manscdp.BuildDeviceUpgradeControlWithProfile(target.Profile, device.DeviceID, sn, manscdp.DeviceUpgradeCommand{
			Firmware: request.Firmware, FileURL: request.FileURL, Manufacturer: request.Manufacturer, SessionID: sessionID,
		})
		if buildErr != nil {
			return buildErr
		}
		createdAt := s.now()
		createdRow = gbmodels.GbDeviceFirmwareUpgrade{
			OperationID: uuid.NewString(), IdempotencyKey: key, DeviceID: device.ID, DeviceCode: device.DeviceID,
			Firmware: request.Firmware, FileURL: request.FileURL, Manufacturer: request.Manufacturer, SessionID: sessionID,
			SN: sn, ProfileVersion: string(target.Profile.Version), ProfileCharset: string(target.Profile.Charset),
			Status: gbmodels.FirmwareUpgradeQueued, ActorID: request.ActorID, ActorDeptID: request.ActorDeptID,
			CreatedAt: createdAt, UpdatedAt: createdAt, DeadlineAt: timePtr(createdAt.Add(upgradeDeadline)),
		}
		if createErr := tx.Create(&createdRow).Error; createErr != nil {
			return createErr
		}
		operation = toOperation(createdRow)
		dispatchBody = body
		dispatchTarget = Target{DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: true, Profile: target.Profile}
		return nil
	})
	lock.Unlock()
	if err != nil {
		// A uniqueness race may have committed the same idempotency key just
		// before this transaction observed it. Re-read only that exact key.
		if existing, found, readErr := s.findByIdempotency(ctx, target.DeviceID, key); readErr == nil && found {
			if !operationMatches(existing, request) {
				return toOperation(existing), true, ErrIdempotencyConflict
			}
			replayed := toOperation(existing)
			replayed.Deduplicated = true
			return replayed, true, nil
		}
		return operation, deduplicated, err
	}
	if deduplicated {
		return operation, true, nil
	}
	destination := net.JoinHostPort(dispatchTarget.IP, fmt.Sprintf("%d", dispatchTarget.Port))
	if s.sender == nil {
		_ = s.persistOutbound(ctx, createdRow.ID, uac.TrackedMessageResult{}, s.now(), ErrServiceUnavailable)
		updated, getErr := s.Get(ctx, createdRow.OperationID)
		if getErr != nil {
			return Operation{}, false, getErr
		}
		return updated, false, ErrServiceUnavailable
	}
	result, sendErr := s.sender.SendMessageTracked(ctx, dispatchTarget.DeviceCode, destination, dispatchTarget.Transport, dispatchBody)
	observedAt := s.now()
	if err := s.persistOutbound(ctx, createdRow.ID, result, observedAt, sendErr); err != nil {
		return Operation{}, false, err
	}
	updated, err := s.Get(ctx, createdRow.OperationID)
	if err != nil {
		return Operation{}, false, err
	}
	return updated, false, sendErr
}

func timePtr(value time.Time) *time.Time { return &value }

func (s *Service) persistOutbound(ctx context.Context, rowID uint, result uac.TrackedMessageResult, observedAt time.Time, sendErr error) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	metadata := map[string]interface{}{
		"sip_status": result.StatusCode, "sip_call_id": result.CallID, "sip_cseq": result.CSeq,
		"updated_at": observedAt,
	}
	if result.StatusCode > 0 {
		metadata["sent_at"] = observedAt
	}
	// Correlation metadata belongs to the outbound attempt even if a device
	// response raced the sender return. Status/error fields below remain
	// conditional on queued, so a final terminal result can never be rolled
	// back by a late SIP transport result.
	if err := s.db.Clauses(dbresolver.Write).WithContext(persistCtx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("id = ?", rowID).Updates(metadata).Error; err != nil {
		return err
	}
	if sendErr == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
		updates := map[string]interface{}{"status": gbmodels.FirmwareUpgradeSent}
		result := s.db.Clauses(dbresolver.Write).WithContext(persistCtx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
			Where("id = ? AND status = ?", rowID, gbmodels.FirmwareUpgradeQueued).Updates(updates)
		return result.Error
	}
	status := gbmodels.FirmwareUpgradeRejected
	errorCode := "SIP_SEND_FAILED"
	errorMessage := "设备升级 MESSAGE 发送失败"
	if sendErr != nil {
		errorMessage = sendErr.Error()
	}
	if (result.Attempted && result.StatusCode == 0) || ctx.Err() != nil {
		status = gbmodels.FirmwareUpgradeUnknown
		errorCode = "SIP_TRANSPORT_UNKNOWN"
	}
	updates := map[string]interface{}{"status": status}
	updates["error_code"] = errorCode
	updates["error_message"] = errorMessage
	if status == gbmodels.FirmwareUpgradeRejected {
		updates["completed_at"] = observedAt
	}
	return s.db.Clauses(dbresolver.Write).WithContext(persistCtx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("id = ? AND status = ?", rowID, gbmodels.FirmwareUpgradeQueued).Updates(updates).Error
}

// HasActive reports whether any request, including unknown, blocks a new
// upgrade. Unknown remains blocking until a valid final device result arrives;
// this service does not provide a manual release or resend operation.
func (s *Service) HasActive(ctx context.Context, deviceID uint) (bool, error) {
	if s == nil || s.db == nil {
		return false, ErrServiceUnavailable
	}
	var count int64
	err := s.db.Clauses(dbresolver.Write).WithContext(ctx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("device_id = ? AND status IN ?", deviceID, activeUpgradeStatuses()).Count(&count).Error
	return count > 0, err
}

// ExpireDue durably changes all waiting states past deadline to unknown. It
// never schedules another send; a later valid final notification may still
// converge unknown to succeeded or failed.
func (s *Service) ExpireDue(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrServiceUnavailable
	}
	if now.IsZero() {
		now = s.now()
	}
	return s.db.Clauses(dbresolver.Write).WithContext(ctx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("status IN ? AND deadline_at IS NOT NULL AND deadline_at <= ?", []gbmodels.FirmwareUpgradeStatus{
			gbmodels.FirmwareUpgradeQueued, gbmodels.FirmwareUpgradeSent, gbmodels.FirmwareUpgradeAccepted,
		}, now).
		Updates(map[string]interface{}{"status": gbmodels.FirmwareUpgradeUnknown, "error_code": "UPGRADE_TIMEOUT", "error_message": "等待设备升级结果超时", "updated_at": now}).Error
}

// OnUpgradeMessage consumes only upgrade-related inbound MANSCDP responses. It
// returns false for messages that should continue through the PTZ handler.
func (s *Service) OnUpgradeMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) (bool, error) {
	if s == nil || s.db == nil {
		return true, ErrServiceUnavailable
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.retired {
		return true, ErrServiceUnavailable
	}
	return s.onUpgradeMessage(ctx, deviceCode, callID, cseq, body)
}

func (s *Service) onUpgradeMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) (bool, error) {
	head, err := manscdp.ParseHead(body)
	if err != nil {
		return false, err
	}
	switch head.CmdType {
	case manscdp.CmdDeviceControl:
		return s.onDeviceControlResponse(ctx, deviceCode, callID, cseq, body)
	case manscdp.CmdDeviceUpgradeResult:
		return s.onUpgradeResult(ctx, deviceCode, callID, cseq, body)
	default:
		return false, nil
	}
}

// OnUpgradeResultMessage is the final-result boundary used by the SIP
// handler. Unlike an ordinary DeviceControl business response, a final
// DeviceUpgradeResult is acknowledged only after durable state (and, on a
// matching success, the device's reported firmware) has been written.
// responseStatus is 200 for an unmatched/accepted notification, 400 for a
// malformed protocol payload, and 503 for a persistence/runtime failure.
func (s *Service) OnUpgradeResultMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) (consumed bool, responseStatus int, err error) {
	if s == nil || s.db == nil {
		return true, 503, ErrServiceUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.retired {
		return true, 503, ErrServiceUnavailable
	}
	if _, parseErr := manscdp.ParseHead(body); parseErr != nil {
		return true, 400, fmt.Errorf("%w: %v", ErrProtocolResponse, parseErr)
	}
	consumed, err = s.onUpgradeResult(persistCtx, deviceCode, callID, cseq, body)
	if err == nil {
		return consumed, 200, nil
	}
	if errors.Is(err, ErrProtocolResponse) {
		return true, 400, err
	}
	return true, 503, err
}

// OnMessage is retained as a compatibility alias for package-local callers;
// the handler integration uses the explicit OnUpgradeMessage name.
func (s *Service) OnMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) (bool, error) {
	return s.OnUpgradeMessage(ctx, deviceCode, callID, cseq, body)
}

func (s *Service) findByDeviceSN(ctx context.Context, deviceCode string, sn int) (gbmodels.GbDeviceFirmwareUpgrade, bool, error) {
	var rows []gbmodels.GbDeviceFirmwareUpgrade
	err := s.db.Clauses(dbresolver.Write).WithContext(ctx).Where("device_code = ? AND sn = ?", strings.TrimSpace(deviceCode), sn).
		Order("id DESC").Limit(2).Find(&rows).Error
	if err != nil {
		return gbmodels.GbDeviceFirmwareUpgrade{}, false, err
	}
	if len(rows) == 0 {
		return gbmodels.GbDeviceFirmwareUpgrade{}, false, nil
	}
	if len(rows) > 1 {
		return rows[0], true, fmt.Errorf("%w: DeviceControl SN 关联不唯一", ErrProtocolResponse)
	}
	return rows[0], true, nil
}

func (s *Service) findBySession(ctx context.Context, deviceCode, sessionID string) (gbmodels.GbDeviceFirmwareUpgrade, bool, error) {
	var row gbmodels.GbDeviceFirmwareUpgrade
	result := s.db.Clauses(dbresolver.Write).WithContext(ctx).Where("device_code = ? AND session_id = ?", strings.TrimSpace(deviceCode), strings.TrimSpace(sessionID)).Limit(1).Find(&row)
	return row, result.RowsAffected == 1, result.Error
}

func (s *Service) onDeviceControlResponse(ctx context.Context, deviceCode, callID, cseq string, body []byte) (bool, error) {
	head, headErr := manscdp.ParseHead(body)
	if headErr != nil {
		return false, headErr
	}
	sn, snErr := strconv.Atoi(strings.TrimSpace(head.SN))
	if snErr != nil || sn <= 0 {
		return false, fmt.Errorf("%w: DeviceControl SN 不合法", ErrProtocolResponse)
	}
	// Match by the transport sender and SN before strict response decoding.
	// This prevents an upgrade response from falling through to PTZ merely
	// because a vendor omitted an optional XML detail.
	row, matched, err := s.findByDeviceSN(ctx, deviceCode, sn)
	if !matched {
		return false, err
	}
	if err != nil {
		return true, err
	}
	response, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), body)
	if err != nil {
		return true, err
	}
	if response.DeviceID != row.DeviceCode {
		return true, fmt.Errorf("%w: DeviceControl DeviceID 与升级设备不一致", ErrProtocolResponse)
	}
	lock := s.deviceLock(row.DeviceID)
	lock.Lock()
	defer lock.Unlock()
	return true, s.applyBusiness(ctx, row, response.Result == manscdp.DeviceControlResultOK, callID, cseq)
}

func (s *Service) applyBusiness(ctx context.Context, row gbmodels.GbDeviceFirmwareUpgrade, accepted bool, callID, cseq string) error {
	observedAt := s.now()
	updates := map[string]interface{}{"device_result": map[bool]string{true: "OK", false: "ERROR"}[accepted], "response_at": observedAt, "response_call_id": callID, "response_cseq": cseq, "updated_at": observedAt}
	if accepted {
		updates["status"] = gbmodels.FirmwareUpgradeAccepted
		updates["accepted_at"] = gorm.Expr("COALESCE(accepted_at, ?)", observedAt)
		updates["error_code"] = ""
		updates["error_message"] = ""
		updates["failed_reason"] = ""
	} else {
		updates["status"] = gbmodels.FirmwareUpgradeRejected
		updates["error_code"] = "DEVICE_REJECTED"
		updates["error_message"] = "设备拒绝设备升级命令"
		updates["completed_at"] = observedAt
	}
	allowed := []gbmodels.FirmwareUpgradeStatus{gbmodels.FirmwareUpgradeQueued, gbmodels.FirmwareUpgradeSent, gbmodels.FirmwareUpgradeUnknown}
	if accepted {
		allowed = append(allowed, gbmodels.FirmwareUpgradeAccepted)
	}
	result := s.db.Clauses(dbresolver.Write).WithContext(ctx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("id = ? AND status IN ?", row.ID, allowed).Updates(updates)
	return result.Error
}

func (s *Service) onUpgradeResult(ctx context.Context, deviceCode, callID, cseq string, body []byte) (bool, error) {
	result, err := manscdp.ParseDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), body)
	if err != nil {
		return true, fmt.Errorf("%w: %v", ErrProtocolResponse, err)
	}
	row, matched, err := s.findBySession(ctx, deviceCode, result.SessionID)
	if err != nil {
		return true, err
	}
	if !matched {
		return false, nil
	}
	if result.DeviceID != row.DeviceCode {
		return true, fmt.Errorf("%w: DeviceUpgradeResult DeviceID 与升级设备不一致", ErrProtocolResponse)
	}
	lock := s.deviceLock(row.DeviceID)
	lock.Lock()
	defer lock.Unlock()
	return true, s.applyFinal(ctx, row, result, callID, cseq)
}

func (s *Service) applyFinal(ctx context.Context, row gbmodels.GbDeviceFirmwareUpgrade, result *manscdp.DeviceUpgradeResultNotify, callID, cseq string) error {
	// The final notification changes both the operation and the device's
	// reported firmware. Keep those writes under the same parent-device lock
	// so a new maintenance operation cannot observe a half-applied result.
	return s.db.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := gbmodels.LockGBDeviceForMaintenance(tx, row.DeviceID, row.DeviceCode); err != nil {
			return err
		}
		var current gbmodels.GbDeviceFirmwareUpgrade
		loaded := tx.Where("id = ?", row.ID).Limit(1).Find(&current)
		if loaded.Error != nil {
			return loaded.Error
		}
		if loaded.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if current.Status.Terminal() {
			return nil
		}
		observedAt := s.now()
		updates := map[string]interface{}{
			"current_firmware": result.Firmware, "device_result": result.UpgradeResult,
			"response_at": observedAt, "response_call_id": callID, "response_cseq": cseq, "updated_at": observedAt,
		}
		matchingSuccess := result.UpgradeResult == "OK" && result.Firmware == current.Firmware
		if matchingSuccess {
			updates["status"] = gbmodels.FirmwareUpgradeSucceeded
			updates["error_code"] = ""
			updates["error_message"] = ""
			updates["failed_reason"] = ""
			updates["completed_at"] = observedAt
		} else if result.UpgradeResult == "OK" {
			updates["status"] = gbmodels.FirmwareUpgradeFailed
			updates["error_code"] = "VERSION_MISMATCH"
			updates["error_message"] = fmt.Sprintf("设备上报版本 %q 与目标版本 %q 不一致", result.Firmware, current.Firmware)
			updates["completed_at"] = observedAt
		} else {
			updates["status"] = gbmodels.FirmwareUpgradeFailed
			updates["error_code"] = "DEVICE_UPGRADE_FAILED"
			updates["error_message"] = "设备升级失败"
			updates["failed_reason"] = result.UpgradeFailedReason
			updates["completed_at"] = observedAt
		}
		allowed := []gbmodels.FirmwareUpgradeStatus{gbmodels.FirmwareUpgradeQueued, gbmodels.FirmwareUpgradeSent, gbmodels.FirmwareUpgradeAccepted, gbmodels.FirmwareUpgradeUnknown}
		changed := tx.Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
			Where("id = ? AND status IN ?", current.ID, allowed).Updates(updates)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			// A concurrent terminal transition wins. Do not overwrite the
			// device version from a late duplicate result.
			return nil
		}
		if matchingSuccess {
			return tx.Model(&gbmodels.GbDevice{}).
				Where("id = ? AND device_id = ?", current.DeviceID, current.DeviceCode).
				Update("firmware", result.Firmware).Error
		}
		return nil
	})
}

// Get returns the public operation view. FileURL is never copied into the
// view, so query parameters and temporary access tokens cannot leak.
func (s *Service) Get(ctx context.Context, operationID string) (Operation, error) {
	var row gbmodels.GbDeviceFirmwareUpgrade
	result := s.db.Clauses(dbresolver.Write).WithContext(ctx).Where("operation_id = ?", strings.TrimSpace(operationID)).Limit(1).Find(&row)
	if result.Error != nil {
		return Operation{}, result.Error
	}
	if result.RowsAffected == 0 {
		return Operation{}, gorm.ErrRecordNotFound
	}
	return toOperation(row), nil
}

func (s *Service) List(ctx context.Context, deviceID uint, page, pageSize int) (Page, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if err := s.ExpireDue(ctx, s.now()); err != nil {
		return Page{}, err
	}
	query := s.db.Clauses(dbresolver.Write).WithContext(ctx).Model(&gbmodels.GbDeviceFirmwareUpgrade{}).Where("device_id = ?", deviceID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return Page{}, err
	}
	var rows []gbmodels.GbDeviceFirmwareUpgrade
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return Page{}, err
	}
	list := make([]Operation, 0, len(rows))
	for _, row := range rows {
		list = append(list, toOperation(row))
	}
	return Page{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

type Operation struct {
	OperationID     string                         `json:"operationId"`
	SessionID       string                         `json:"sessionId"`
	DeviceID        uint                           `json:"deviceId"`
	SN              int                            `json:"-"`
	Status          gbmodels.FirmwareUpgradeStatus `json:"status"`
	Firmware        string                         `json:"firmware"`
	CurrentFirmware string                         `json:"currentFirmware"`
	Manufacturer    string                         `json:"manufacturer"`
	SIPStatus       int                            `json:"sipStatus"`
	ErrorCode       string                         `json:"errorCode"`
	ErrorMessage    string                         `json:"errorMessage"`
	FailedReason    string                         `json:"failedReason"`
	ActorID         uint                           `json:"actorId"`
	CreatedAt       time.Time                      `json:"createdAt"`
	SentAt          *time.Time                     `json:"sentAt"`
	AcceptedAt      *time.Time                     `json:"acceptedAt"`
	CompletedAt     *time.Time                     `json:"completedAt"`
	DeadlineAt      *time.Time                     `json:"deadlineAt"`
	Deduplicated    bool                           `json:"deduplicated,omitempty"`
}

type Page struct {
	List     []Operation `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

func toOperation(row gbmodels.GbDeviceFirmwareUpgrade) Operation {
	return Operation{
		OperationID: row.OperationID, SessionID: row.SessionID, DeviceID: row.DeviceID, SN: row.SN, Status: row.Status,
		Firmware: row.Firmware, CurrentFirmware: row.CurrentFirmware, Manufacturer: row.Manufacturer,
		SIPStatus: row.SIPStatus, ErrorCode: row.ErrorCode, ErrorMessage: row.ErrorMessage,
		FailedReason: row.FailedReason, ActorID: row.ActorID, CreatedAt: row.CreatedAt,
		SentAt: row.SentAt, AcceptedAt: row.AcceptedAt, CompletedAt: row.CompletedAt, DeadlineAt: row.DeadlineAt,
	}
}

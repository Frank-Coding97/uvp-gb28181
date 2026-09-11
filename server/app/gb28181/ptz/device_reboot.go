package ptz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const (
	deviceRebootAction        = "teleboot"
	deviceRebootDerivedV1     = "reboot:v1:"
	deviceRebootStatusError   = "DEVICE_REBOOT_SEND_REJECTED"
	deviceRebootStatusUnknown = "DEVICE_REBOOT_TRANSPORT_UNKNOWN"
)

// DeviceRebootTarget contains only the registered parent device facts needed
// for a device-level TeleBoot. ChannelID/ChannelCode intentionally do not
// exist here: a parent reboot must never be represented by a fake channel.
type DeviceRebootTarget struct {
	DeviceID     uint
	DeviceCode   string
	IP           string
	Port         int
	Transport    string
	DeviceOnline bool
	Profile      protocol.Profile
}

type deviceRebootResult struct {
	operation    gbmodels.GbPTZOperation
	deduplicated bool
}

type deviceRebootLockKey struct {
	db       *gorm.DB
	deviceID uint
}

type deviceRebootLockEntry struct {
	mu   sync.Mutex
	refs int
}

var deviceRebootLockTable = struct {
	sync.Mutex
	entries map[deviceRebootLockKey]*deviceRebootLockEntry
}{entries: make(map[deviceRebootLockKey]*deviceRebootLockEntry)}

func acquireDeviceRebootLock(db *gorm.DB, deviceID uint) func() {
	key := deviceRebootLockKey{db: db, deviceID: deviceID}
	deviceRebootLockTable.Lock()
	entry := deviceRebootLockTable.entries[key]
	if entry == nil {
		entry = &deviceRebootLockEntry{}
		deviceRebootLockTable.entries[key] = entry
	}
	entry.refs++
	deviceRebootLockTable.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		deviceRebootLockTable.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(deviceRebootLockTable.entries, key)
		}
		deviceRebootLockTable.Unlock()
	}
}

// ExecuteDeviceReboot creates and sends one parent-device TeleBoot. The
// durable row is committed before the MESSAGE is sent so a process crash
// leaves a visible queued record and cannot silently retry the command.
//
// The pair return is kept aligned with Service.Execute. Callers that need to
// render the deduplication flag should use ExecuteDeviceRebootDetailed.
func (s *Service) ExecuteDeviceReboot(ctx context.Context, target DeviceRebootTarget, idempotencyKey string, actorID, actorDeptID uint) (gbmodels.GbPTZOperation, error) {
	operation, _, err := s.ExecuteDeviceRebootDetailed(ctx, target, idempotencyKey, actorID, actorDeptID)
	return operation, err
}

// ExecuteDeviceRebootDetailed is the device maintenance entry point. It
// returns whether an existing operation was reused by the device-level
// idempotency or the shared 60-second TeleBoot window.
func (s *Service) ExecuteDeviceRebootDetailed(ctx context.Context, target DeviceRebootTarget, rawKey string, actorID, actorDeptID uint) (gbmodels.GbPTZOperation, bool, error) {
	result, err := s.executeDeviceRebootDetailed(ctx, target, rawKey, actorID, actorDeptID)
	return result.operation, result.deduplicated, err
}

func (s *Service) executeDeviceRebootDetailed(ctx context.Context, target DeviceRebootTarget, rawKey string, actorID, actorDeptID uint) (deviceRebootResult, error) {
	var result deviceRebootResult
	if s == nil || s.db == nil {
		return result, operationError(ErrorCodeHomePositionUnavailable, "PTZ 数据库未就绪", nil)
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.retired {
		return result, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 已卸载", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target.DeviceCode = strings.TrimSpace(target.DeviceCode)
	if target.DeviceID == 0 || target.DeviceCode == "" {
		return result, operationError(ErrorCodeHomePositionInvalidArgument, "设备重启目标不完整", nil)
	}
	normalizedKey, err := normalizeIdempotencyKey(rawKey)
	if err != nil {
		return result, err
	}
	derivedKey := deviceRebootIdempotencyKey(target.DeviceID, normalizedKey)
	payloadJSON, err := canonicalPayload(map[string]interface{}{"confirmed": true})
	if err != nil {
		return result, err
	}
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}

	unlock := acquireDeviceRebootLock(s.db, target.DeviceID)
	defer unlock()

	var dispatchTarget DeviceRebootTarget
	var dispatchBody []byte
	err = ptzWriter(s.db).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		device, lockErr := lockDeviceRebootRow(tx, target)
		if lockErr != nil {
			return lockErr
		}

		if existing, found, findErr := findDeviceRebootByKey(tx, device.ID, derivedKey); findErr != nil {
			return findErr
		} else if found {
			result.operation = existing
			result.deduplicated = true
			return nil
		}
		if strings.TrimSpace(rawKey) != "" {
			if existing, found, findErr := findDeviceRebootByKey(tx, device.ID, strings.TrimSpace(rawKey)); findErr != nil {
				return findErr
			} else if found {
				result.operation = existing
				result.deduplicated = true
				return nil
			}
		}
		if existing, found, findErr := findRecentDeviceReboot(tx, device.ID, s.now().Add(-time.Minute)); findErr != nil {
			return findErr
		} else if found {
			result.operation = existing
			result.deduplicated = true
			return nil
		}

		if device.Status != gbmodels.DeviceStatusOnline {
			return operationError(ErrorCodeHomePositionDeviceOffline, "设备离线", nil)
		}
		if active, activeErr := hasActiveFirmwareUpgrade(tx, device.ID); activeErr != nil {
			return operationError(ErrorCodeHomePositionUnavailable, "检查设备升级状态失败", activeErr)
		} else if active {
			return operationError(ErrorCodeHomePositionUnavailable, "设备固件升级进行中,禁止重启", nil)
		}
		if strings.TrimSpace(device.IP) == "" || device.Port <= 0 {
			return operationError(ErrorCodeHomePositionUnavailable, "设备来源地址缺失", nil)
		}
		if s.sender == nil {
			return operationError(ErrorCodeHomePositionUnavailable, "SIP UAC 未就绪", nil)
		}

		sn := s.nextSN()
		createdAt := s.now()
		queueDeadline := createdAt.Add(time.Minute)
		operation := gbmodels.GbPTZOperation{
			OperationID: uuid.NewString(), IdempotencyKey: derivedKey,
			DeviceID: device.ID, DeviceCode: device.DeviceID,
			ChannelID: 0, ChannelCode: "",
			CmdType: manscdp.CmdDeviceControl, Action: deviceRebootAction, PayloadJSON: payloadJSON,
			ProfileVersion: string(profile.Version), ProfileCharset: string(profile.Charset),
			TargetScope: gbmodels.ControlTargetScopeDevice, TargetCode: device.DeviceID,
			ScopeKey: gbmodels.ControlTargetScopeDevice + ":" + device.DeviceID,
			SN:       sn, Status: gbmodels.PTZOperationQueued, Attempt: 1, MaxAttempts: 1,
			ResponseRequired: false, ActorID: actorID, ActorDeptID: actorDeptID, CreatedAt: createdAt, QueueDeadlineAt: &queueDeadline,
		}
		body, buildErr := manscdp.BuildTeleBootControlWithProfile(profile, device.DeviceID, sn, true)
		if buildErr != nil {
			completedAt := createdAt
			operation.Status = gbmodels.PTZOperationRejected
			operation.ErrorCode = deviceRebootStatusError
			operation.ErrorMessage = buildErr.Error()
			operation.CompletedAt = &completedAt
		} else {
			dispatchBody = body
		}
		if createErr := tx.Create(&operation).Error; createErr != nil {
			return createErr
		}
		result.operation = operation
		dispatchTarget = DeviceRebootTarget{
			DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
			Transport: device.Transport, DeviceOnline: true, Profile: profile,
		}
		return nil
	})
	if err != nil {
		// A database uniqueness race may have committed the other request just
		// before this transaction observed the conflict. Re-read only the exact
		// derived key; any read failure remains an error (fail closed).
		if existing, found, readErr := findDeviceRebootByKey(ptzWriter(s.db).WithContext(ctx), target.DeviceID, derivedKey); readErr == nil && found {
			return deviceRebootResult{operation: existing, deduplicated: true}, nil
		}
		return result, err
	}
	if result.deduplicated {
		return result, nil
	}

	if result.operation.Status != gbmodels.PTZOperationQueued {
		return result, nil
	}
	return s.sendDeviceReboot(ctx, dispatchTarget, result.operation, dispatchBody)
}

func deviceRebootIdempotencyKey(deviceID uint, rawKey string) string {
	digest := sha256.Sum256([]byte(strconv.FormatUint(uint64(deviceID), 10) + "\x00" + rawKey))
	return deviceRebootDerivedV1 + hex.EncodeToString(digest[:])
}

func lockDeviceRebootRow(tx *gorm.DB, target DeviceRebootTarget) (gbmodels.GbDevice, error) {
	device, err := gbmodels.LockGBDeviceForMaintenance(tx, target.DeviceID, target.DeviceCode)
	if err == nil {
		return device, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return device, operationError(ErrorCodeHomePositionUnavailable, "设备不存在或已删除", nil)
	}
	if strings.Contains(err.Error(), "device code mismatch") {
		return device, operationError(ErrorCodeHomePositionUnavailable, "设备目标不匹配", nil)
	}
	return device, err
}

func hasActiveFirmwareUpgrade(tx *gorm.DB, deviceID uint) (bool, error) {
	// Firmware upgrade migration is a runtime prerequisite for device-level
	// maintenance. Query errors, including a missing table, stay fail-closed so
	// a reboot can never slip through an unknown upgrade state.
	var count int64
	err := tx.Model(&gbmodels.GbDeviceFirmwareUpgrade{}).
		Where("device_id = ? AND status IN ?", deviceID, []gbmodels.FirmwareUpgradeStatus{
			gbmodels.FirmwareUpgradeQueued, gbmodels.FirmwareUpgradeSent, gbmodels.FirmwareUpgradeAccepted, gbmodels.FirmwareUpgradeUnknown,
		}).Count(&count).Error
	return count > 0, err
}

func findDeviceRebootByKey(db *gorm.DB, deviceID uint, key string) (gbmodels.GbPTZOperation, bool, error) {
	var operation gbmodels.GbPTZOperation
	result := db.Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND action = ? AND idempotency_key = ?", deviceID, deviceRebootAction, key).
		Order("id DESC").Limit(1).Find(&operation)
	return operation, result.RowsAffected > 0, result.Error
}

func findRecentDeviceReboot(db *gorm.DB, deviceID uint, since time.Time) (gbmodels.GbPTZOperation, bool, error) {
	var operation gbmodels.GbPTZOperation
	result := db.Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND action = ? AND created_at >= ?", deviceID, deviceRebootAction, since).
		Where("status IN ?", []gbmodels.PTZOperationStatus{
			gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent, gbmodels.PTZOperationAccepted, gbmodels.PTZOperationUnknown,
		}).Order("id DESC").Limit(1).Find(&operation)
	return operation, result.RowsAffected > 0, result.Error
}

func (s *Service) sendDeviceReboot(ctx context.Context, target DeviceRebootTarget, operation gbmodels.GbPTZOperation, body []byte) (deviceRebootResult, error) {
	result := deviceRebootResult{operation: operation}
	destination := net.JoinHostPort(target.IP, strconv.Itoa(target.Port))
	tracked, sendErr := s.sender.SendMessageTracked(ctx, target.DeviceCode, destination, target.Transport, body)
	observedAt := s.now()
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.persistFirstOutboundMetadata(persistCtx, operation.ID, tracked, observedAt); err != nil {
		return result, err
	}

	status := gbmodels.PTZOperationRejected
	errorCode := deviceRebootStatusError
	errorMessage := tracked.ErrorSummary
	if errorMessage == "" && sendErr != nil {
		errorMessage = sendErr.Error()
	}
	switch {
	case tracked.StatusCode >= 200 && tracked.StatusCode < 300:
		status = gbmodels.PTZOperationSent
		errorCode = ""
		errorMessage = ""
	case tracked.Attempted && tracked.StatusCode < 200:
		status = gbmodels.PTZOperationUnknown
		errorCode = deviceRebootStatusUnknown
	}
	if status == gbmodels.PTZOperationSent {
		updated, err := s.MarkSent(persistCtx, operation.OperationID, tracked, observedAt)
		if err != nil {
			return result, err
		}
		result.operation = updated
		return result, nil
	}
	updates := map[string]interface{}{
		"status": status, "error_code": errorCode, "error_message": errorMessage, "completed_at": observedAt,
	}
	if err := ptzWriter(s.db).WithContext(persistCtx).Model(&gbmodels.GbPTZOperation{}).
		Where("id = ? AND status = ?", operation.ID, gbmodels.PTZOperationQueued).Updates(updates).Error; err != nil {
		return result, err
	}
	if err := ptzWriter(s.db).WithContext(persistCtx).First(&result.operation, operation.ID).Error; err != nil {
		return result, err
	}
	return result, nil
}

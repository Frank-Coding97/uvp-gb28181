package assign

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// ErrAssignmentSecurityUnavailable is returned when the durable device
// security projection cannot prove a safe transfer. Callers must treat it as
// a failed transfer; the surrounding transaction is rolled back.
var ErrAssignmentSecurityUnavailable = errors.New("设备安全状态不可用")

// TransferReceipt is an internal post-commit handoff to later device-scoped
// cleanup. Every field is excluded from JSON so an HTTP request cannot supply
// or observe a receipt as an assignment input/output contract.
type TransferReceipt struct {
	DevicePK            uint      `json:"-"`
	DeviceCode          string    `json:"-"`
	OldOwnerDeptID      uint      `json:"-"`
	NewOwnerDeptID      uint      `json:"-"`
	OldEpoch            int64     `json:"-"`
	NewEpoch            int64     `json:"-"`
	LegacyRevokedBefore time.Time `json:"-"`
}

// DeviceTransferRecorder is the durable OpenAPI side of one assignment
// transaction. Implementations must use the supplied transaction and must not
// perform network I/O.
type DeviceTransferRecorder interface {
	RecordDeviceTransfer(context.Context, *gorm.DB, string, int64) error
}

// ServiceOption customizes only deterministic transfer dependencies. The
// default service always installs the production OpenAPI recorder.
type ServiceOption func(*Service)

func WithTransferClock(clock func() time.Time) ServiceOption {
	return func(service *Service) {
		if clock != nil {
			service.clock = clock
		}
	}
}

func WithTransferRecorder(recorder DeviceTransferRecorder) ServiceOption {
	return func(service *Service) {
		if recorder == nil || isNilTransferRecorder(recorder) {
			return
		}
		service.transferRecorder = recorder
	}
}

func isNilTransferRecorder(recorder DeviceTransferRecorder) bool {
	value := reflect.ValueOf(recorder)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func normalizeAssignmentError(err error) error {
	if errors.Is(err, ErrAssignmentSecurityUnavailable) {
		return ErrAssignmentSecurityUnavailable
	}
	return err
}

func newDefaultTransferRecorder(db *gorm.DB, clock func() time.Time) DeviceTransferRecorder {
	return playauth.NewOpenAPIRevocationStore(db, clock)
}

type assignmentDevice struct {
	ID                  uint       `gorm:"column:id"`
	DeviceCode          string     `gorm:"column:device_id"`
	Name                string     `gorm:"column:name"`
	OwnerDeptID         uint       `gorm:"column:owner_dept_id"`
	AccessEpoch         int64      `gorm:"column:access_epoch"`
	LegacyRevokedBefore *time.Time `gorm:"column:legacy_revoked_before"`
}

func lockAssignmentDevice(tx *gorm.DB, deviceID uint, visibleDeptIDs []uint, needFilter bool) (assignmentDevice, error) {
	var rows []assignmentDevice
	query := lockedAssignmentDeviceTable(tx)
	query = query.Select("id, device_id, name, owner_dept_id").
		Where("id = ? AND deleted_at IS NULL", deviceID)
	if needFilter {
		query = query.Where("owner_dept_id IN ?", visibleDeptIDs)
	}
	result := query.Limit(2).Find(&rows)
	if result.Error != nil {
		return assignmentDevice{}, ErrAssignmentSecurityUnavailable
	}
	if result.RowsAffected == 0 || len(rows) == 0 {
		return assignmentDevice{}, ErrDeviceNotVisible
	}
	if result.RowsAffected != 1 || len(rows) != 1 || rows[0].ID != deviceID {
		return assignmentDevice{}, ErrAssignmentSecurityUnavailable
	}
	if strings.TrimSpace(rows[0].DeviceCode) == "" {
		return assignmentDevice{}, ErrAssignmentSecurityUnavailable
	}
	return rows[0], nil
}

func loadAssignmentSecurity(tx *gorm.DB, deviceID uint) (int64, *time.Time, error) {
	var rows []struct {
		AccessEpoch           *int64     `gorm:"column:access_epoch"`
		CleanupCompletedEpoch *int64     `gorm:"column:cleanup_completed_epoch"`
		LegacyRevokedBefore   *time.Time `gorm:"column:legacy_revoked_before"`
	}
	query := lockedAssignmentDeviceTable(tx)
	result := query.Select("access_epoch, cleanup_completed_epoch, legacy_revoked_before").Where("id = ? AND deleted_at IS NULL", deviceID).Limit(2).Find(&rows)
	if result.Error != nil {
		return 0, nil, ErrAssignmentSecurityUnavailable
	}
	if result.RowsAffected != 1 || len(rows) != 1 || rows[0].AccessEpoch == nil || *rows[0].AccessEpoch <= 0 {
		return 0, nil, ErrAssignmentSecurityUnavailable
	}
	if rows[0].CleanupCompletedEpoch == nil || *rows[0].CleanupCompletedEpoch <= 0 || *rows[0].CleanupCompletedEpoch > *rows[0].AccessEpoch {
		return 0, nil, ErrAssignmentSecurityUnavailable
	}
	if rows[0].LegacyRevokedBefore != nil {
		cutoff := rows[0].LegacyRevokedBefore.UTC()
		if cutoff.Unix() <= 0 || cutoff.Nanosecond() != 0 {
			return 0, nil, ErrAssignmentSecurityUnavailable
		}
		rows[0].LegacyRevokedBefore = &cutoff
	}
	return *rows[0].AccessEpoch, rows[0].LegacyRevokedBefore, nil
}

func lockedAssignmentDeviceTable(tx *gorm.DB) *gorm.DB {
	if strings.EqualFold(tx.Dialector.Name(), "sqlserver") {
		return tx.Table("gb_device WITH (UPDLOCK, HOLDLOCK)")
	}
	return tx.Table("gb_device").Clauses(clause.Locking{Strength: "UPDATE"})
}

func (s *Service) transferLocked(ctx context.Context, tx *gorm.DB, device assignmentDevice, targetDeptID uint) (*TransferReceipt, error) {
	if device.OwnerDeptID == targetDeptID {
		return nil, nil
	}
	accessEpoch, legacyRevokedBefore, err := loadAssignmentSecurity(tx, device.ID)
	if err != nil {
		return nil, err
	}
	device.AccessEpoch = accessEpoch
	device.LegacyRevokedBefore = legacyRevokedBefore
	if s.clock == nil || s.transferRecorder == nil || device.AccessEpoch == math.MaxInt64 {
		return nil, ErrAssignmentSecurityUnavailable
	}
	now := s.clock().UTC()
	if now.Unix() <= 0 || now.Unix() == math.MaxInt64 {
		return nil, ErrAssignmentSecurityUnavailable
	}
	newEpoch := device.AccessEpoch + 1
	cutoff := time.Unix(now.Unix()+1, 0).UTC()
	if device.LegacyRevokedBefore != nil && !cutoff.After(device.LegacyRevokedBefore.UTC()) {
		cutoff = device.LegacyRevokedBefore.UTC()
	}

	updated := tx.Table("gb_device").Where("id = ? AND deleted_at IS NULL AND owner_dept_id = ? AND access_epoch = ?", device.ID, device.OwnerDeptID, device.AccessEpoch).
		Updates(map[string]any{
			"owner_dept_id":         targetDeptID,
			"access_epoch":          newEpoch,
			"legacy_revoked_before": cutoff,
		})
	if updated.Error != nil {
		return nil, ErrAssignmentSecurityUnavailable
	}
	if updated.RowsAffected != 1 {
		return nil, ErrAssignmentSecurityUnavailable
	}
	if err := playauth.CancelReservedDeviceOperationIntents(ctx, tx, int64(device.ID), device.DeviceCode, newEpoch); err != nil {
		return nil, ErrAssignmentSecurityUnavailable
	}
	if err := cascadeAssignment(tx, device, targetDeptID); err != nil {
		return nil, err
	}
	if err := s.transferRecorder.RecordDeviceTransfer(ctx, tx, device.DeviceCode, newEpoch); err != nil {
		return nil, err
	}
	return &TransferReceipt{
		DevicePK:            device.ID,
		DeviceCode:          device.DeviceCode,
		OldOwnerDeptID:      device.OwnerDeptID,
		NewOwnerDeptID:      targetDeptID,
		OldEpoch:            device.AccessEpoch,
		NewEpoch:            newEpoch,
		LegacyRevokedBefore: cutoff,
	}, nil
}

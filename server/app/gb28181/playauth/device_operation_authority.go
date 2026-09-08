package playauth

import (
	"context"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

type deviceIntentAuthority interface {
	GenerationID() string
	CheckTx(*gorm.DB) error
	RequireRetiredTx(*gorm.DB, string) error
}

// NewAuthorizedDeviceOperationIntentStore receives the root's single authority.
// Construction is not dispatch permission: each effect must fence its own CAS.
func NewAuthorizedDeviceOperationIntentStore(db *gorm.DB, authority *processauthority.Authority) (*DeviceOperationIntentStore, error) {
	s := newDeviceOperationIntentStore(db, authority)
	if db == nil || !s.hasAuthority() {
		return nil, ErrDeviceIntentUnavailable
	}
	return s, nil
}

func newDeviceOperationIntentStore(db *gorm.DB, authority deviceIntentAuthority) *DeviceOperationIntentStore {
	return &DeviceOperationIntentStore{db: db, authority: authority}
}

func (s *DeviceOperationIntentStore) hasAuthority() bool {
	return s != nil && validDeviceProcessAuthority(s.authority)
}

func validDeviceProcessAuthority(authority deviceIntentAuthority) bool {
	if isNilInterface(authority) {
		return false
	}
	id, err := sipCleanupProcessID()
	return err == nil && validIntentID(id) && id != "00000000000000000000000000000000" && authority.GenerationID() == id
}

func (s *DeviceOperationIntentStore) checkAuthorityTx(tx *gorm.DB) error {
	if !s.hasAuthority() || s.authority.CheckTx(tx) != nil {
		return ErrDeviceIntentUnavailable
	}
	return nil
}

// The current fence must already be held in this transaction. Foreign IDs are
// evidence only when the same database/domain ledger proves them retired.
func (s *DeviceOperationIntentStore) requireCleanupGenerationTx(tx *gorm.DB, generation string) error {
	if !s.hasAuthority() || !validIntentID(generation) || generation == "00000000000000000000000000000000" {
		return ErrDeviceIntentUnavailable
	}
	if generation != s.authority.GenerationID() && s.authority.RequireRetiredTx(tx, generation) != nil {
		return ErrDeviceIntentUnavailable
	}
	return nil
}

// Wrap only effect checks, never late-observation paths. The authority row is
// acquired before the device/intent rows in the same transaction.
func (s *DeviceOperationIntentStore) effectDeviceCheck(check func(*gorm.DB, context.Context, DeviceOperationIntentIdentity) error) func(*gorm.DB, context.Context, DeviceOperationIntentIdentity) error {
	return func(tx *gorm.DB, ctx context.Context, id DeviceOperationIntentIdentity) error {
		if err := s.checkAuthorityTx(tx); err != nil {
			return err
		}
		return check(tx, ctx, id)
	}
}

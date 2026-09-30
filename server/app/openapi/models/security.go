package models

import "time"

// SecurityState is the singleton, durable security boundary for OpenAPI media
// authorization. The row is created by the migration/initialization path;
// application code may only observe it or latch it from unlocked to locked.
//
// The database constraints intentionally encode both the singleton identity
// and the one-way state shape. A missing row is not equivalent to an unlocked
// row: callers must fail closed when it cannot be read.
type SecurityState struct {
	ID             int64      `gorm:"column:id;primaryKey;check:ck_openapi_security_singleton,id = 1"`
	MustAuthLocked bool       `gorm:"column:must_auth_locked;not null;default:false;check:ck_openapi_security_state,(must_auth_locked = false AND lock_version = 0 AND locked_at IS NULL) OR (must_auth_locked = true AND lock_version > 0 AND locked_at IS NOT NULL)"`
	LockedAt       *time.Time `gorm:"column:locked_at"`
	LockVersion    int64      `gorm:"column:lock_version;not null;default:0"`
}

func (SecurityState) TableName() string { return "sys_openapi_security_state" }

// MediaNodeSecurity is the narrow runtime-identity projection for meta_node.
// It is intentionally kept beside the security state because runtime probes
// update only these columns and must never Save a complete media-node row.
type MediaNodeSecurity struct {
	ID                       int64      `gorm:"column:id;primaryKey"`
	CurrentBootNonce         *string    `gorm:"column:current_boot_nonce;type:char(32)"`
	RetiredBootHistory       *string    `gorm:"column:retired_boot_history;type:text"`
	RuntimeEpoch             int64      `gorm:"column:runtime_epoch;not null;default:0"`
	RuntimeProtocolVersion   int64      `gorm:"column:runtime_protocol_version;not null;default:0"`
	RuntimeConfirmedRevision int64      `gorm:"column:runtime_confirmed_revision;not null;default:0"`
	RuntimeConfirmedAt       *time.Time `gorm:"column:runtime_confirmed_at"`
	RuntimeIdentityStatus    string     `gorm:"column:runtime_identity_status;size:16;not null;default:unknown"`
}

func (MediaNodeSecurity) TableName() string { return "meta_node" }

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
	ID             int64      `gorm:"column:id;primaryKey;check:ck_sys_openapi_security_state_id,id = 1"`
	MustAuthLocked bool       `gorm:"column:must_auth_locked;not null;default:false;check:ck_sys_openapi_security_state_shape,(must_auth_locked = false AND lock_version = 0 AND locked_at IS NULL) OR (must_auth_locked = true AND lock_version > 0 AND locked_at IS NOT NULL)"`
	LockedAt       *time.Time `gorm:"column:locked_at"`
	LockVersion    int64      `gorm:"column:lock_version;not null;default:0"`
}

func (SecurityState) TableName() string { return "sys_openapi_security_state" }

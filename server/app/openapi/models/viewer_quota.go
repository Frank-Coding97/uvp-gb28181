package models

import "time"

type GrantState string

const (
	GrantStatePending GrantState = "pending"
	GrantStateIssued  GrantState = "issued"
	GrantStateBound   GrantState = "bound"
	GrantStateRevoked GrantState = "revoked"
	GrantStateExpired GrantState = "expired"
	GrantStateFailed  GrantState = "failed"
)

type ViewerState string

const (
	ViewerStatePending       ViewerState = "pending"
	ViewerStateActive        ViewerState = "active"
	ViewerStateRevokePending ViewerState = "revoke_pending"
	ViewerStateClosed        ViewerState = "closed"
)

// PlayGrant is an internal quota lease for the unified OpenAPI live endpoint.
// It does not correspond to a public grant or authorization API.
type PlayGrant struct {
	GrantID  string   `gorm:"column:grant_id;type:char(36);primaryKey"`
	ClientID int64    `gorm:"column:client_id;not null;index:idx_openapi_grant_client_state,priority:1"`
	Scope    string   `gorm:"column:scope;size:64;not null"`
	Viewers  []Viewer `gorm:"foreignKey:GrantID;references:GrantID;constraint:OnDelete:RESTRICT"`

	DeviceID        *string `gorm:"column:device_id;size:20"`
	ChannelID       *string `gorm:"column:channel_id;size:20"`
	ClientEpoch     int64   `gorm:"column:client_epoch;not null;default:1;check:ck_openapi_grant_epochs,client_epoch > 0 AND scope_epoch > 0 AND device_epoch > 0"`
	ScopeEpoch      int64   `gorm:"column:scope_epoch;not null;default:1"`
	DeviceEpoch     int64   `gorm:"column:device_epoch;not null;default:1"`
	NodeUUID        *string `gorm:"column:node_uuid;size:64"`
	BootNonce       *string `gorm:"column:boot_nonce;type:char(32)"`
	Schema          *string `gorm:"column:schema;size:32"`
	VHost           *string `gorm:"column:vhost;size:128"`
	App             *string `gorm:"column:app;size:64"`
	Stream          *string `gorm:"column:stream;size:255"`
	MediaGeneration *uint64 `gorm:"column:media_generation"`
	Protocol        *string `gorm:"column:protocol;size:16"`

	IssuedAt  time.Time  `gorm:"column:issued_at;not null;index:idx_openapi_grant_expires"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null;index:idx_openapi_grant_expires"`
	State     GrantState `gorm:"column:state;size:16;not null;default:pending;index:idx_openapi_grant_client_state,priority:2;check:ck_openapi_grant_binding,state NOT IN ('issued','bound') OR (device_id IS NOT NULL AND device_id <> '' AND channel_id IS NOT NULL AND channel_id <> '' AND node_uuid IS NOT NULL AND node_uuid <> '' AND boot_nonce IS NOT NULL AND boot_nonce <> '' AND length(boot_nonce) = 32 AND schema IS NOT NULL AND schema <> '' AND vhost IS NOT NULL AND vhost <> '' AND app IS NOT NULL AND app <> '' AND stream IS NOT NULL AND stream <> '' AND media_generation IS NOT NULL AND media_generation > 0 AND protocol IS NOT NULL AND protocol <> '')"`
	Reason    string     `gorm:"column:reason;size:64;not null;default:'';check:ck_openapi_grant_state,state IN ('pending','issued','bound','revoked','expired','failed')"`
	CreatedAt time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt time.Time  `gorm:"column:updated_at;not null"`
}

func (PlayGrant) TableName() string { return "gb_openapi_play_grant" }

type Viewer struct {
	ID              int64       `gorm:"column:id;primaryKey;autoIncrement"`
	GrantID         string      `gorm:"column:grant_id;type:char(36);not null;uniqueIndex:uk_openapi_viewer_grant"`
	NodeUUID        string      `gorm:"column:node_uuid;size:64;not null;uniqueIndex:uk_openapi_viewer_identity,priority:1;check:ck_openapi_viewer_identity,node_uuid <> '' AND boot_nonce <> '' AND length(boot_nonce) = 32 AND identifier <> ''"`
	BootNonce       string      `gorm:"column:boot_nonce;type:char(32);not null;uniqueIndex:uk_openapi_viewer_identity,priority:2"`
	Identifier      string      `gorm:"column:identifier;size:128;not null;uniqueIndex:uk_openapi_viewer_identity,priority:3"`
	Schema          string      `gorm:"column:schema;size:32;not null;check:ck_openapi_viewer_media_binding,schema <> '' AND vhost <> '' AND app <> '' AND stream <> ''"`
	VHost           string      `gorm:"column:vhost;size:128;not null"`
	App             string      `gorm:"column:app;size:64;not null"`
	Stream          string      `gorm:"column:stream;size:255;not null"`
	MediaGeneration uint64      `gorm:"column:media_generation;not null;check:ck_openapi_viewer_media_generation,media_generation > 0"`
	State           ViewerState `gorm:"column:state;size:16;not null;default:pending;index:idx_openapi_viewer_state_retry,priority:1;check:ck_openapi_viewer_state,state IN ('pending','active','revoke_pending','closed')"`
	LastSeenAt      *time.Time  `gorm:"column:last_seen_at"`
	RetryAt         *time.Time  `gorm:"column:retry_at;index:idx_openapi_viewer_state_retry,priority:2"`
	Attempts        int         `gorm:"column:attempts;not null;default:0;check:ck_openapi_viewer_attempts,attempts >= 0"`
	LastErrorClass  string      `gorm:"column:last_error_class;size:64;not null;default:''"`
	CreatedAt       time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time   `gorm:"column:updated_at;not null"`
}

func (Viewer) TableName() string { return "gb_openapi_viewer" }

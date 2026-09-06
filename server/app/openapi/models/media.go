package models

import "time"

// GrantState is the durable lifecycle of one external playback authorization.
// A pending grant may exist before a media session has been identified. Once
// it becomes issued or bound, the complete media binding is required by the
// database check constraint as well as by the service layer.
type GrantState string

const (
	GrantStatePending GrantState = "pending"
	GrantStateIssued  GrantState = "issued"
	GrantStateBound   GrantState = "bound"
	GrantStateRevoked GrantState = "revoked"
	GrantStateExpired GrantState = "expired"
	GrantStateFailed  GrantState = "failed"
)

// ViewerState records the state of one real media session. Pending is used
// after a trusted Hook identifies the session and before admission is closed;
// it is not a placeholder for an unknown identifier.
type ViewerState string

const (
	ViewerStatePending       ViewerState = "pending"
	ViewerStateActive        ViewerState = "active"
	ViewerStateRevokePending ViewerState = "revoke_pending"
	ViewerStateClosed        ViewerState = "closed"
)

// PlayGrant is intentionally independent from the existing personnel JWT and
// media v2 token models. Nullable media fields allow a pending grant to wait
// for a real media session; issued and bound rows must carry every field in
// the binding tuple.
type PlayGrant struct {
	GrantID  string   `gorm:"column:grant_id;type:char(36);primaryKey" json:"grantId"`
	ClientID int64    `gorm:"column:client_id;not null;index:idx_openapi_grant_client_state,priority:1" json:"clientId"`
	Scope    string   `gorm:"column:scope;size:64;not null" json:"scope"`
	Viewers  []Viewer `gorm:"foreignKey:GrantID;references:GrantID;constraint:OnDelete:RESTRICT" json:"-"`

	DeviceID        *string `gorm:"column:device_id;size:20" json:"deviceId,omitempty"`
	ChannelID       *string `gorm:"column:channel_id;size:20" json:"channelId,omitempty"`
	ClientEpoch     int64   `gorm:"column:client_epoch;not null;default:1;check:ck_openapi_grant_epochs,client_epoch > 0 AND scope_epoch > 0 AND device_epoch > 0" json:"clientEpoch"`
	ScopeEpoch      int64   `gorm:"column:scope_epoch;not null;default:1" json:"scopeEpoch"`
	DeviceEpoch     int64   `gorm:"column:device_epoch;not null;default:1" json:"deviceEpoch"`
	NodeUUID        *string `gorm:"column:node_uuid;size:64" json:"nodeUuid,omitempty"`
	BootNonce       *string `gorm:"column:boot_nonce;type:char(32)" json:"bootNonce,omitempty"`
	Schema          *string `gorm:"column:schema;size:32" json:"schema,omitempty"`
	VHost           *string `gorm:"column:vhost;size:128" json:"vhost,omitempty"`
	App             *string `gorm:"column:app;size:64" json:"app,omitempty"`
	Stream          *string `gorm:"column:stream;size:255" json:"stream,omitempty"`
	MediaGeneration *uint64 `gorm:"column:media_generation" json:"mediaGeneration,omitempty"`
	Protocol        *string `gorm:"column:protocol;size:16" json:"protocol,omitempty"`

	IssuedAt  time.Time  `gorm:"column:issued_at;not null;index:idx_openapi_grant_expires" json:"issuedAt"`
	ExpiresAt time.Time  `gorm:"column:expires_at;not null;index:idx_openapi_grant_expires" json:"expiresAt"`
	State     GrantState `gorm:"column:state;size:16;not null;default:pending;index:idx_openapi_grant_client_state,priority:2;check:ck_openapi_grant_binding,state NOT IN ('issued','bound') OR (device_id IS NOT NULL AND device_id <> '' AND channel_id IS NOT NULL AND channel_id <> '' AND node_uuid IS NOT NULL AND node_uuid <> '' AND boot_nonce IS NOT NULL AND boot_nonce <> '' AND length(boot_nonce) = 32 AND schema IS NOT NULL AND schema <> '' AND vhost IS NOT NULL AND vhost <> '' AND app IS NOT NULL AND app <> '' AND stream IS NOT NULL AND stream <> '' AND media_generation IS NOT NULL AND media_generation > 0 AND protocol IS NOT NULL AND protocol <> '')" json:"state"`
	Reason    string     `gorm:"column:reason;size:64;not null;default:'';check:ck_openapi_grant_state,state IN ('pending','issued','bound','revoked','expired','failed')" json:"reason,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt time.Time  `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (PlayGrant) TableName() string { return "gb_openapi_play_grant" }

// Viewer is keyed by the exact identity emitted by one trusted media process.
// The tuple is deliberately non-null and has no filtered/NULL unique index:
// an unknown session is never persisted as a viewer row.
type Viewer struct {
	ID              int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	GrantID         string      `gorm:"column:grant_id;type:char(36);not null;uniqueIndex:uk_openapi_viewer_grant" json:"grantId"`
	NodeUUID        string      `gorm:"column:node_uuid;size:64;not null;uniqueIndex:uk_openapi_viewer_identity,priority:1;check:ck_openapi_viewer_identity,node_uuid <> '' AND boot_nonce <> '' AND length(boot_nonce) = 32 AND identifier <> ''" json:"nodeUuid"`
	BootNonce       string      `gorm:"column:boot_nonce;type:char(32);not null;uniqueIndex:uk_openapi_viewer_identity,priority:2" json:"bootNonce"`
	Identifier      string      `gorm:"column:identifier;size:128;not null;uniqueIndex:uk_openapi_viewer_identity,priority:3" json:"identifier"`
	Schema          string      `gorm:"column:schema;size:32;not null;check:ck_openapi_viewer_media_binding,schema <> '' AND vhost <> '' AND app <> '' AND stream <> ''" json:"schema"`
	VHost           string      `gorm:"column:vhost;size:128;not null" json:"vhost"`
	App             string      `gorm:"column:app;size:64;not null" json:"app"`
	Stream          string      `gorm:"column:stream;size:255;not null" json:"stream"`
	MediaGeneration uint64      `gorm:"column:media_generation;not null;check:ck_openapi_viewer_media_generation,media_generation > 0" json:"mediaGeneration"`
	State           ViewerState `gorm:"column:state;size:16;not null;default:pending;index:idx_openapi_viewer_state_retry,priority:1;check:ck_openapi_viewer_state,state IN ('pending','active','revoke_pending','closed')" json:"state"`
	LastSeenAt      *time.Time  `gorm:"column:last_seen_at" json:"lastSeenAt,omitempty"`
	RetryAt         *time.Time  `gorm:"column:retry_at;index:idx_openapi_viewer_state_retry,priority:2" json:"retryAt,omitempty"`
	Attempts        int         `gorm:"column:attempts;not null;default:0;check:ck_openapi_viewer_attempts,attempts >= 0" json:"attempts"`
	LastErrorClass  string      `gorm:"column:last_error_class;size:64;not null;default:''" json:"lastErrorClass,omitempty"`
	CreatedAt       time.Time   `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt       time.Time   `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (Viewer) TableName() string { return "gb_openapi_viewer" }

// DeviceSecurity is a same-table projection for the two security fields that
// later device-assignment code must update atomically. It must not be used as
// a full GbDevice replacement or passed to a broad Save operation.
type DeviceSecurity struct {
	ID          uint  `gorm:"column:id;primaryKey"`
	AccessEpoch int64 `gorm:"column:access_epoch;not null;default:1;check:ck_gb_device_access_epoch,access_epoch > 0"`
	// LegacyRevokedBefore is stored as a UTC whole-second boundary because the
	// legacy v2 iat is an integer Unix second. Callers must convert with
	// time.Unix(iat, 0).UTC(); the revocation contract must reject tokens issued
	// in the same second. Comparison and assignment details remain in T13.
	LegacyRevokedBefore *time.Time `gorm:"column:legacy_revoked_before"`
}

func (DeviceSecurity) TableName() string { return "gb_device" }

// MediaNodeSecurity is the corresponding narrow projection for meta_node.
// It stores observed runtime identity only; it does not alter tags_json or
// claim protocol support before a trusted probe confirms it.
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

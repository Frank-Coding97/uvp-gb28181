package model

import (
	"time"

	"gorm.io/gorm"
)

type CascadeProfileOverride string

const (
	CascadeProfileOverrideAuto CascadeProfileOverride = "auto"
	CascadeProfileOverride2016 CascadeProfileOverride = "2016"
	CascadeProfileOverride2022 CascadeProfileOverride = "2022"
)

type CascadeMediaSessionState string

const (
	CascadeMediaSessionStateReceived     CascadeMediaSessionState = "received"
	CascadeMediaSessionStateProvisioning CascadeMediaSessionState = "provisioning"
	CascadeMediaSessionStateAnswered     CascadeMediaSessionState = "answered"
	CascadeMediaSessionStateActive       CascadeMediaSessionState = "active"
	CascadeMediaSessionStateClosing      CascadeMediaSessionState = "closing"
	CascadeMediaSessionStateClosed       CascadeMediaSessionState = "closed"
	CascadeMediaSessionStateFailed       CascadeMediaSessionState = "failed"
	CascadeMediaSessionStateCancelled    CascadeMediaSessionState = "cancelled"
	CascadeMediaSessionStateAborted      CascadeMediaSessionState = "aborted"
)

// GbCascadePlatform is an upstream-specific SIP identity and its runtime facts.
// Secret fields persist a securestore.Envelope and are never plaintext credentials.
type GbCascadePlatform struct {
	ID               uint64 `gorm:"primaryKey" json:"id"`
	Name             string `gorm:"column:name;size:128;not null;uniqueIndex:uk_cascade_platform_name" json:"name"`
	UpstreamServerID string `gorm:"column:upstream_server_id;size:20;not null;uniqueIndex:uk_cascade_platform_connection,priority:3" json:"upstreamServerId"`
	UpstreamDomain   string `gorm:"column:upstream_domain;size:255;not null" json:"upstreamDomain"`
	Host             string `gorm:"column:host;size:255;not null;uniqueIndex:uk_cascade_platform_connection,priority:4" json:"host"`
	Port             int    `gorm:"column:port;not null;uniqueIndex:uk_cascade_platform_connection,priority:5" json:"port"`

	LocalDeviceID string `gorm:"column:local_device_id;size:20;not null;uniqueIndex:uk_cascade_platform_connection,priority:1" json:"localDeviceId"`
	LocalDomain   string `gorm:"column:local_domain;size:255;not null;uniqueIndex:uk_cascade_platform_connection,priority:2" json:"localDomain"`
	LocalSIPIP    string `gorm:"column:local_sip_ip;size:45;not null" json:"localSipIp"`
	LocalSIPPort  int    `gorm:"column:local_sip_port;not null" json:"localSipPort"`

	MediaAdvertiseIP string `gorm:"column:media_advertise_ip;size:45" json:"mediaAdvertiseIp"`
	AuthUsername     string `gorm:"column:auth_username;size:255" json:"authUsername"`
	SecretNonce      []byte `gorm:"column:secret_nonce;type:blob" json:"-"`
	SecretCiphertext []byte `gorm:"column:secret_ciphertext;type:blob" json:"-"`
	SecretAlg        string `gorm:"column:secret_alg;size:32" json:"-"`
	SecretKeyVersion string `gorm:"column:secret_key_version;size:64" json:"-"`

	ProfileOverride      CascadeProfileOverride `gorm:"column:profile_override;size:16;not null;default:auto" json:"profileOverride"`
	ReportedGBVersion    string                 `gorm:"column:reported_gb_version;size:16" json:"reportedGbVersion"`
	ReportedGBVersionAt  *time.Time             `gorm:"column:reported_gb_version_at" json:"reportedGbVersionAt"`
	EffectiveVersion     string                 `gorm:"column:effective_version;size:16;not null;default:2016" json:"effectiveVersion"`
	EffectiveVersionFrom string                 `gorm:"column:effective_version_source;size:32;not null;default:default" json:"effectiveVersionSource"`
	EffectiveVersionAt   *time.Time             `gorm:"column:effective_version_at" json:"effectiveVersionAt"`
	CharsetOverride      string                 `gorm:"column:charset_override;size:32" json:"charsetOverride"`

	RegisterExpires   int    `gorm:"column:register_expires;not null;default:3600" json:"registerExpires"`
	KeepaliveInterval int    `gorm:"column:keepalive_interval;not null;default:60" json:"keepaliveInterval"`
	RetryPolicy       string `gorm:"column:retry_policy;type:text" json:"retryPolicy"`
	Transport         string `gorm:"column:transport;size:16;not null;default:UDP;uniqueIndex:uk_cascade_platform_connection,priority:6" json:"transport"`

	CatalogBatchSize int  `gorm:"column:catalog_batch_size;not null;default:100" json:"catalogBatchSize"`
	PublishPlatform  bool `gorm:"column:publish_platform;not null;default:false" json:"publishPlatform"`
	PublishCivil     bool `gorm:"column:publish_civil;not null;default:false" json:"publishCivil"`
	PublishGroup     bool `gorm:"column:publish_group;not null;default:false" json:"publishGroup"`
	MaxStreams       int  `gorm:"column:max_streams;not null;default:1" json:"maxStreams"`
	PTZEnabled       bool `gorm:"column:ptz_enabled;not null;default:false" json:"ptzEnabled"`

	RegisterAt        *time.Time `gorm:"column:register_at" json:"registerAt"`
	RegisterExpiresAt *time.Time `gorm:"column:register_expires_at" json:"registerExpiresAt"`
	HeartbeatAt       *time.Time `gorm:"column:heartbeat_at" json:"heartbeatAt"`
	LastErrorCode     string     `gorm:"column:last_error_code;size:64" json:"lastErrorCode"`
	LastErrorMessage  string     `gorm:"column:last_error_message;type:text" json:"lastErrorMessage"`
	LastErrorAt       *time.Time `gorm:"column:last_error_at" json:"lastErrorAt"`

	Enabled            bool           `gorm:"column:enabled;not null;default:false;index:idx_cascade_platform_enabled" json:"enabled"`
	ConfigRevision     uint64         `gorm:"column:config_revision;not null;default:1" json:"configRevision"`
	ProjectionRevision uint64         `gorm:"column:projection_revision;not null;default:0" json:"projectionRevision"`
	CreatedAt          time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at;index:idx_cascade_platform_deleted_at" json:"-"`
}

func (GbCascadePlatform) TableName() string { return "gb_cascade_platform" }

// GbCascadeDeviceProjection maps one source device to the identifier visible to an upstream platform.
type GbCascadeDeviceProjection struct {
	ID                uint64         `gorm:"primaryKey" json:"id"`
	PlatformID        uint64         `gorm:"column:platform_id;not null;uniqueIndex:uk_cascade_device_source,priority:1;uniqueIndex:uk_cascade_device_published,priority:1;index:idx_cascade_device_platform_active,priority:1" json:"platformId"`
	SourceDeviceID    uint64         `gorm:"column:source_device_id;not null;uniqueIndex:uk_cascade_device_source,priority:2" json:"sourceDeviceId"`
	PublishedDeviceID string         `gorm:"column:published_device_id;size:20;not null;uniqueIndex:uk_cascade_device_published,priority:2" json:"publishedDeviceId"`
	Name              string         `gorm:"column:name;size:255" json:"name"`
	Manufacturer      string         `gorm:"column:manufacturer;size:255" json:"manufacturer"`
	Model             string         `gorm:"column:model;size:255" json:"model"`
	Owner             string         `gorm:"column:owner;size:255" json:"owner"`
	CivilCode         string         `gorm:"column:civil_code;size:32" json:"civilCode"`
	Address           string         `gorm:"column:address;size:255" json:"address"`
	Parental          int            `gorm:"column:parental;not null;default:0" json:"parental"`
	Secrecy           int            `gorm:"column:secrecy;not null;default:0" json:"secrecy"`
	Active            bool           `gorm:"column:active;not null;default:true;index:idx_cascade_device_platform_active,priority:2" json:"active"`
	Revision          uint64         `gorm:"column:revision;not null;default:1" json:"revision"`
	CreatedAt         time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt         gorm.DeletedAt `gorm:"column:deleted_at;index:idx_cascade_device_deleted_at" json:"-"`
}

func (GbCascadeDeviceProjection) TableName() string { return "gb_cascade_device_projection" }

// GbCascadeChannelProjection is the authorization source of truth for a published channel.
type GbCascadeChannelProjection struct {
	ID                 uint64         `gorm:"primaryKey" json:"id"`
	PlatformID         uint64         `gorm:"column:platform_id;not null;uniqueIndex:uk_cascade_channel_source,priority:1;uniqueIndex:uk_cascade_channel_published,priority:1;index:idx_cascade_channel_platform_active,priority:1" json:"platformId"`
	DeviceProjectionID uint64         `gorm:"column:device_projection_id;not null;index:idx_cascade_channel_device_projection" json:"deviceProjectionId"`
	SourceChannelID    uint64         `gorm:"column:source_channel_id;not null;uniqueIndex:uk_cascade_channel_source,priority:2" json:"sourceChannelId"`
	PublishedChannelID string         `gorm:"column:published_channel_id;size:20;not null;uniqueIndex:uk_cascade_channel_published,priority:2" json:"publishedChannelId"`
	Name               string         `gorm:"column:name;size:255" json:"name"`
	ParentOverride     string         `gorm:"column:parent_override;size:20" json:"parentOverride"`
	PTZAllowed         bool           `gorm:"column:ptz_allowed;not null;default:false" json:"ptzAllowed"`
	Active             bool           `gorm:"column:active;not null;default:true;index:idx_cascade_channel_platform_active,priority:2" json:"active"`
	Revision           uint64         `gorm:"column:revision;not null;default:1" json:"revision"`
	CreatedAt          time.Time      `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time      `gorm:"column:updated_at;not null" json:"updatedAt"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at;index:idx_cascade_channel_deleted_at" json:"-"`
}

func (GbCascadeChannelProjection) TableName() string { return "gb_cascade_channel_projection" }

// GbCascadeMediaSession persists enough facts for audit and restart cleanup, never dialog recovery.
type GbCascadeMediaSession struct {
	ID                 uint64                   `gorm:"primaryKey" json:"id"`
	PlatformID         uint64                   `gorm:"column:platform_id;not null;index:idx_cascade_media_platform_state,priority:1" json:"platformId"`
	DialogKey          string                   `gorm:"column:dialog_key;size:512;not null;uniqueIndex:uk_cascade_media_dialog" json:"dialogKey"`
	CallID             string                   `gorm:"column:call_id;size:255;not null;index:idx_cascade_media_call_id" json:"callId"`
	LocalTag           string                   `gorm:"column:local_tag;size:255" json:"localTag"`
	RemoteTag          string                   `gorm:"column:remote_tag;size:255" json:"remoteTag"`
	CSeq               uint64                   `gorm:"column:cseq;not null;default:0" json:"cseq"`
	SourceDeviceID     uint64                   `gorm:"column:source_device_id;not null" json:"sourceDeviceId"`
	SourceChannelID    uint64                   `gorm:"column:source_channel_id;not null" json:"sourceChannelId"`
	PublishedChannelID string                   `gorm:"column:published_channel_id;size:20;not null" json:"publishedChannelId"`
	ProfileVersion     string                   `gorm:"column:profile_version;size:16" json:"profileVersion"`
	ProfileCharset     string                   `gorm:"column:profile_charset;size:32" json:"profileCharset"`
	SDPSummary         string                   `gorm:"column:sdp_summary;type:text" json:"-"`
	ZLMNodeID          int64                    `gorm:"column:zlm_node_id;not null;default:0" json:"zlmNodeId"`
	ZLMVHost           string                   `gorm:"column:zlm_vhost;size:128" json:"zlmVhost"`
	ZLMApp             string                   `gorm:"column:zlm_app;size:64" json:"zlmApp"`
	ZLMStream          string                   `gorm:"column:zlm_stream;size:128" json:"zlmStream"`
	SenderSSRC         string                   `gorm:"column:sender_ssrc;size:32" json:"senderSsrc"`
	Transport          string                   `gorm:"column:transport;size:16" json:"transport"`
	RemoteIP           string                   `gorm:"column:remote_ip;size:45" json:"remoteIp"`
	RemotePort         int                      `gorm:"column:remote_port;not null;default:0" json:"remotePort"`
	State              CascadeMediaSessionState `gorm:"column:state;size:16;not null;default:received;index:idx_cascade_media_platform_state,priority:2" json:"state"`
	FailureCode        string                   `gorm:"column:failure_code;size:64" json:"failureCode"`
	FailureMessage     string                   `gorm:"column:failure_message;type:text" json:"failureMessage"`
	ReceivedAt         *time.Time               `gorm:"column:received_at" json:"receivedAt"`
	AnsweredAt         *time.Time               `gorm:"column:answered_at" json:"answeredAt"`
	ActiveAt           *time.Time               `gorm:"column:active_at" json:"activeAt"`
	ClosedAt           *time.Time               `gorm:"column:closed_at" json:"closedAt"`
	CreatedAt          time.Time                `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time                `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbCascadeMediaSession) TableName() string { return "gb_cascade_media_session" }

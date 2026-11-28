package models

import "time"

// GbChannelVideoShare stores one share lifecycle for a real channel row.
// Public credentials are derived from the persisted identifiers and signing key.
type GbChannelVideoShare struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	ChannelPK      uint       `gorm:"column:channel_pk;not null;uniqueIndex:uk_video_share_channel" json:"channelPk"`
	PublicID       string     `gorm:"column:public_id;size:64;not null;uniqueIndex:uk_video_share_public_id" json:"publicId"`
	Generation     string     `gorm:"column:generation;size:64;not null" json:"generation"`
	Enabled        bool       `gorm:"column:enabled;not null;default:false;index:idx_video_share_enabled_expiry" json:"enabled"`
	ActivatedAt    *time.Time `gorm:"column:activated_at" json:"activatedAt,omitempty"`
	ExpiresAt      *time.Time `gorm:"column:expires_at;index:idx_video_share_enabled_expiry" json:"expiresAt,omitempty"`
	CreatedBy      *uint      `gorm:"column:created_by;index" json:"createdBy,omitempty"`
	DevicePK       uint       `gorm:"column:device_pk;not null;index" json:"devicePk"`
	DeviceID       string     `gorm:"column:device_id;size:20;not null" json:"deviceId"`
	DeviceEpoch    uint64     `gorm:"column:device_epoch;not null;default:0" json:"deviceEpoch"`
	Source         string     `gorm:"column:source;size:16;not null" json:"source"`
	PolicyActorID  *uint      `gorm:"column:policy_actor_id" json:"policyActorId,omitempty"`
	PolicyRevision uint64     `gorm:"column:policy_revision;not null;default:0" json:"policyRevision"`
	KeyID          string     `gorm:"column:key_id;size:64;not null" json:"keyId"`
	Revision       uint64     `gorm:"column:revision;not null;default:1" json:"revision"`
	RevokedAt      *time.Time `gorm:"column:revoked_at" json:"revokedAt,omitempty"`
	RevokeReason   string     `gorm:"column:revoke_reason;size:64;not null;default:''" json:"revokeReason"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (GbChannelVideoShare) TableName() string { return "gb_channel_video_share" }

// GbDeviceVideoSharePolicy snapshots the global default when a device first
// registers successfully. Catalog discovery must not consult the live global
// switch because that would retroactively enable older devices.
type GbDeviceVideoSharePolicy struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	DevicePK       uint      `gorm:"column:device_pk;not null;uniqueIndex:uk_video_share_device_policy" json:"devicePk"`
	DefaultEnabled bool      `gorm:"column:default_enabled;not null;default:false" json:"defaultEnabled"`
	Source         string    `gorm:"column:source;size:16;not null" json:"source"`
	PolicyActorID  *uint     `gorm:"column:policy_actor_id" json:"policyActorId,omitempty"`
	PolicyRevision uint64    `gorm:"column:policy_revision;not null;default:0" json:"policyRevision"`
	InitializedAt  time.Time `gorm:"column:initialized_at;not null" json:"initializedAt"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (GbDeviceVideoSharePolicy) TableName() string { return "gb_device_video_share_policy" }

package models

import "time"

// SysUserSession records one independently revocable backend login session.
type SysUserSession struct {
	SID              string     `gorm:"column:sid;size:36;primaryKey" json:"sid"`
	UserID           uint       `gorm:"column:user_id;not null;index:idx_user_id" json:"userId"`
	RefreshTokenHash *string    `gorm:"column:refresh_token_hash;size:64" json:"-"`
	RefreshJTI       *string    `gorm:"column:refresh_jti;size:36" json:"-"`
	ClientIP         string     `gorm:"column:client_ip;size:50;not null" json:"clientIp"`
	LoginLocation    string     `gorm:"column:login_location;size:100;not null" json:"loginLocation"`
	UserAgent        string     `gorm:"column:user_agent;size:500;not null" json:"userAgent"`
	Browser          string     `gorm:"column:browser;size:100;not null" json:"browser"`
	OS               string     `gorm:"column:os;size:100;not null" json:"os"`
	LoginAt          time.Time  `gorm:"column:login_at;not null" json:"loginAt"`
	LastActiveAt     time.Time  `gorm:"column:last_active_at;not null;index:idx_session_valid,priority:3" json:"lastActiveAt"`
	SessionExpiresAt time.Time  `gorm:"column:session_expires_at;not null;index:idx_session_valid,priority:2" json:"sessionExpiresAt"`
	RevokedAt        *time.Time `gorm:"column:revoked_at;index:idx_session_valid,priority:1" json:"revokedAt"`
	RevokeReason     string     `gorm:"column:revoke_reason;size:32" json:"revokeReason"`
	RevokedBy        *uint      `gorm:"column:revoked_by" json:"revokedBy"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt        time.Time  `gorm:"column:updated_at" json:"updatedAt"`
}

func (SysUserSession) TableName() string { return "sys_user_sessions" }

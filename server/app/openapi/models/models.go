// Package models contains the independent machine-client persistence model.
// It deliberately has no user-role or shared-device associations.
package models

import "time"

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"

	// DataScopeDepartment limits a client to its configured owner department.
	DataScopeDepartment int8 = 3
	// DataScopeDepartmentAndChildren includes the owner department and all of
	// its descendants in the current department tree.
	DataScopeDepartmentAndChildren int8 = 4
)

// NormalizeDataScope keeps rows created before the data-scope column existed
// compatible with the historical exact-owner behavior.
func NormalizeDataScope(dataScope int8) int8 {
	if dataScope == 0 {
		return DataScopeDepartment
	}
	return dataScope
}

func ValidDataScope(dataScope int8) bool {
	switch NormalizeDataScope(dataScope) {
	case DataScopeDepartment, DataScopeDepartmentAndChildren:
		return true
	default:
		return false
	}
}

// Client is not an HTTP response DTO. Verification material is additionally
// hidden from JSON so accidental diagnostic serialization cannot reveal it.
type Client struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AK                string    `gorm:"column:ak;size:36;not null;uniqueIndex:uk_openapi_ak" json:"ak"`
	Name              string    `gorm:"size:100;not null" json:"name"`
	OwnerDeptID       uint      `gorm:"column:owner_dept_id;not null;index:idx_openapi_client_dept" json:"ownerDeptId"`
	DataScope         int8      `gorm:"column:data_scope;not null;default:3" json:"dataScope"`
	ResponsibleUserID uint      `gorm:"column:responsible_user_id;not null;default:0" json:"responsibleUserId"`
	Status            string    `gorm:"size:16;not null;default:disabled" json:"status"`
	SecretCiphertext  []byte    `gorm:"column:secret_ciphertext;not null" json:"-"`
	SecretIV          []byte    `gorm:"column:secret_iv;not null" json:"-"`
	SecretKeyID       string    `gorm:"column:secret_key_id;size:64;not null" json:"-"`
	SecretVersion     int64     `gorm:"column:secret_version;not null;default:1" json:"-"`
	AuthEpoch         int64     `gorm:"column:auth_epoch;not null;default:1" json:"-"`
	RateLimit         int       `gorm:"column:rate_limit;not null;default:10" json:"rateLimit"`
	Burst             int       `gorm:"not null;default:20" json:"burst"`
	ViewerQuota       int       `gorm:"column:viewer_quota;not null;default:10" json:"viewerQuota"`
	RowVersion        int64     `gorm:"column:row_version;not null;default:1" json:"rowVersion"`
	CreatedBy         uint      `gorm:"not null" json:"createdBy"`
	UpdatedBy         uint      `gorm:"not null" json:"updatedBy"`
	CreatedAt         time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt         time.Time `gorm:"not null" json:"updatedAt"`
}

func (Client) TableName() string { return "sys_openapi_client" }

// Disabled scope rows are retained: regranting never reuses an old epoch.
type ClientScope struct {
	ClientID   int64     `gorm:"primaryKey;autoIncrement:false;column:client_id"`
	Scope      string    `gorm:"primaryKey;size:64"`
	Enabled    bool      `gorm:"not null;default:false"`
	ScopeEpoch int64     `gorm:"column:scope_epoch;not null;default:1"`
	UpdatedBy  uint      `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

func (ClientScope) TableName() string { return "sys_openapi_client_scope" }

// Nonce rows must be committed independently of later business transactions.
type Nonce struct {
	ClientID   int64     `gorm:"primaryKey;autoIncrement:false;column:client_id;uniqueIndex:uk_openapi_nonce,priority:1"`
	Value      string    `gorm:"primaryKey;column:nonce;size:32;uniqueIndex:uk_openapi_nonce,priority:2"`
	AcceptedAt time.Time `gorm:"column:accepted_at;not null"`
	ExpiresAt  time.Time `gorm:"column:expires_at;not null;index:idx_openapi_nonce_expiry"`
}

func (Nonce) TableName() string { return "sys_openapi_nonce" }

// Audit intentionally cannot store request bodies, response bodies or tokens.
type Audit struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	RequestID     string    `gorm:"column:request_id;size:64;not null;uniqueIndex:uk_openapi_audit_request"`
	ClientID      *int64    `gorm:"column:client_id;index:idx_openapi_audit_client_time,priority:1"`
	AKFingerprint string    `gorm:"column:ak_fingerprint;size:64;not null;default:''"`
	Scope         string    `gorm:"size:64;not null;default:''"`
	ResourceType  string    `gorm:"column:resource_type;size:32;not null;default:''"`
	ResourceID    string    `gorm:"column:resource_id;size:128;not null;default:''"`
	Result        string    `gorm:"size:24;not null"`
	ReasonClass   string    `gorm:"column:reason_class;size:64;not null;default:''"`
	Source        string    `gorm:"size:64;not null;default:''"`
	LatencyMS     int64     `gorm:"column:latency_ms;not null;default:0"`
	CreatedAt     time.Time `gorm:"not null;index:idx_openapi_audit_client_time,priority:2;index:idx_openapi_audit_time"`
	CompletedAt   *time.Time
}

func (Audit) TableName() string { return "sys_openapi_audit" }

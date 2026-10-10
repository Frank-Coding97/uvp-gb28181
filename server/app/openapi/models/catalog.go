package models

import "time"

const (
	CatalogStatusDraft    = "draft"
	CatalogStatusActive   = "active"
	CatalogStatusDisabled = "disabled"

	ReleaseStatusDraft      = "draft"
	ReleaseStatusPublished  = "published"
	ReleaseStatusSuperseded = "superseded"
	ReleaseStatusRolledBack = "rolled_back"

	RuntimeCatalogReady       = "ready"
	RuntimeCatalogUnavailable = "unavailable"
)

// CapabilityGroup is an independently managed business capability group.
// Code is retained after a soft delete so a published group identity cannot be
// silently reused by a later catalog edit.
type CapabilityGroup struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string     `gorm:"size:64;not null;uniqueIndex:uk_openapi_capability_group_code" json:"code"`
	Name        string     `gorm:"size:100;not null" json:"name"`
	Description string     `gorm:"size:500;not null;default:''" json:"description"`
	Sort        int        `gorm:"not null;default:0" json:"sort"`
	Status      string     `gorm:"size:16;not null;default:draft" json:"status"`
	RowVersion  int64      `gorm:"column:row_version;not null;default:1" json:"rowVersion"`
	CreatedBy   uint       `gorm:"not null;default:0" json:"createdBy"`
	UpdatedBy   uint       `gorm:"not null;default:0" json:"updatedBy"`
	CreatedAt   time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updatedAt"`
	DeletedAt   *time.Time `gorm:"column:deleted_at;index:idx_openapi_capability_group_deleted" json:"-"`
}

func (CapabilityGroup) TableName() string { return "sys_openapi_capability_group" }

// Capability owns a stable external scope. Scope is deliberately unique over
// all rows, including soft-deleted rows, so a published scope can never be
// reused for a different meaning.
type Capability struct {
	ID           int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID      int64      `gorm:"column:group_id;not null;index:idx_openapi_capability_group;uniqueIndex:uk_openapi_capability_group_capability_code,priority:1" json:"groupId"`
	Code         string     `gorm:"size:96;not null;uniqueIndex:uk_openapi_capability_group_capability_code,priority:2" json:"code"`
	Scope        string     `gorm:"size:96;not null;uniqueIndex:uk_openapi_capability_scope" json:"scope"`
	Name         string     `gorm:"size:160;not null" json:"name"`
	Description  string     `gorm:"size:500;not null;default:''" json:"description"`
	ResourceType string     `gorm:"column:resource_type;size:32;not null;default:''" json:"resourceType"`
	RiskLevel    string     `gorm:"column:risk_level;size:16;not null;default:read" json:"riskLevel"`
	Status       string     `gorm:"size:16;not null;default:draft" json:"status"`
	SysAPIID     *int64     `gorm:"column:sys_api_id;index:idx_openapi_capability_sys_api" json:"sysApiId,omitempty"`
	SysAPIPath   string     `gorm:"column:sys_api_path;size:255;not null;default:''" json:"sysApiPath"`
	SysAPIMethod string     `gorm:"column:sys_api_method;size:16;not null;default:''" json:"sysApiMethod"`
	PublishedAt  *time.Time `gorm:"column:published_at" json:"publishedAt,omitempty"`
	RowVersion   int64      `gorm:"column:row_version;not null;default:1" json:"rowVersion"`
	CreatedBy    uint       `gorm:"not null;default:0" json:"createdBy"`
	UpdatedBy    uint       `gorm:"not null;default:0" json:"updatedBy"`
	CreatedAt    time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt    time.Time  `gorm:"not null" json:"updatedAt"`
	DeletedAt    *time.Time `gorm:"column:deleted_at;index:idx_openapi_capability_deleted" json:"-"`
}

func (Capability) TableName() string { return "sys_openapi_capability" }

// Operation is the externally visible HTTP contract attached to one
// capability. The adapter key is resolved only through code, never evaluated
// from database text.
type Operation struct {
	ID                     int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CapabilityID           int64      `gorm:"column:capability_id;not null;index:idx_openapi_operation_capability" json:"capabilityId"`
	Code                   string     `gorm:"size:96;not null" json:"code"`
	Name                   string     `gorm:"size:160;not null" json:"name"`
	Method                 string     `gorm:"size:16;not null;uniqueIndex:uk_openapi_operation_route,priority:1" json:"method"`
	ExternalPath           string     `gorm:"column:external_path;size:255;not null;uniqueIndex:uk_openapi_operation_route,priority:2" json:"externalPath"`
	AdapterKey             string     `gorm:"column:adapter_key;size:96;not null" json:"adapterKey"`
	AdapterContractVersion string     `gorm:"column:adapter_contract_version;size:32;not null;default:v1" json:"adapterContractVersion"`
	ResourceType           string     `gorm:"column:resource_type;size:32;not null;default:''" json:"resourceType"`
	RiskLevel              string     `gorm:"column:risk_level;size:16;not null;default:read" json:"riskLevel"`
	IdempotencyMode        string     `gorm:"column:idempotency_mode;size:16;not null;default:none" json:"idempotencyMode"`
	RequestSchema          string     `gorm:"column:request_schema;type:text;not null" json:"requestSchema"`
	ResponseSchema         string     `gorm:"column:response_schema;type:text;not null" json:"responseSchema"`
	SysAPIID               *int64     `gorm:"column:sys_api_id;index:idx_openapi_operation_sys_api" json:"sysApiId,omitempty"`
	SysAPIPath             string     `gorm:"column:sys_api_path;size:255;not null;default:''" json:"sysApiPath"`
	SysAPIMethod           string     `gorm:"column:sys_api_method;size:16;not null;default:''" json:"sysApiMethod"`
	Sort                   int        `gorm:"not null;default:0" json:"sort"`
	Status                 string     `gorm:"size:16;not null;default:draft" json:"status"`
	CreatedBy              uint       `gorm:"not null;default:0" json:"createdBy"`
	UpdatedBy              uint       `gorm:"not null;default:0" json:"updatedBy"`
	CreatedAt              time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt              time.Time  `gorm:"not null" json:"updatedAt"`
	DeletedAt              *time.Time `gorm:"column:deleted_at;index:idx_openapi_operation_deleted" json:"-"`
}

func (Operation) TableName() string { return "sys_openapi_operation" }

// Release is the lifecycle record for one immutable catalog publication.
// ReleaseItem is the runtime source of truth; this row only tracks lifecycle
// and content identity.
type Release struct {
	ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Version       int64      `gorm:"not null;uniqueIndex:uk_openapi_release_version" json:"version"`
	Name          string     `gorm:"size:160;not null;default:''" json:"name"`
	Status        string     `gorm:"size:16;not null;default:draft;index:idx_openapi_release_status" json:"status"`
	SnapshotHash  string     `gorm:"column:snapshot_hash;size:64;not null;default:''" json:"snapshotHash"`
	ItemCount     int        `gorm:"column:item_count;not null;default:0" json:"itemCount"`
	ParentRelease *int64     `gorm:"column:parent_release_id;index:idx_openapi_release_parent" json:"parentReleaseId,omitempty"`
	PublishedBy   *uint      `gorm:"column:published_by" json:"publishedBy,omitempty"`
	PublishedAt   *time.Time `gorm:"column:published_at" json:"publishedAt,omitempty"`
	RollbackOf    *int64     `gorm:"column:rollback_of_id;index:idx_openapi_release_rollback" json:"rollbackOfId,omitempty"`
	ErrorMessage  string     `gorm:"column:error_message;size:500;not null;default:''" json:"errorMessage"`
	CreatedBy     uint       `gorm:"not null;default:0" json:"createdBy"`
	CreatedAt     time.Time  `gorm:"not null" json:"createdAt"`
	UpdatedAt     time.Time  `gorm:"not null" json:"updatedAt"`
}

func (Release) TableName() string { return "sys_openapi_release" }

// ReleaseItem is a complete, immutable operation snapshot. Source IDs are
// retained only for diagnostics; runtime routing uses the copied contract.
type ReleaseItem struct {
	ID                     int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ReleaseID              int64     `gorm:"column:release_id;not null;uniqueIndex:uk_openapi_release_item_route,priority:1" json:"releaseId"`
	CapabilityID           *int64    `gorm:"column:capability_id;index:idx_openapi_release_item_capability" json:"capabilityId,omitempty"`
	OperationID            *int64    `gorm:"column:operation_id;index:idx_openapi_release_item_operation" json:"operationId,omitempty"`
	GroupCode              string    `gorm:"column:group_code;size:64;not null" json:"groupCode"`
	GroupName              string    `gorm:"column:group_name;size:100;not null" json:"groupName"`
	CapabilityCode         string    `gorm:"column:capability_code;size:96;not null" json:"capabilityCode"`
	CapabilityName         string    `gorm:"column:capability_name;size:160;not null" json:"capabilityName"`
	Scope                  string    `gorm:"size:96;not null" json:"scope"`
	Method                 string    `gorm:"size:16;not null;uniqueIndex:uk_openapi_release_item_route,priority:2" json:"method"`
	ExternalPath           string    `gorm:"column:external_path;size:255;not null;uniqueIndex:uk_openapi_release_item_route,priority:3" json:"externalPath"`
	AdapterKey             string    `gorm:"column:adapter_key;size:96;not null" json:"adapterKey"`
	AdapterContractVersion string    `gorm:"column:adapter_contract_version;size:32;not null" json:"adapterContractVersion"`
	ResourceType           string    `gorm:"column:resource_type;size:32;not null;default:''" json:"resourceType"`
	RiskLevel              string    `gorm:"column:risk_level;size:16;not null" json:"riskLevel"`
	IdempotencyMode        string    `gorm:"column:idempotency_mode;size:16;not null" json:"idempotencyMode"`
	RequestSchema          string    `gorm:"column:request_schema;type:text;not null" json:"requestSchema"`
	ResponseSchema         string    `gorm:"column:response_schema;type:text;not null" json:"responseSchema"`
	SysAPIID               *int64    `gorm:"column:sys_api_id" json:"sysApiId,omitempty"`
	SysAPIPath             string    `gorm:"column:sys_api_path;size:255;not null;default:''" json:"sysApiPath"`
	SysAPIMethod           string    `gorm:"column:sys_api_method;size:16;not null;default:''" json:"sysApiMethod"`
	Sort                   int       `gorm:"not null;default:0" json:"sort"`
	SnapshotJSON           string    `gorm:"column:snapshot_json;type:text;not null" json:"snapshotJson"`
	CreatedAt              time.Time `gorm:"not null" json:"createdAt"`
}

func (ReleaseItem) TableName() string { return "sys_openapi_release_item" }

// RuntimeState is a singleton projection of the active immutable release.
// It is intentionally not a foreign-key owner of client scopes or source APIs.
type RuntimeState struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	ActiveRelease *int64     `gorm:"column:active_release_id;index:idx_openapi_runtime_release" json:"activeReleaseId,omitempty"`
	ActiveVersion int64      `gorm:"column:active_version;not null;default:0" json:"activeVersion"`
	SnapshotHash  string     `gorm:"column:snapshot_hash;size:64;not null;default:''" json:"snapshotHash"`
	Status        string     `gorm:"size:24;not null;default:ready" json:"status"`
	RuntimeEpoch  int64      `gorm:"column:runtime_epoch;not null;default:0" json:"runtimeEpoch"`
	LastError     string     `gorm:"column:last_error;size:500;not null;default:''" json:"lastError"`
	LoadedAt      *time.Time `gorm:"column:loaded_at" json:"loadedAt,omitempty"`
	UpdatedAt     time.Time  `gorm:"not null" json:"updatedAt"`
}

func (RuntimeState) TableName() string { return "sys_openapi_runtime_state" }

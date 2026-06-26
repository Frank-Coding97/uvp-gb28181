package models

import "time"

// FallbackType anomaly 兜底落点类型
type FallbackType string

const (
	FallbackTypeVirtualOrg FallbackType = "virtual_org"
	FallbackTypeChannel    FallbackType = "channel"
	FallbackTypeDevice     FallbackType = "device"
)

// GbAnomalyRecord 目录异常审计(Q1 决议;plan §3.7)
// 每一次"识别失败被兜底"的事件留痕,供 anomaly 详情页一键改类型/挂载
type GbAnomalyRecord struct {
	ID              uint         `gorm:"primarykey" json:"id"`
	TenantID        uint         `gorm:"column:tenant_id;index:idx_tenant_resolved,priority:1" json:"tenantId"`
	CatalogNodeID   uint         `gorm:"column:catalog_node_id;not null;index:idx_node" json:"catalogNodeId"`
	RawCode         string       `gorm:"column:raw_code;size:64;not null" json:"rawCode"`
	GuessedType     string       `gorm:"column:guessed_type;size:32" json:"guessedType"`
	FallbackType    FallbackType `gorm:"column:fallback_type;size:16;not null" json:"fallbackType"`
	SourceDeviceID  *uint        `gorm:"column:source_device_id" json:"sourceDeviceId"`
	Reason          string       `gorm:"column:reason;size:255" json:"reason"`
	Resolved        bool         `gorm:"column:resolved;default:false;index:idx_tenant_resolved,priority:2" json:"resolved"`
	ResolvedBy      *uint        `gorm:"column:resolved_by" json:"resolvedBy"`
	ResolvedAt      *time.Time   `gorm:"column:resolved_at" json:"resolvedAt"`
	ResolvedAction  string       `gorm:"column:resolved_action;size:64" json:"resolvedAction"`
	CreatedAt       time.Time    `gorm:"index:idx_tenant_resolved,priority:3" json:"createdAt"`
}

// TableName 表名
func (GbAnomalyRecord) TableName() string { return "gb_anomaly_record" }

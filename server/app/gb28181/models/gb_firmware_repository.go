package models

import "time"

// FirmwareStatus represents the lifecycle state of a firmware file.
type FirmwareStatus string

const (
	FirmwareStatusDraft     FirmwareStatus = "draft"
	FirmwareStatusPublished FirmwareStatus = "published"
	FirmwareStatusArchived  FirmwareStatus = "archived"
)

// GbFirmwareRepository stores uploaded firmware files with versioning,
// manufacturer metadata, and tenant isolation.
type GbFirmwareRepository struct {
	ID           uint64         `gorm:"primaryKey;column:id" json:"id"`
	FirmwareID   string         `gorm:"column:firmware_id;size:64;uniqueIndex;not null" json:"firmwareId"`
	Version      string         `gorm:"column:version;size:255;not null;index:idx_manu_model,priority:2" json:"version"`
	Manufacturer string         `gorm:"column:manufacturer;size:255;not null;index:idx_manu_model,priority:1" json:"manufacturer"`
	ModelPattern string         `gorm:"column:model_pattern;size:500" json:"modelPattern"`
	FileName     string         `gorm:"column:file_name;size:500;not null" json:"fileName"`
	FileSize     int64          `gorm:"column:file_size;not null" json:"fileSize"`
	FileHash     string         `gorm:"column:file_hash;size:128;index" json:"fileHash"`
	StoragePath  string         `gorm:"column:storage_path;type:text;not null" json:"-"`
	StorageKey   string         `gorm:"column:storage_key;size:500" json:"-"`
	ReleaseDate  *time.Time     `gorm:"column:release_date;type:date" json:"releaseDate"`
	Status       FirmwareStatus `gorm:"column:status;size:20;not null;index:idx_dept_status,priority:2" json:"status"`
	UploadedBy   uint64         `gorm:"column:uploaded_by;not null" json:"uploadedBy"`
	DeptID       uint64         `gorm:"column:dept_id;not null;index:idx_dept_status,priority:1" json:"deptId"`
	CreatedAt    time.Time      `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
	Remark       string         `gorm:"column:remark;type:text" json:"remark"`
}

func (GbFirmwareRepository) TableName() string {
	return "gb_firmware_repository"
}

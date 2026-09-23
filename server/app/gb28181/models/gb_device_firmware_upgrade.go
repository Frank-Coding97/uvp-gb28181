package models

import "time"

// FirmwareUpgradeStatus is the durable platform state for one whole-device
// GB/T 28181-2022 software upgrade.
type FirmwareUpgradeStatus string

const (
	FirmwareUpgradeQueued    FirmwareUpgradeStatus = "queued"
	FirmwareUpgradeSent      FirmwareUpgradeStatus = "sent"
	FirmwareUpgradeAccepted  FirmwareUpgradeStatus = "accepted"
	FirmwareUpgradeSucceeded FirmwareUpgradeStatus = "succeeded"
	FirmwareUpgradeFailed    FirmwareUpgradeStatus = "failed"
	FirmwareUpgradeRejected  FirmwareUpgradeStatus = "rejected"
	FirmwareUpgradeUnknown   FirmwareUpgradeStatus = "unknown"
)

func (s FirmwareUpgradeStatus) Terminal() bool {
	switch s {
	case FirmwareUpgradeSucceeded, FirmwareUpgradeFailed, FirmwareUpgradeRejected:
		return true
	default:
		return false
	}
}

// GbDeviceFirmwareUpgrade stores the request and all protocol milestones.
// FileURL is deliberately omitted from JSON: it may contain a temporary
// access token and is only needed to reconstruct the outbound XML.
type GbDeviceFirmwareUpgrade struct {
	ID              uint                  `gorm:"primaryKey" json:"id"`
	OperationID     string                `gorm:"column:operation_id;size:64;not null;uniqueIndex:uk_firmware_upgrade_operation" json:"operationId"`
	IdempotencyKey  string                `gorm:"column:idempotency_key;size:128;not null;uniqueIndex:uk_firmware_upgrade_device_idempotency,priority:2" json:"-"`
	DeviceID        uint                  `gorm:"column:device_id;not null;index:idx_firmware_upgrade_device_time,priority:1;index:idx_firmware_upgrade_device_status,priority:1;uniqueIndex:uk_firmware_upgrade_device_idempotency,priority:1;uniqueIndex:uk_firmware_upgrade_device_session,priority:1" json:"deviceId"`
	DeviceCode      string                `gorm:"column:device_code;size:20;not null;index:idx_firmware_upgrade_device_sn,priority:1;index:idx_firmware_upgrade_device_session,priority:1" json:"deviceCode"`
	Firmware        string                `gorm:"column:firmware;size:255;not null" json:"firmware"`
	FileURL         string                `gorm:"column:file_url;size:2048;not null" json:"-"`
	Manufacturer    string                `gorm:"column:manufacturer;size:255;not null" json:"manufacturer"`
	SessionID       string                `gorm:"column:session_id;size:128;not null;uniqueIndex:uk_firmware_upgrade_device_session,priority:2;index:idx_firmware_upgrade_device_session,priority:2" json:"sessionId"`
	SN              int                   `gorm:"column:sn;not null;index:idx_firmware_upgrade_device_sn,priority:2;uniqueIndex:uk_firmware_upgrade_sn" json:"sn"`
	ProfileVersion  string                `gorm:"column:profile_version;size:8;not null" json:"profileVersion"`
	ProfileCharset  string                `gorm:"column:profile_charset;size:16;not null" json:"profileCharset"`
	SIPStatus       int                   `gorm:"column:sip_status;not null;default:0" json:"sipStatus"`
	SIPCallID       string                `gorm:"column:sip_call_id;size:255" json:"-"`
	SIPCSeq         string                `gorm:"column:sip_cseq;size:64" json:"-"`
	DeviceResult    string                `gorm:"column:device_result;size:16" json:"deviceResult"`
	DeviceError     string                `gorm:"column:device_error;type:text" json:"deviceError"`
	Status          FirmwareUpgradeStatus `gorm:"column:status;size:16;not null;index:idx_firmware_upgrade_device_status,priority:2" json:"status"`
	ErrorCode       string                `gorm:"column:error_code;size:64" json:"errorCode"`
	ErrorMessage    string                `gorm:"column:error_message;type:text" json:"errorMessage"`
	FailedReason    string                `gorm:"column:failed_reason;size:8" json:"failedReason"`
	CurrentFirmware string                `gorm:"column:current_firmware;size:255" json:"currentFirmware"`
	ActorID         uint                  `gorm:"column:actor_id;not null;default:0" json:"actorId"`
	ActorDeptID     uint                  `gorm:"column:actor_dept_id;not null;default:0" json:"actorDeptId"`
	CreatedAt       time.Time             `gorm:"column:created_at;not null;index:idx_firmware_upgrade_device_time,priority:2" json:"createdAt"`
	UpdatedAt       time.Time             `gorm:"column:updated_at;not null" json:"updatedAt"`
	SentAt          *time.Time            `gorm:"column:sent_at" json:"sentAt"`
	AcceptedAt      *time.Time            `gorm:"column:accepted_at" json:"acceptedAt"`
	CompletedAt     *time.Time            `gorm:"column:completed_at" json:"completedAt"`
	DeadlineAt      *time.Time            `gorm:"column:deadline_at;index:idx_firmware_upgrade_deadline" json:"deadlineAt"`
	ResponseAt      *time.Time            `gorm:"column:response_at" json:"-"`
	ResponseCallID  string                `gorm:"column:response_call_id;size:255" json:"-"`
	ResponseCSeq    string                `gorm:"column:response_cseq;size:64" json:"-"`
}

func (GbDeviceFirmwareUpgrade) TableName() string { return "gb_device_firmware_upgrade" }

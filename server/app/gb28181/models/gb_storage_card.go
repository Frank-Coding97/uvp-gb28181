package models

import "time"

// StorageCardState 是 GB/T 28181-2022 A.2.6.16 里 SDCardStatusInfo/Item/Status
// 的取值域。DB 里原样存标准的小写字面量，避免"平台内部一套叫法、标准另一套"
// 的双向映射 —— 排障时直接在表里看到的就是线上报文里的那个词。
type StorageCardState string

const (
	StorageCardStateOK          StorageCardState = "ok"
	StorageCardStateFormatting  StorageCardState = "formatting"
	StorageCardStateUnformatted StorageCardState = "unformatted"
	StorageCardStateIdle        StorageCardState = "idle"
	StorageCardStateError       StorageCardState = "error"
	// StorageCardStateUnknown 是设备报了个我们不认识的取值时的落点。
	// ⛔ 刻意不复用 StorageCardStateError：设备报了个新状态 ≠ 卡坏了。
	StorageCardStateUnknown StorageCardState = "unknown"
)

// StorageCardStateValid 报告取值是否落在标准枚举内。
func StorageCardStateValid(state StorageCardState) bool {
	switch state {
	case StorageCardStateOK, StorageCardStateFormatting, StorageCardStateUnformatted,
		StorageCardStateIdle, StorageCardStateError, StorageCardStateUnknown:
		return true
	default:
		return false
	}
}

// GbDeviceStorageCard 是一张存储卡的**最近一次查询事实**。
//
// 语义与 GbPTZHomePosition 同族：这里存的是"设备最近一次应答里怎么说"，
// 不是设备的实时状态。ObservedAt 之后设备侧可能已经变了，前端据此判 fresh/stale。
//
// ⭐ 为什么是"每张卡一行"而不是"一次查询一行"：A.2.6.16 的应答本身就是一个
// 列表（Item maxOccurs=8），而前端要展示的是每张卡的容量/剩余/状态。一次查询
// 一行会把列表塞进 JSON 列，导致"卡 2 的剩余空间变化"这种最常见的对账无从比较。
//
// ⛔ 唯一键是 (device_id, target_code, card_id) 三元组，不是 (device_id, card_id)：
// 同一台设备既可以被按**设备编码**查，也可以被按**某个通道编码**查（标准里
// DeviceID 是"查询目标设备编码"），两条路径得到的卡列表未必一样。三元组让两条
// 路径各自收敛，不会互相覆盖。
type GbDeviceStorageCard struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	DeviceID   uint   `gorm:"column:device_id;not null;index:idx_storage_card_device,priority:1;uniqueIndex:uk_storage_card_target,priority:1" json:"deviceId"`
	TargetCode string `gorm:"column:target_code;size:20;not null;uniqueIndex:uk_storage_card_target,priority:2" json:"targetCode"`
	// CardID 是标准里的 SD卡编号(ID)，从 1 开始。0 只可能来自不合规的设备。
	CardID  int              `gorm:"column:card_id;not null;uniqueIndex:uk_storage_card_target,priority:3" json:"cardId"`
	HddName string           `gorm:"column:hdd_name;size:128;not null;default:''" json:"hddName"`
	Status  StorageCardState `gorm:"column:status;size:16;not null;default:unknown" json:"status"`
	// FormatProgress 对应标准的可选字段：只在 Status=formatting 时有意义，
	// 所以用指针保留"设备没给"与"给了 0"的区别。
	FormatProgress *int `gorm:"column:format_progress" json:"formatProgress"`
	// CapacityMB / FreeSpaceMB 单位都是 MB（标准原文「存储容量，单位：MB」）。
	CapacityMB  int `gorm:"column:capacity_mb;not null;default:0" json:"capacityMb"`
	FreeSpaceMB int `gorm:"column:free_space_mb;not null;default:0" json:"freeSpaceMb"`
	// SourceOperationSeq 是落库时的单调序号 CAS 依据：只有更晚的查询才能
	// 覆盖已有的卡事实，迟到的旧应答不会把新数据写回去。
	SourceOperationSeq uint      `gorm:"column:source_operation_seq;not null;default:0" json:"sourceOperationSeq"`
	SourceSN           int       `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	SourceOperationID  *string   `gorm:"column:source_operation_id;size:64" json:"sourceOperationId"`
	ObservedAt         time.Time `gorm:"column:observed_at;not null;index:idx_storage_card_device,priority:2" json:"observedAt"`
	RawSummary         string    `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbDeviceStorageCard) TableName() string { return "gb_device_storage_card" }

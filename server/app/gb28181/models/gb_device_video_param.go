package models

import "time"

// GbDeviceVideoParam 是一条码流的**最近一次回读事实**（GB/T 28181-2022 A.2.1.13）。
//
// 语义与 GbDeviceStorageCard 同族：这里存的是"设备最近一次应答里怎么说"，
// 不是设备的实时配置。ObservedAt 之后设备侧可能已经变了，前端据此判 fresh/stale。
//
// ⭐ 为什么唯一键里有 stream_number：标准里 `Item` 是 `maxOccurs="unbounded"`，
// 同一台设备按 `StreamNumber` 分条（0=主码流 / 1=子码流1 …）。
// 一次查询 = 每个码流一份配置，所以"每码流一行"才是可对账的最小粒度。
//
// ⛔ 唯一键是 (device_id, target_code, stream_number) 三元组，不是 (device_id, stream_number)：
// 同一台设备既可以被按**设备编码**查，也可以被按**某个通道编码**查（标准的 DeviceID
// 是"查询目标设备编码"），两条路径得到的配置未必一样。三元组让两条路径各自收敛。
type GbDeviceVideoParam struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	DeviceID   uint   `gorm:"column:device_id;not null;index:idx_video_param_device,priority:1;uniqueIndex:uk_video_param_target,priority:1" json:"deviceId"`
	TargetCode string `gorm:"column:target_code;size:20;not null;uniqueIndex:uk_video_param_target,priority:2" json:"targetCode"`
	// StreamNumber 标准明文：0=主码流；1=子码流1；2=子码流2…
	StreamNumber int `gorm:"column:stream_number;not null;uniqueIndex:uk_video_param_target,priority:3" json:"streamNumber"`
	// ---- 以下五列**原样存附录 G 的码值字符串**，不做码值→人读串的转换 ----
	// ⛔ 一旦在这里存成人读串，对账时比的就是两套表示，设备回 "2" 而库里是 "H.264"
	// 会永远判成不一致。人读串只允许出现在前端展示层。
	VideoFormat string `gorm:"column:video_format;size:8;not null;default:''" json:"videoFormat"`
	Resolution  string `gorm:"column:resolution;size:32;not null;default:''" json:"resolution"`
	FrameRate   string `gorm:"column:frame_rate;size:8;not null;default:''" json:"frameRate"`
	BitRateType string `gorm:"column:bit_rate_type;size:8;not null;default:''" json:"bitRateType"`
	// VideoBitRate 单位 kb/s（附录 G），是**条件必选**：仅 BitRateType=1(CBR) 时才有值。
	// ⛔ 用可空列而不是空串：NULL 表示"这一帧设备没给这个元素"，
	// 与"设备给了个 0"是两件事（标准的注释是「固定码率时必选」）。
	VideoBitRate *string `gorm:"column:video_bit_rate;size:16" json:"videoBitRate"`
	// SourceOperationSeq 是落库时的单调序号 CAS 依据：只有更晚的回读才能覆盖
	// 已有的行，迟到的旧应答不会把新数据写回去。
	SourceOperationSeq uint      `gorm:"column:source_operation_seq;not null;default:0" json:"sourceOperationSeq"`
	SourceSN           int       `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	SourceOperationID  *string   `gorm:"column:source_operation_id;size:64" json:"sourceOperationId"`
	ObservedAt         time.Time `gorm:"column:observed_at;not null;index:idx_video_param_device,priority:2" json:"observedAt"`
	RawSummary         string    `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbDeviceVideoParam) TableName() string { return "gb_device_video_param" }

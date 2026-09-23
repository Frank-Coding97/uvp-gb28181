package models

import "time"

// GbDeviceConfig 是**一个设备的某一组配置**最近一次回读得到的事实。
//
// 语义与 GbDeviceVideoParam / GbDeviceStorageCard 同族：存的是"设备最近一次应答里怎么说"，
// 不是设备的实时配置。ObservedAt 之后设备侧可能已经变了，前端据此判 fresh/stale。
//
// ⭐ 为什么是**一张通用表**而不是每组配置一张：A.2.1 的配置家族有 8 个 ConfigType，
// 但它们的**读写通道是同一条**（A.2.4.7 ConfigDownload 查询 / A.2.3.2.5 DeviceConfig 下发），
// 差别只在块内字段。给每组各建一张表意味着 8 套迁移 × 3 方言 + 8 个模型 + 8 个 DDL 测试，
// 而换来的查询能力这个场景用不上（读取永远是"按 device + target + type 取一行"）。
// 所以这里用 payload_json 承载组内结构，**组内字段的取值范围校验留给协议层**
// （manscdp 的 ValidateDeviceConfigBlocks / 各块 validate），不在库里做约束。
//
// ⛔ 与 VideoParamAttribute 那套（每码流一行）的分工：那一组要按 stream_number 分条，
// 是"每码流一份配置"的真结构差异，所以它有专门的表；本表按 config_type 分条即可。
//
// ⛔ 唯一键是 (device_id, target_code, config_type) 三元组，不是 (device_id, config_type)：
// 同一台设备既可以被按**设备编码**查，也可以被按**某个通道编码**查（标准的 DeviceID
// 是"查询目标设备编码"），两条路径得到的配置未必一样。三元组让两条路径各自收敛
// —— 与 GbDeviceVideoParam 同一条口径。
type GbDeviceConfig struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	DeviceID   uint   `gorm:"column:device_id;not null;uniqueIndex:uk_device_config_target,priority:1;index:idx_device_config_device,priority:1" json:"deviceId"`
	TargetCode string `gorm:"column:target_code;size:20;not null;uniqueIndex:uk_device_config_target,priority:2" json:"targetCode"`
	// ConfigType 是标准里的 `ConfigType` 取值（BasicParam / OSDConfig / PictureMask /
	// FrameMirror / VideoRecordPlan / VideoAlarmRecord / AlarmReport / VideoParamOpt）。
	// ⛔ 存**标准原文名**，不存人读串 —— 对账时比的是同一套表示。
	ConfigType string `gorm:"column:config_type;size:32;not null;uniqueIndex:uk_device_config_target,priority:3" json:"configType"`
	// PayloadJSON 是该组配置的规范化 JSON（键 = XML 元素名，与协议层 DTO 一一对应）。
	// ⛔ 只存**设备真的发回来的**东西：元素缺席与"设备回了个 0"必须能在 JSON 里区分，
	// 所以可空元素在协议层是 *int/*string，序列化后是 null 而不是 0。
	PayloadJSON string `gorm:"column:payload_json;type:text;not null" json:"payload"`
	// SourceOperationSeq 是落库时的单调序号 CAS 依据：只有更晚的回读才能覆盖
	// 已有的行，迟到的旧应答不会把新数据写回去。
	SourceOperationSeq uint      `gorm:"column:source_operation_seq;not null;default:0" json:"sourceOperationSeq"`
	SourceSN           int       `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	SourceOperationID  *string   `gorm:"column:source_operation_id;size:64" json:"sourceOperationId"`
	ObservedAt         time.Time `gorm:"column:observed_at;not null;index:idx_device_config_device,priority:2" json:"observedAt"`
	RawSummary         string    `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbDeviceConfig) TableName() string { return "gb_device_config" }

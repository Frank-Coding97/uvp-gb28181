package models

import "time"

// TargetTrackMode 是落库的跟踪模式，取值就是标准 A.2.3.1.14 `TargetTrack`
// 元素的原样拼写（Auto/Manual/Stop）。
//
// ⛔ 刻意不做"平台内部小写化"：本仓其它状态列（如 StorageCardState）也是"存标准原样"，
// 排障时在表里直接看到的就是线上报文里的那个词，中间少一层映射就少一处能写歪的地方。
type TargetTrackMode string

const (
	TargetTrackModeAuto   TargetTrackMode = "Auto"
	TargetTrackModeManual TargetTrackMode = "Manual"
	TargetTrackModeStop   TargetTrackMode = "Stop"
)

// TargetTrackModeValid 报告取值是否落在标准枚举内。
func TargetTrackModeValid(mode TargetTrackMode) bool {
	switch mode {
	case TargetTrackModeAuto, TargetTrackModeManual, TargetTrackModeStop:
		return true
	default:
		return false
	}
}

// GbDeviceTargetTrack 是**平台最近一次下发的目标跟踪指令**。
//
// ⛔⛔ 语义纪律：这不是"设备当前正在跟踪什么"。
// 目标跟踪在 GB/T 28181-2022 里是一条**无应答命令**（9.3.1 d) + 表 1 序号 13）
// —— 设备不会回执，而且 2022 全文里**没有**任何"目标跟踪状态查询/上报"的命令
// （对比：存储卡格式化至少还能事后再查一次 SDCardStatus 做对账，这里连这个都没有）。
// ⇒ 本表存的是**平台的意图**，读接口与界面**必须**把它显示成
// 「已下发，设备未回执，实际状态未知」，不许写成「正在跟踪」。
//
// 之所以仍然要落库（而不是只留 gb_ptz_operation 那行审计）：意图要能在刷新页面、
// 换一个操作员、甚至审计记录老化之后仍然看得见。同族的"平台侧认定"先例是
// GbPTZHomePosition 的 Source=control_ack 一行。
//
// ⛔ 唯一键是 (device_id, target_code) 两元组，target_code 是**球机通道编码**
// （= 报文里 SN 之后的 DeviceID，标准尾注原文「指全景相机的球机通道」）。
// 全景通道编码只作为一列 DeviceID2 存着，**不进唯一键** —— 同一台球机换个全景通道
// 再下发一次，是"同一条跟踪指令的更新"，不是第二条记录。
type GbDeviceTargetTrack struct {
	ID         uint            `gorm:"primaryKey" json:"id"`
	DeviceID   uint            `gorm:"column:device_id;not null;index:idx_target_track_device,priority:1;uniqueIndex:uk_target_track_target,priority:1" json:"deviceId"`
	ChannelID  uint            `gorm:"column:channel_id;not null;default:0;index:idx_target_track_channel" json:"channelId"`
	TargetCode string          `gorm:"column:target_code;size:20;not null;uniqueIndex:uk_target_track_target,priority:2" json:"targetCode"`
	Mode       TargetTrackMode `gorm:"column:mode;size:16;not null" json:"mode"`
	// DeviceID2 是报文里的"全景相机中的全景通道ID"，可能为空（Auto/Stop 通常不带）。
	// ⛔ 空串与"设备没上报"在这里是同一件事 —— 两者都表示"这条指令没指定全景通道"，
	// 所以用值类型而不是指针，不像设备自报事实那几列那样需要区分。
	DeviceID2 string `gorm:"column:device_id2;size:20;not null;default:''" json:"deviceId2"`
	// 以下六列是 TargetArea 的六个子元素，**整体可空**：Auto/Stop 不带框选坐标。
	// ⛔ 必须可空且不许兜底成 0：窗口尺寸为 0 是非法值，把"没给框"写成
	// "(0,0,0,0,0,0)" 会让界面显示一个钉在左上角的假跟踪框。
	AreaLength    *int `gorm:"column:area_length" json:"areaLength,omitempty"`
	AreaWidth     *int `gorm:"column:area_width" json:"areaWidth,omitempty"`
	AreaMidPointX *int `gorm:"column:area_mid_point_x" json:"areaMidPointX,omitempty"`
	AreaMidPointY *int `gorm:"column:area_mid_point_y" json:"areaMidPointY,omitempty"`
	AreaLengthX   *int `gorm:"column:area_length_x" json:"areaLengthX,omitempty"`
	AreaLengthY   *int `gorm:"column:area_length_y" json:"areaLengthY,omitempty"`
	// SourceOperationSeq 是单调序号 CAS 依据：迟到的重放（同一 idempotency key
	// 返回的旧 operation）不会把更晚一次下发写进去的意图覆盖回去。
	SourceOperationSeq uint      `gorm:"column:source_operation_seq;not null;default:0" json:"sourceOperationSeq"`
	SourceSN           int       `gorm:"column:source_sn;not null;default:0" json:"sourceSn"`
	SourceOperationID  *string   `gorm:"column:source_operation_id;size:64" json:"sourceOperationId"`
	CommandedBy        uint      `gorm:"column:commanded_by;not null;default:0" json:"commandedBy"`
	CommandedByDeptID  uint      `gorm:"column:commanded_by_dept_id;not null;default:0" json:"commandedByDeptId"`
	CommandedAt        time.Time `gorm:"column:commanded_at;not null;index:idx_target_track_device,priority:2" json:"commandedAt"`
	RawSummary         string    `gorm:"column:raw_summary;type:text" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbDeviceTargetTrack) TableName() string { return "gb_device_target_track" }

// TargetTrackRoutePath 是目标跟踪接口**在路由组内**的相对路径
// （挂在 `/api/gb28181/device-mgmt` 组下）。
//
// ⛔ 读与写**共用一个路径、靠方法区分**，照 video-params 的先例
// （读 `gb28181:ptz:view` / 写 `gb28181:ptz:control`），不是两条路径。
// ⛔ 与 [TargetTrackAPIPath] 拆成两个常量，理由同 StorageCardFormatRoutePath：
// 前者用相对路径（gin 组自动补前缀），后者必须是被鉴权中间件实际看到的全路径。
// 两个都手写字面量时，错一个字符的表现是"接口通但恒 403"或"根本没注册"，且两边都不报错。
const TargetTrackRoutePath = "/channel/:id/target-track"

// TargetTrackAPIPath 是目标跟踪接口的**全路径**（三处必须同名：迁移 / 路由 / 测试）。
const TargetTrackAPIPath = "/api/gb28181/device-mgmt" + TargetTrackRoutePath

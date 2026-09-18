package models

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// 通道在线状态
const (
	ChannelStatusOffline int8 = 0
	ChannelStatusOnline  int8 = 1
)

// GbChannel 国标通道(设备下的视频通道,Catalog 填充)
type GbChannel struct {
	ID           uint   `gorm:"primarykey" json:"id"`
	ChannelID    string `gorm:"column:channel_id;size:20;comment:通道国标编码" json:"channelId"`
	DeviceID     string `gorm:"column:device_id;size:20;comment:所属设备编码" json:"deviceId"`
	Name         string `gorm:"column:name;size:255;comment:通道名称" json:"name"`
	Alias        string `gorm:"column:alias;size:255;default:'';comment:用户自定义别名(不被上报覆盖)" json:"alias"`
	Manufacturer string `gorm:"column:manufacturer;size:255" json:"manufacturer"`
	Model        string `gorm:"column:model;size:255" json:"model"`
	Owner        string `gorm:"column:owner;size:64" json:"owner"`
	CivilCode    string `gorm:"column:civil_code;size:32;comment:行政区划" json:"civilCode"`
	ParentID     string `gorm:"column:parent_id;size:20;comment:父节点编码" json:"parentId"`
	PTZType      int8   `gorm:"column:ptz_type;comment:云台类型 0未知 1球机 2半球 3固定枪机 4遥控枪机 5遥控半球 6多目全景/拼接通道 7多目分割通道(5-7 为 2022 新增)" json:"ptzType"`
	// ---- 通道属性(GB/T 28181 附录 A / §9.3.1),全部来自 Catalog Item 的 <Info> 容器 ----
	// ⛔ 0 / '' 表示"设备未上报该属性",不是"该属性为 0" —— 落库侧据此决定是否覆盖。
	RoomType        int8   `gorm:"column:room_type;default:0;comment:室内外 0未上报 1室外 2室内(两版编码一致)" json:"roomType"`
	SupplyLightType int8   `gorm:"column:supply_light_type;default:0;comment:补光方式 0未上报 1无补光 2红外 3白光 4激光 9其他" json:"supplyLightType"`
	DirectionType   int8   `gorm:"column:direction_type;default:0;comment:方向 0未上报" json:"directionType"`
	Resolution      string `gorm:"column:resolution;size:32;default:'';comment:分辨率,如 1920*1080" json:"resolution"`
	// ---- 版本独有属性:2016 与 2022 各占一半,并存落库 ----
	// 设备上报哪一组,就说明它用的是哪一版目录形态;两组同时为空表示设备没报 <Info>。
	// ⛔ 不做版本分支:设备注册声明的 EffectiveVersion 有 default:2016,对 2022 设备会误判。
	PositionType             int8   `gorm:"column:position_type;default:0;comment:位置类型 0未上报 2016独有 1省际检查站…10交通干线" json:"positionType"`
	UseType                  int8   `gorm:"column:use_type;default:0;comment:用途 0未上报 2016独有 1治安 2交通 3重点" json:"useType"`
	PhotoelectricImagingType string `gorm:"column:photoelectric_imaging_type;size:32;default:'';comment:光电成像类型 2022独有,可多值 / 分隔" json:"photoelectricImagingType"`
	CapturePositionType      string `gorm:"column:capture_position_type;size:32;default:'';comment:采集部位类型 2022独有,见附录O" json:"capturePositionType"`
	// StreamNumberList 是设备声明的"支持的码流编号"列表（2022 独有，A.2.1.9 <Info> 内），
	// 形如 "0/1" 或 "0/1/2"；空串 = 设备本次未上报。
	//
	// ⛔ 它在视频参数面板里是「按几段码流渲染」的唯一出处，**不是** VideoParamOpt ——
	// 后者只有 DownloadSpeed + Resolution 两个字段，跟码流数量无关。
	// 之前 B-2 那批判定它属"点播取流能力"而只解析不落库，本轮面板需要它，故补落。
	StreamNumberList string `gorm:"column:stream_number_list;size:32;default:'';comment:支持的码流编号 2022独有,可多值 / 分隔,如 0/1 或 0/1/2" json:"streamNumberList"`
	// IPAddress / Port 来自 Catalog Item 层(不是 <Info>),为设备声明的通道取流地址
	IPAddress               string     `gorm:"column:ip_address;size:64;default:'';comment:设备声明的通道 IP(Catalog Item 层)" json:"ipAddress"`
	Port                    int        `gorm:"column:port;default:0;comment:设备声明的通道端口(Catalog Item 层)" json:"port"`
	Longitude               float64    `gorm:"column:longitude;comment:经度" json:"longitude"`
	Latitude                float64    `gorm:"column:latitude;comment:纬度" json:"latitude"`
	Status                  int8       `gorm:"column:status;default:0;comment:通道在线" json:"status"`
	StreamID                string     `gorm:"column:stream_id;size:64;comment:当前播放流ID" json:"streamId"`
	CurrentSSRC             string     `gorm:"column:current_ssrc;size:10;not null;default:'';comment:当前实时媒体会话SSRC" json:"currentSsrc"`
	OnDemandLive            bool       `gorm:"column:on_demand_live;default:true;comment:按需直播,无人观看自动关闭" json:"onDemandLive"`
	StreamTransport         string     `gorm:"column:stream_transport;size:16;default:TCP-Passive;comment:流传输模式 UDP/TCP-Active/TCP-Passive" json:"streamTransport"`
	AudioEnabled            bool       `gorm:"column:audio_enabled;default:true;comment:点播是否接收音频" json:"audioEnabled"`
	RecordingMode           string     `gorm:"column:recording_mode;size:16;default:off;comment:录像模式 off/continuous/scheduled" json:"recordingMode"`
	CloudRecordingEnabled   bool       `gorm:"column:cloud_recording_enabled;default:false;comment:云端录像期望开关" json:"cloudRecordingEnabled"`
	CloudRecordingState     string     `gorm:"column:cloud_recording_state;size:20;default:disabled;comment:云端录像运行状态" json:"cloudRecordingState"`
	CloudRecordingError     string     `gorm:"column:cloud_recording_error;size:500;default:'';comment:云端录像最近错误" json:"cloudRecordingError"`
	CloudRecordingUpdatedAt *time.Time `gorm:"column:cloud_recording_updated_at;comment:云端录像状态更新时间" json:"cloudRecordingUpdatedAt"`
	// Capabilities A1 新增:通道能力 JSON {audio, h265, night_vision, alarm_io, recording}
	// 用 *string + 默认 NULL — MySQL JSON 列不接受空字符串("The document is empty"),
	// nil 写入 NULL,前端拿到 null 即按"无能力上报"渲染
	Capabilities *string `gorm:"column:capabilities;type:json;comment:通道能力 JSON" json:"capabilities"`
	// SnapshotURL 通道最新快照 URL(相对路径,如 /uploads/gb-channel-snapshot/2026-07/xxx_yyy.jpg)
	// 由 snapshot.Service 在播放触发后异步更新
	SnapshotURL string     `gorm:"column:snapshot_url;size:500;default:'';comment:通道最新快照 URL" json:"snapshotUrl"`
	SnapshotAt  *time.Time `gorm:"column:snapshot_at;comment:通道最新快照抓拍时间" json:"snapshotAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `gorm:"index" json:"deletedAt"`
	OwnerDeptID uint       `gorm:"column:owner_dept_id" json:"ownerDeptId"`
}

func (GbChannel) TableName() string { return "gb_channel" }

type GbChannelList []*GbChannel

// UpsertChannel 按 device_id+channel_id 唯一键 upsert
func UpsertChannel(c context.Context, ch *GbChannel) error {
	var existing GbChannel
	result := app.DB().WithContext(c).
		Where("device_id = ? AND channel_id = ?", ch.DeviceID, ch.ChannelID).
		Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return app.DB().WithContext(c).Create(ch).Error
	}
	ch.ID = existing.ID
	updates := map[string]any{
		"name":              ch.Name,
		"manufacturer":      ch.Manufacturer,
		"model":             ch.Model,
		"owner":             ch.Owner,
		"civil_code":        ch.CivilCode,
		"parent_id":         ch.ParentID,
		"ptz_type":          ch.PTZType,
		"room_type":         ch.RoomType,
		"supply_light_type": ch.SupplyLightType,
		"direction_type":    ch.DirectionType,
		"resolution":        ch.Resolution,

		"position_type":              ch.PositionType,
		"use_type":                   ch.UseType,
		"photoelectric_imaging_type": ch.PhotoelectricImagingType,
		"capture_position_type":      ch.CapturePositionType,
		"stream_number_list":         ch.StreamNumberList,

		"ip_address":    ch.IPAddress,
		"port":          ch.Port,
		"longitude":     ch.Longitude,
		"latitude":      ch.Latitude,
		"status":        ch.Status,
		"capabilities":  ch.Capabilities,
		"owner_dept_id": ch.OwnerDeptID,
	}
	return app.DB().WithContext(c).Model(&GbChannel{}).Where("id = ?", existing.ID).Updates(updates).Error
}

// ListChannelsByDevice 列出某设备的所有通道
func ListChannelsByDevice(c context.Context, deviceID string) (GbChannelList, error) {
	var list GbChannelList
	err := app.DB().WithContext(c).Where("device_id = ?", deviceID).Order("channel_id").Find(&list).Error
	return list, err
}

// FindChannel 查单个通道
func FindChannel(c context.Context, deviceID, channelID string) (*GbChannel, error) {
	var ch GbChannel
	result := app.DB().WithContext(c).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Limit(1).Find(&ch)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ch, nil
}

// UpdateChannelStream 更新通道当前播放流ID
func UpdateChannelStream(c context.Context, deviceID, channelID, streamID string) error {
	return app.DB().WithContext(c).Model(&GbChannel{}).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Update("stream_id", streamID).Error
}

// SetChannelCurrent atomically records the current stream identity and its
// independent GB28181 media SSRC. Both values describe one live generation
// and must never be persisted by separate updates.
func SetChannelCurrent(c context.Context, deviceID, channelID, streamID, ssrc string) error {
	return app.DB().WithContext(c).Model(&GbChannel{}).
		Where("device_id = ? AND channel_id = ?", deviceID, channelID).
		Updates(map[string]any{"stream_id": streamID, "current_ssrc": ssrc}).Error
}

// ClearChannelStream 按当前流 ID 清空通道播放状态
func ClearChannelStream(c context.Context, streamID string) error {
	return app.DB().WithContext(c).Model(&GbChannel{}).
		Where("stream_id = ?", streamID).
		Update("stream_id", "").Error
}

// ClearChannelCurrentIfCurrent clears both current identity fields only when
// the supplied stream/SSRC still owns the row. A stale callback therefore
// cannot erase a newer media generation.
func ClearChannelCurrentIfCurrent(c context.Context, streamID, ssrc string) (bool, error) {
	result := app.DB().WithContext(c).Model(&GbChannel{}).
		Where("stream_id = ? AND current_ssrc = ?", streamID, ssrc).
		Updates(map[string]any{"stream_id": "", "current_ssrc": ""})
	return result.RowsAffected == 1, result.Error
}

// FindChannelByStreamID 按当前播放流 ID 查询通道。
func FindChannelByStreamID(c context.Context, streamID string) (*GbChannel, error) {
	var ch GbChannel
	result := app.DB().WithContext(c).
		Where("stream_id = ?", streamID).
		Limit(1).Find(&ch)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ch, nil
}

// ListPlayingChannels 列出所有 DB 认为在播的通道(stream_id != ”).
// 用于 play/reconciler 对账扫描:只查关键字段避免拖慢,不 SELECT * 拉快照 URL 等大字段.
func ListPlayingChannels(c context.Context) (GbChannelList, error) {
	var list GbChannelList
	err := app.DB().WithContext(c).
		Select("id, device_id, channel_id, stream_id, current_ssrc").
		Where("stream_id != ''").
		Find(&list).Error
	return list, err
}

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
	ID           uint       `gorm:"primarykey" json:"id"`
	ChannelID    string     `gorm:"column:channel_id;size:20;comment:通道国标编码" json:"channelId"`
	DeviceID     string     `gorm:"column:device_id;size:20;comment:所属设备编码" json:"deviceId"`
	Name         string     `gorm:"column:name;size:255;comment:通道名称" json:"name"`
	Manufacturer string     `gorm:"column:manufacturer;size:255" json:"manufacturer"`
	Model        string     `gorm:"column:model;size:255" json:"model"`
	Owner        string     `gorm:"column:owner;size:64" json:"owner"`
	CivilCode    string     `gorm:"column:civil_code;size:32;comment:行政区划" json:"civilCode"`
	ParentID     string     `gorm:"column:parent_id;size:20;comment:父节点编码" json:"parentId"`
	PTZType      int8       `gorm:"column:ptz_type;comment:云台类型" json:"ptzType"`
	Longitude    float64    `gorm:"column:longitude;comment:经度" json:"longitude"`
	Latitude     float64    `gorm:"column:latitude;comment:纬度" json:"latitude"`
	Status       int8       `gorm:"column:status;default:0;comment:通道在线" json:"status"`
	StreamID     string     `gorm:"column:stream_id;size:64;comment:当前播放流ID" json:"streamId"`
	// Capabilities A1 新增:通道能力 JSON {audio, h265, night_vision, alarm_io, recording}
	// 用 *string + 默认 NULL — MySQL JSON 列不接受空字符串("The document is empty"),
	// nil 写入 NULL,前端拿到 null 即按"无能力上报"渲染
	Capabilities *string    `gorm:"column:capabilities;type:json;comment:通道能力 JSON" json:"capabilities"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	DeletedAt    *time.Time `gorm:"index" json:"deletedAt"`
	TenantID     uint       `gorm:"column:tenant_id" json:"tenantId"`
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
	return app.DB().WithContext(c).Model(&GbChannel{}).Where("id = ?", existing.ID).Updates(ch).Error
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

package models

import "time"

// GbChannelFavoriteGroup stores a user's channel favorite group.
type GbChannelFavoriteGroup struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OwnerUserID uint      `gorm:"column:owner_user_id;not null;uniqueIndex:uk_gb_channel_favorite_group_owner_name,priority:1;index:idx_gb_channel_favorite_group_owner" json:"ownerUserId"`
	Name        string    `gorm:"column:name;size:64;not null;uniqueIndex:uk_gb_channel_favorite_group_owner_name,priority:2" json:"name"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbChannelFavoriteGroup) TableName() string { return "gb_channel_favorite_group" }

// GbChannelFavoriteItem stores stable device/channel codes and display snapshots.
type GbChannelFavoriteItem struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	GroupID     uint      `gorm:"column:group_id;not null;uniqueIndex:uk_gb_channel_favorite_item_code,priority:1;index:idx_gb_channel_favorite_item_group" json:"groupId"`
	DeviceCode  string    `gorm:"column:device_code;size:64;not null;uniqueIndex:uk_gb_channel_favorite_item_code,priority:2" json:"deviceCode"`
	ChannelCode string    `gorm:"column:channel_code;size:64;not null;uniqueIndex:uk_gb_channel_favorite_item_code,priority:3" json:"channelCode"`
	DeviceName  string    `gorm:"column:device_name;size:255;not null;default:''" json:"deviceName"`
	ChannelName string    `gorm:"column:channel_name;size:255;not null;default:''" json:"channelName"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (GbChannelFavoriteItem) TableName() string { return "gb_channel_favorite_item" }

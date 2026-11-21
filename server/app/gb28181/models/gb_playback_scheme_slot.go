package models

import "time"

// GbPlaybackSchemeSlot stores one occupied cell without transient playback data.
type GbPlaybackSchemeSlot struct {
	ID                  uint      `gorm:"primarykey" json:"id"`
	SchemeID            uint      `gorm:"column:scheme_id;not null;uniqueIndex:uk_playback_scheme_slot,priority:1;index:idx_playback_scheme_slot_scheme" json:"-"`
	SlotIndex           int       `gorm:"column:slot_index;not null;uniqueIndex:uk_playback_scheme_slot,priority:2" json:"slotIndex"`
	DeviceCode          string    `gorm:"column:device_code;size:20;not null" json:"deviceCode"`
	ChannelCode         string    `gorm:"column:channel_code;size:20;not null" json:"channelCode"`
	DeviceNameSnapshot  string    `gorm:"column:device_name_snapshot;size:255;not null" json:"deviceName"`
	ChannelNameSnapshot string    `gorm:"column:channel_name_snapshot;size:255;not null" json:"channelName"`
	CreatedAt           time.Time `gorm:"column:created_at;not null" json:"-"`
}

func (GbPlaybackSchemeSlot) TableName() string { return "gb_playback_scheme_slot" }

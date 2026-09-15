package subscribe

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type PositionProcessor struct {
	db             *gorm.DB
	now            func() time.Time
	historyEnabled func() bool
}

func NewPositionProcessor(db *gorm.DB, now func() time.Time, historyEnabled ...func() bool) *PositionProcessor {
	if now == nil {
		now = time.Now
	}
	enabled := PositionHistoryEnabled
	if len(historyEnabled) > 0 && historyEnabled[0] != nil {
		enabled = historyEnabled[0]
	}
	return &PositionProcessor{db: db, now: now, historyEnabled: enabled}
}

func (p *PositionProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.db == nil || device == nil {
		return fmt.Errorf("位置处理器未就绪")
	}
	position, err := manscdp.ParseMobilePositionNotify(notification.Body)
	if err != nil {
		return err
	}
	if position.Longitude == 0 || position.Latitude == 0 {
		return fmt.Errorf("位置坐标不能为 0")
	}
	eventTime, err := parsePositionTime(position.Time)
	if err != nil {
		return err
	}
	var channel gbmodels.GbChannel
	channelResult := p.db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", device.DeviceID, position.DeviceID).Limit(1).Find(&channel)
	var channelID *uint
	if channelResult.Error == nil && channelResult.RowsAffected > 0 {
		channelID = &channel.ID
	}
	receivedAt := p.now()
	latest := gbmodels.GbMobilePositionLatest{
		DeviceID: device.ID, SourceCode: position.DeviceID, ChannelID: channelID,
		EventTime: eventTime, ReceivedAt: receivedAt, Longitude: position.Longitude, Latitude: position.Latitude,
		Speed: floatPtr(position.Speed), Direction: floatPtr(position.Direction), Altitude: floatPtr(position.Altitude),
	}
	upsertLatest := func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "device_id"}, {Name: "source_code"}},
			DoUpdates: clause.AssignmentColumns([]string{"channel_id", "event_time", "received_at", "longitude", "latitude", "speed", "direction", "altitude", "updated_at"}),
		}).Create(&latest).Error
	}
	if p.historyEnabled == nil || !p.historyEnabled() {
		return upsertLatest(p.db.WithContext(ctx))
	}
	history := gbmodels.GbMobilePositionHistory{
		DeviceID: device.ID, SourceCode: position.DeviceID, ChannelID: channelID,
		EventTime: eventTime, ReceivedAt: receivedAt, Longitude: position.Longitude, Latitude: position.Latitude,
		Speed: floatPtr(position.Speed), Direction: floatPtr(position.Direction), Altitude: floatPtr(position.Altitude),
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertLatest(tx); err != nil {
			return err
		}
		return tx.Create(&history).Error
	})
}

func parsePositionTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("位置时间非法: %q", value)
}

func floatPtr(value float64) *float64 { return &value }

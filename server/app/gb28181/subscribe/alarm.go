package subscribe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type AlarmProcessor struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAlarmProcessor(db *gorm.DB, now func() time.Time) *AlarmProcessor {
	if now == nil {
		now = time.Now
	}
	return &AlarmProcessor{db: db, now: now}
}

func (p *AlarmProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.db == nil || device == nil {
		return fmt.Errorf("报警处理器未就绪")
	}
	alarm, err := manscdp.ParseAlarmNotify(notification.Body)
	if err != nil {
		return err
	}
	var channel gbmodels.GbChannel
	channelResult := p.db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", device.DeviceID, alarm.DeviceID).Limit(1).Find(&channel)
	var channelID *uint
	if channelResult.Error == nil && channelResult.RowsAffected > 0 {
		channelID = &channel.ID
	}
	digest := sha256.Sum256(notification.Body)
	dedupe := notification.CallID + "|" + notification.CSeq + "|" + alarm.SN
	if strings.Trim(dedupe, "|") == "" {
		dedupe = fmt.Sprintf("raw:%x", digest)
	}
	now := p.now()
	row := gbmodels.GbAlarmEvent{
		DeviceID: device.ID, ChannelID: channelID, SourceCode: alarm.DeviceID, SN: alarm.SN,
		Priority: intPtr(alarm.Priority), Method: intPtr(alarm.Method), AlarmType: alarm.AlarmType,
		AlarmTypeParam: alarm.AlarmTypeParam, Description: alarm.Description, CallID: notification.CallID, CSeq: notification.CSeq,
		DedupeKey: dedupe, RawDigest: fmt.Sprintf("%x", digest), RawSummary: truncateRaw(notification.Body), ReceivedAt: now,
	}
	if alarm.AlarmTime != "" {
		when, err := parsePositionTime(alarm.AlarmTime)
		if err != nil {
			return fmt.Errorf("报警时间非法: %w", err)
		}
		row.AlarmTime = &when
	}
	if alarm.Longitude != 0 || alarm.Latitude != 0 {
		row.Longitude, row.Latitude = floatPtr(alarm.Longitude), floatPtr(alarm.Latitude)
	}
	return p.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(&row).Error
}

func (s *Service) OnAlarmMessage(ctx context.Context, deviceCode, callID, cseq string, body []byte) error {
	if s == nil || s.db == nil || strings.TrimSpace(deviceCode) == "" {
		return fmt.Errorf("报警设备编码为空")
	}
	processor, ok := s.processors.Load(gbmodels.SubscriptionKindAlarm)
	if !ok {
		return nil
	}
	var device gbmodels.GbDevice
	result := s.db.WithContext(ctx).Where("device_id = ?", deviceCode).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		return fmt.Errorf("报警设备不存在")
	}
	return processor.(Processor).Process(ctx, &device, Notification{Kind: gbmodels.SubscriptionKindAlarm, DeviceCode: deviceCode, CallID: callID, CSeq: cseq, Body: body})
}

func intPtr(value int) *int { return &value }

func truncateRaw(body []byte) string {
	const limit = 2048
	utf8Body := manscdp.DecodeToUTF8(body)
	if len(utf8Body) <= limit {
		return string(utf8Body)
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(utf8Body[cut]) {
		cut--
	}
	return string(utf8Body[:cut])
}

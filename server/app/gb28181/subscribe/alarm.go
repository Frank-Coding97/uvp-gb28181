package subscribe

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.com/uvp-gb28181/app/gb28181/alarmpopup"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type AlarmProcessor struct {
	db          *gorm.DB
	now         func() time.Time
	saveEnabled func() bool
	relay       AlarmNotificationRelay
	// popupEnabled 是「实时弹出」开关的读取器，nil 时读 gb28181.alarm.popup_enabled。
	// 抽成字段是为了让用例不必依赖全局配置（与 saveEnabled 同一套路）。
	popupEnabled func() bool
}

type AlarmNotificationRelay interface {
	RelayAlarmNotify(context.Context, string, string, string, []byte) error
}

var ErrAlarmNotAccepted = errors.New("alarm was not durably accepted")

func NewAlarmProcessor(db *gorm.DB, now func() time.Time) *AlarmProcessor {
	if now == nil {
		now = time.Now
	}
	return &AlarmProcessor{db: db, now: now, saveEnabled: gbconfig.SaveAlarmMessages}
}

func (p *AlarmProcessor) SetRelay(relay AlarmNotificationRelay) {
	if p != nil {
		p.relay = relay
	}
}

// popupEnabledNow 读「设备告警实时弹出」开关：用例可注入，生产走实时配置。
func (p *AlarmProcessor) popupEnabledNow() bool {
	if p.popupEnabled != nil {
		return p.popupEnabled()
	}
	return gbconfig.AlarmPopupEnabled()
}

func (p *AlarmProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.db == nil || device == nil {
		return fmt.Errorf("报警处理器未就绪")
	}
	alarm, err := manscdp.ParseAlarmNotify(notification.Body)
	if err != nil {
		return err
	}
	// 落库与弹窗是两条独立的链路：「存报警消息」默认关、「实时弹出」默认开。
	persist := p.saveEnabled == nil || p.saveEnabled()
	// ⛔ 只有「开关开着 + 真有人连着 SSE」才去查通道名，否则每条报警白打一次 SQL。
	popup := p.popupEnabledNow() && alarmpopup.SubscriberCount() > 0
	var channel gbmodels.GbChannel
	channelFound := false
	if persist || popup {
		channelResult := p.db.WithContext(ctx).Where("device_id = ? AND channel_id = ?", device.DeviceID, alarm.DeviceID).Limit(1).Find(&channel)
		channelFound = channelResult.Error == nil && channelResult.RowsAffected > 0
	}
	var channelPtr *gbmodels.GbChannel
	if channelFound {
		channelPtr = &channel
	}
	if !persist {
		if popup {
			alarmpopup.Publish(buildPopupEvent(device, channelPtr, alarm, p.now(), 0))
		}
		if p.relay != nil {
			if err := p.relay.RelayAlarmNotify(ctx, device.DeviceID, notification.CallID, notification.CSeq, notification.Body); err != nil {
				return errors.Join(ErrAlarmNotAccepted, err)
			}
		}
		return nil
	}
	var channelID *uint
	if channelPtr != nil {
		channelID = &channelPtr.ID
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
	result := p.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "dedupe_key"}}, DoNothing: true}).Create(&row)
	if result.Error != nil {
		return errors.Join(ErrAlarmNotAccepted, result.Error)
	}
	// ⛔ 只有真正插入成功才弹：同一报文被设备重发时 dedupe_key 冲突、RowsAffected=0，
	// 再弹一次会让"报警刷屏"变成"弹窗刷屏"。
	if popup && result.RowsAffected > 0 {
		alarmpopup.Publish(buildPopupEvent(device, channelPtr, alarm, now, row.ID))
	}
	if p.relay != nil {
		if err := p.relay.RelayAlarmNotify(ctx, device.DeviceID, notification.CallID, notification.CSeq, notification.Body); err != nil {
			return errors.Join(ErrAlarmNotAccepted, err)
		}
	}
	return nil
}

// buildPopupEvent 把已解析的报警拼成前端浮层可直接渲染的形状。
//
// 报警时间在这里**容错**解析：弹窗是一条提示链路，格式不合法的 AlarmTime
// 不该让整条报警被丢掉（落库分支仍保持严格校验、不合法即拒绝）。
func buildPopupEvent(device *gbmodels.GbDevice, channel *gbmodels.GbChannel, alarm *manscdp.AlarmNotify, receivedAt time.Time, alarmID uint64) alarmpopup.Event {
	event := alarmpopup.Event{
		AlarmID:     alarmID,
		DeviceDBID:  device.ID,
		OwnerDeptID: device.OwnerDeptID,
		DeviceCode:  device.DeviceID,
		DeviceName:  device.Name,
		DeviceAlias: device.Alias,
		SourceCode:  alarm.DeviceID,
		Priority:    intPtr(alarm.Priority),
		Method:      intPtr(alarm.Method),
		AlarmType:   alarm.AlarmType,
		// AlarmTypeParam 是字符串，直接透传；前端按需展示。
		AlarmTypeParam: alarm.AlarmTypeParam,
		Description:    alarm.Description,
		ReceivedAt:     receivedAt,
	}
	if channel != nil {
		event.ChannelCode = channel.ChannelID
		event.ChannelName = channel.Name
		event.ChannelAlias = channel.Alias
	}
	if alarm.AlarmTime != "" {
		if when, err := parsePositionTime(alarm.AlarmTime); err == nil {
			event.AlarmTime = &when
		}
	}
	return event
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
	if result.Error != nil {
		return errors.Join(ErrAlarmNotAccepted, result.Error)
	}
	if result.RowsAffected == 0 {
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

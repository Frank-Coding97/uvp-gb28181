package subscribe

import (
	"context"
	"fmt"
	"strings"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type Notification struct {
	Kind              gbmodels.SubscriptionKind
	DeviceCode        string
	CallID            string
	CSeq              string
	Source            string
	SubscriptionState string
	Expires           int
	Body              []byte
}

type Processor interface {
	Process(context.Context, *gbmodels.GbDevice, Notification) error
}

type ProcessorFunc func(context.Context, *gbmodels.GbDevice, Notification) error

func (f ProcessorFunc) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	return f(ctx, device, notification)
}

func (s *Service) SetProcessor(kind gbmodels.SubscriptionKind, processor Processor) {
	if s == nil || !kind.Valid() || processor == nil {
		return
	}
	s.processors.Store(kind, processor)
}

// OnNotify validates a notification against the persisted subscription dialog and dispatches its business payload.
func (s *Service) OnNotify(ctx context.Context, notification Notification) error {
	notification.CallID = strings.TrimSpace(notification.CallID)
	if s == nil || s.db == nil || !notification.Kind.Valid() || strings.TrimSpace(notification.DeviceCode) == "" || notification.CallID == "" {
		return fmt.Errorf("非法订阅通知")
	}
	var sub gbmodels.GbDeviceSubscription
	query := s.db.WithContext(ctx).
		Where("kind = ? AND enabled = ? AND call_id = ?", notification.Kind, true, notification.CallID)
	result := query.Limit(1).Find(&sub)
	if result.Error != nil || result.RowsAffected == 0 {
		return fmt.Errorf("未匹配到活动订阅")
	}
	var device gbmodels.GbDevice
	result = s.db.WithContext(ctx).Where("id = ?", sub.DeviceID).Limit(1).Find(&device)
	if result.Error != nil || result.RowsAffected == 0 {
		return fmt.Errorf("订阅设备不存在")
	}
	if notification.DeviceCode != device.DeviceID {
		var channel gbmodels.GbChannel
		channelResult := s.db.WithContext(ctx).
			Where("device_id = ? AND channel_id = ?", device.DeviceID, notification.DeviceCode).
			Limit(1).Find(&channel)
		if channelResult.Error != nil || channelResult.RowsAffected == 0 {
			return fmt.Errorf("通知设备编码不属于订阅设备")
		}
	}
	if strings.EqualFold(notification.SubscriptionState, "terminated") {
		return s.db.WithContext(ctx).Model(&sub).Updates(map[string]any{
			"status": gbmodels.SubscriptionStatusExpired, "next_action_at": nil,
		}).Error
	}
	if processor, ok := s.processors.Load(notification.Kind); ok {
		if err := processor.(Processor).Process(ctx, &device, notification); err != nil {
			return err
		}
	}
	now := s.now()
	updates := map[string]any{"last_notify_at": now}
	if notification.Expires > 0 {
		expiresAt := now.Add(time.Duration(notification.Expires) * time.Second)
		updates["expires_at"] = expiresAt
	}
	return s.db.WithContext(ctx).Model(&sub).Updates(updates).Error
}

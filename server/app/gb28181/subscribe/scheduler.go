package subscribe

import (
	"context"
	"sync"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type Scheduler struct {
	service  *Service
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewScheduler(service *Service, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Scheduler{service: service, interval: interval}
}

func (s *Scheduler) Start(parent context.Context) {
	if s == nil || s.service == nil || s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			_ = s.service.RunDue(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.cancel = nil
}

// RunDue renews enabled subscriptions whose persisted next_action_at is due.
func (s *Service) RunDue(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := s.now()
	var subs []gbmodels.GbDeviceSubscription
	if err := s.db.WithContext(ctx).
		Where("enabled = ? AND next_action_at IS NOT NULL AND next_action_at <= ?", true, now).
		Find(&subs).Error; err != nil {
		return err
	}
	for _, sub := range subs {
		var device gbmodels.GbDevice
		result := s.db.WithContext(ctx).Where("id = ?", sub.DeviceID).Limit(1).Find(&device)
		if result.Error != nil || result.RowsAffected == 0 {
			continue
		}
		if device.Status != gbmodels.DeviceStatusOnline {
			_ = s.db.WithContext(ctx).Model(&sub).Updates(map[string]any{
				"status": gbmodels.SubscriptionStatusExpired, "next_action_at": nil,
			}).Error
			continue
		}
		_, _ = s.Renew(ctx, &device, sub.Kind)
	}
	return nil
}

// WakeDevice schedules enabled subscriptions after a REGISTER or keepalive recovery without sending inline.
func (s *Service) WakeDevice(ctx context.Context, deviceID uint) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.ApplyGlobalDefaults(ctx, deviceID); err != nil {
		return err
	}
	now := s.now()
	return s.db.WithContext(ctx).Model(&gbmodels.GbDeviceSubscription{}).
		Where("device_id = ? AND enabled = ?", deviceID, true).
		Update("next_action_at", now).Error
}

// WakeDeviceByCode schedules enabled subscriptions after a device recovers online.
func (s *Service) WakeDeviceByCode(ctx context.Context, deviceCode string) error {
	if s == nil || s.db == nil || deviceCode == "" {
		return nil
	}
	var device gbmodels.GbDevice
	result := s.db.WithContext(ctx).Select("id").Where("device_id = ?", deviceCode).Limit(1).Find(&device)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	return s.WakeDevice(ctx, device.ID)
}

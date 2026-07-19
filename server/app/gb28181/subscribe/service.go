package subscribe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type Sender interface {
	SendSubscribe(context.Context, uac.SubscriptionRequest) (uac.SubscriptionResponse, error)
}

const (
	minExpiresSeconds          = 60
	maxExpiresSeconds          = 7 * 24 * 60 * 60
	minPositionIntervalSeconds = 1
	maxPositionIntervalSeconds = 24 * 60 * 60
)

// Service owns all durable subscription state transitions.
type Service struct {
	db         *gorm.DB
	sender     Sender
	now        func() time.Time
	locks      sync.Map // map[string]*sync.Mutex, key is deviceID/kind
	processors sync.Map // map[SubscriptionKind]Processor
}

func NewService(db *gorm.DB, sender Sender, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, sender: sender, now: now}
}

func (s *Service) lock(deviceID uint, kind gbmodels.SubscriptionKind) func() {
	key := fmt.Sprintf("%d/%s", deviceID, kind)
	value, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func subscriptionEvent(kind gbmodels.SubscriptionKind) string {
	if kind == gbmodels.SubscriptionKindCatalog {
		return "Catalog"
	}
	return "presence"
}

func retryDelay(count int) time.Duration {
	switch {
	case count <= 1:
		return time.Minute
	case count == 2:
		return 5 * time.Minute
	case count == 3:
		return 30 * time.Minute
	default:
		return 24 * time.Hour
	}
}

func renewalAt(now time.Time, expires int) time.Time {
	if expires < 600 {
		return now.Add(time.Duration(expires*8/10) * time.Second)
	}
	return now.Add(time.Duration(expires-300) * time.Second)
}

func (s *Service) findOrCreate(ctx context.Context, deviceID uint, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	var sub gbmodels.GbDeviceSubscription
	result := s.db.WithContext(ctx).Where("device_id = ? AND kind = ?", deviceID, kind).Limit(1).Find(&sub)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return &sub, nil
	}
	sub = gbmodels.GbDeviceSubscription{
		DeviceID: deviceID, Kind: kind, Event: subscriptionEvent(kind),
		Status: gbmodels.SubscriptionStatusDisabled, ExpiresSeconds: 3600,
	}
	if kind == gbmodels.SubscriptionKindMobilePosition {
		sub.IntervalSeconds = 30
	}
	if err := s.db.WithContext(ctx).Create(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Service) Enable(ctx context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	enabled := true
	return s.Configure(ctx, device, kind, &enabled, nil, nil)
}

func (s *Service) Renew(ctx context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	if device == nil || device.Status != gbmodels.DeviceStatusOnline {
		return nil, fmt.Errorf("设备离线，无法续订")
	}
	unlock := s.lock(device.ID, kind)
	defer unlock()
	sub, err := s.findOrCreate(ctx, device.ID, kind)
	if err != nil {
		return nil, err
	}
	if !sub.Enabled {
		return sub, fmt.Errorf("订阅未启用")
	}
	return s.send(ctx, device, sub, sub.ExpiresSeconds)
}

func (s *Service) Disable(ctx context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind) (*gbmodels.GbDeviceSubscription, error) {
	enabled := false
	return s.Configure(ctx, device, kind, &enabled, nil, nil)
}

// Configure persists subscription policy and immediately applies it when the subscription is enabled.
func (s *Service) Configure(ctx context.Context, device *gbmodels.GbDevice, kind gbmodels.SubscriptionKind, enabled *bool, expiresSeconds *int, intervalSeconds *int) (*gbmodels.GbDeviceSubscription, error) {
	if s == nil || s.db == nil || device == nil || !kind.Valid() {
		return nil, fmt.Errorf("设备或订阅类型无效")
	}
	if enabled == nil && expiresSeconds == nil && intervalSeconds == nil {
		return nil, fmt.Errorf("未提供订阅配置")
	}
	if expiresSeconds != nil && (*expiresSeconds < minExpiresSeconds || *expiresSeconds > maxExpiresSeconds) {
		return nil, fmt.Errorf("订阅有效期需在 %d-%d 秒之间", minExpiresSeconds, maxExpiresSeconds)
	}
	if intervalSeconds != nil {
		if kind != gbmodels.SubscriptionKindMobilePosition {
			return nil, fmt.Errorf("仅位置订阅支持上报间隔")
		}
		if *intervalSeconds < minPositionIntervalSeconds || *intervalSeconds > maxPositionIntervalSeconds {
			return nil, fmt.Errorf("位置上报间隔需在 %d-%d 秒之间", minPositionIntervalSeconds, maxPositionIntervalSeconds)
		}
	}

	unlock := s.lock(device.ID, kind)
	defer unlock()
	sub, err := s.findOrCreate(ctx, device.ID, kind)
	if err != nil {
		return nil, err
	}

	targetEnabled := sub.Enabled
	if enabled != nil {
		targetEnabled = *enabled
	}
	if targetEnabled && (device.Status != gbmodels.DeviceStatusOnline || s.sender == nil) {
		return nil, fmt.Errorf("设备离线，无法启用或更新订阅")
	}

	updates := map[string]any{}
	if expiresSeconds != nil {
		updates["expires_seconds"] = *expiresSeconds
		sub.ExpiresSeconds = *expiresSeconds
	}
	if intervalSeconds != nil {
		updates["interval_seconds"] = *intervalSeconds
		sub.IntervalSeconds = *intervalSeconds
	}
	if !targetEnabled {
		updates["enabled"] = false
		updates["status"] = gbmodels.SubscriptionStatusDisabled
		updates["next_action_at"] = nil
		if err := s.db.WithContext(ctx).Model(sub).Updates(updates).Error; err != nil {
			return nil, err
		}
		if sub.CallID == "" || device.Status != gbmodels.DeviceStatusOnline || s.sender == nil {
			_ = s.db.WithContext(ctx).First(sub, sub.ID).Error
			return sub, nil
		}
		_, sendErr := s.sendRequest(ctx, device, sub, 0)
		if sendErr != nil {
			_ = s.db.WithContext(ctx).Model(sub).Update("last_error", sendErr.Error()).Error
			_ = s.db.WithContext(ctx).First(sub, sub.ID).Error
			return sub, sendErr
		}
		_ = s.db.WithContext(ctx).First(sub, sub.ID).Error
		return sub, nil
	}

	updates["enabled"] = true
	updates["status"] = gbmodels.SubscriptionStatusPending
	updates["last_error"] = ""
	if err := s.db.WithContext(ctx).Model(sub).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.send(ctx, device, sub, sub.ExpiresSeconds)
}

func (s *Service) send(ctx context.Context, device *gbmodels.GbDevice, sub *gbmodels.GbDeviceSubscription, expires int) (*gbmodels.GbDeviceSubscription, error) {
	now := s.now()
	if err := s.db.WithContext(ctx).Model(sub).Updates(map[string]any{
		"status": gbmodels.SubscriptionStatusPending, "last_subscribe_at": now,
	}).Error; err != nil {
		return sub, err
	}
	response, err := s.sendRequest(ctx, device, sub, expires)
	if err != nil {
		next := now.Add(retryDelay(sub.RetryCount + 1))
		_ = s.db.WithContext(ctx).Model(sub).Updates(map[string]any{
			"status": gbmodels.SubscriptionStatusDegraded, "retry_count": sub.RetryCount + 1,
			"last_error": err.Error(), "next_action_at": next,
		}).Error
		_ = s.db.WithContext(ctx).First(sub, sub.ID).Error
		return sub, err
	}
	actualExpires := response.Expires
	if actualExpires <= 0 {
		actualExpires = sub.ExpiresSeconds
	}
	expiresAt := now.Add(time.Duration(actualExpires) * time.Second)
	next := renewalAt(now, actualExpires)
	if err := s.db.WithContext(ctx).Model(sub).Updates(map[string]any{
		"enabled": true, "status": gbmodels.SubscriptionStatusActive,
		"call_id":   response.CallID,
		"local_tag": response.LocalTag, "remote_tag": response.RemoteTag, "cseq": response.CSeq,
		"expires_at": expiresAt, "next_action_at": next, "retry_count": 0,
		"last_status_code": response.StatusCode, "last_error": "",
	}).Error; err != nil {
		return sub, err
	}
	_ = s.db.WithContext(ctx).First(sub, sub.ID).Error
	return sub, nil
}

func (s *Service) sendRequest(ctx context.Context, device *gbmodels.GbDevice, sub *gbmodels.GbDeviceSubscription, expires int) (uac.SubscriptionResponse, error) {
	body, event, err := manscdp.BuildSubscriptionQuery(sub.Kind, device.DeviceID, int(sub.CSeq+1), sub.IntervalSeconds)
	if err != nil {
		return uac.SubscriptionResponse{}, err
	}
	return s.sender.SendSubscribe(ctx, uac.SubscriptionRequest{
		DeviceID: device.DeviceID, Destination: net.JoinHostPort(device.IP, strconv.Itoa(device.Port)),
		Transport: device.Transport, Event: event, Body: body, Expires: expires,
		CallID: sub.CallID, LocalTag: sub.LocalTag, RemoteTag: sub.RemoteTag, CSeq: sub.CSeq + 1,
	})
}

package device

import (
	"context"
	"fmt"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// onlineKeyPrefix Redis 在线态 key 前缀(cachekeyprefix 由底座统一加,见 config)
const onlineKeyPrefix = "gb28181:device:online:"

// OnlineKey 设备在线态 Redis key
func OnlineKey(deviceID string) string {
	return onlineKeyPrefix + deviceID
}

// RegisterInfo 注册时采集的信息
type RegisterInfo struct {
	DeviceID  string
	Transport string
	IP        string
	Port      int
	Expires   int
}

// HandleRegister 处理注册成功:自动建档(upsert)+ 写 Redis 在线态
// onlineTTL = 心跳周期 × 丢失阈值
func HandleRegister(ctx context.Context, info RegisterInfo, onlineTTL time.Duration) error {
	now := time.Now()
	d := &gbmodels.GbDevice{
		DeviceID:     info.DeviceID,
		Transport:    info.Transport,
		IP:           info.IP,
		Port:         info.Port,
		Expires:      info.Expires,
		RegisterTime: &now,
		Status:       gbmodels.DeviceStatusOnline,
	}
	if err := gbmodels.Upsert(ctx, d); err != nil {
		return fmt.Errorf("自动建档失败: %w", err)
	}
	return markOnline(ctx, info.DeviceID, onlineTTL)
}

// HandleUnregister 处理注销(Expires=0):删在线态 + 落库离线
func HandleUnregister(ctx context.Context, deviceID string) error {
	if app.Cache != nil {
		_ = app.Cache.Del(ctx, OnlineKey(deviceID))
	}
	return gbmodels.UpdateStatus(ctx, deviceID, gbmodels.DeviceStatusOffline)
}

// Keepalive 处理心跳:刷新 Redis 在线态 TTL + 更新 keepalive_time
func Keepalive(ctx context.Context, deviceID string, onlineTTL time.Duration) error {
	now := time.Now()
	if err := app.DB().WithContext(ctx).Model(&gbmodels.GbDevice{}).
		Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{"keepalive_time": now, "status": gbmodels.DeviceStatusOnline}).Error; err != nil {
		return err
	}
	return markOnline(ctx, deviceID, onlineTTL)
}

// IsOnline 查 Redis 在线态
func IsOnline(ctx context.Context, deviceID string) bool {
	if app.Cache == nil {
		return false
	}
	n, err := app.Cache.Exists(ctx, OnlineKey(deviceID))
	return err == nil && n > 0
}

// markOnline 写 Redis 在线标记 + TTL
func markOnline(ctx context.Context, deviceID string, ttl time.Duration) error {
	if app.Cache == nil {
		return nil
	}
	return app.Cache.Set(ctx, OnlineKey(deviceID), "1", ttl)
}

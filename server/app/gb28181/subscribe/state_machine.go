// Package subscribe 实现 GB/T 28181 Subscribe Catalog 智能升降级状态机(Q4 决议)
//
// 三态:unknown → subscribed / fallback
//   - unknown:设备首次注册,未尝试 SUBSCRIBE
//   - subscribed:SUBSCRIBE 成功,等 NOTIFY 推送增量更新
//   - fallback:SUBSCRIBE 失败(或 30min 无 NOTIFY),降级到主动 Query 兜底
//
// 三个 cron:
//   - poller:每 30min 对 fallback 设备主动 CatalogQuery
//   - reconciler:每 60min 对 subscribed 设备对账(Query → diff → anomaly)
//   - retry:每 24h 对 fallback 设备重试 SUBSCRIBE(设备可能升级固件后支持)
//
// notify_handler:SIP NOTIFY 收到 → 投递 catalog.Pipeline.IngestDelta
package subscribe

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// StateMachine 订阅能力状态机
type StateMachine struct {
	db     *gorm.DB
	logger *zap.Logger
	mu     sync.Mutex
}

// NewStateMachine 构造状态机(bootstrap 调用)
func NewStateMachine(db *gorm.DB, logger *zap.Logger) *StateMachine {
	return &StateMachine{db: db, logger: logger}
}

// OnRegister 设备注册成功事件 → 尝试 SUBSCRIBE
//
// 行为:
//   1. 查 subscribe_capability:
//      - unknown → 尝试 SUBSCRIBE(本期模拟:直接标 fallback,真实 SUBSCRIBE 需 UAC 发 SIP 请求)
//      - subscribed → 不动(已订阅,等 NOTIFY)
//      - fallback → 如果距离上次 test > 24h,重试
//   2. 更新 subscribe_last_test = now
func (sm *StateMachine) OnRegister(ctx context.Context, deviceID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var dev gbmodels.GbDevice
	res := sm.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(1).Find(&dev)
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}

	now := time.Now()

	switch dev.SubscribeCapability {
	case gbmodels.SubscribeUnknown:
		// Phase 1 简化:模拟 SUBSCRIBE 失败 → 直接 fallback
		// Phase 2 真正发 SIP SUBSCRIBE 请求 + 等 200 OK,根据 response 决定
		sm.logger.Info("subscribe: trying SUBSCRIBE (simulated)",
			zap.String("deviceId", deviceID))
		sm.transition(ctx, &dev, gbmodels.SubscribeFallback, now)

	case gbmodels.SubscribeFallback:
		// 距离上次 test > 24h 才重试
		if dev.SubscribeLastTest != nil && time.Since(*dev.SubscribeLastTest) < 24*time.Hour {
			return
		}
		sm.logger.Info("subscribe: retrying SUBSCRIBE for fallback device",
			zap.String("deviceId", deviceID))
		sm.transition(ctx, &dev, gbmodels.SubscribeFallback, now)

	case gbmodels.SubscribeSubscribed:
		// 已订阅,不动;只刷 last_test
		sm.db.WithContext(ctx).Model(&dev).Update("subscribe_last_test", now)
	}
}

// OnNotify 收到 NOTIFY → 设备确认支持 SUBSCRIBE
//
// 行为:如果设备当前不是 subscribed,提升为 subscribed
func (sm *StateMachine) OnNotify(ctx context.Context, deviceID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var dev gbmodels.GbDevice
	res := sm.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(1).Find(&dev)
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}

	if dev.SubscribeCapability == gbmodels.SubscribeSubscribed {
		// 刷新过期时间(续订,30min 内有 NOTIFY 就保持 subscribed)
		now := time.Now()
		expires := now.Add(30 * time.Minute)
		sm.db.WithContext(ctx).Model(&dev).Updates(map[string]any{
			"subscribe_last_test":  now,
			"subscribe_expires_at": expires,
		})
		return
	}

	// 升级到 subscribed
	now := time.Now()
	expires := now.Add(30 * time.Minute)
	sm.logger.Info("subscribe: device confirmed SUBSCRIBE support, upgrading",
		zap.String("deviceId", deviceID),
		zap.String("from", string(dev.SubscribeCapability)))
	sm.db.WithContext(ctx).Model(&dev).Updates(map[string]any{
		"subscribe_capability":  gbmodels.SubscribeSubscribed,
		"subscribe_last_test":   now,
		"subscribe_expires_at":  expires,
	})
}

// DegradeToFallback 降级(超时无 NOTIFY / reconciler 发现不一致)
func (sm *StateMachine) DegradeToFallback(ctx context.Context, deviceID, reason string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var dev gbmodels.GbDevice
	res := sm.db.WithContext(ctx).Where("device_id = ?", deviceID).Limit(1).Find(&dev)
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}

	if dev.SubscribeCapability == gbmodels.SubscribeFallback {
		return // 已经是 fallback
	}

	sm.logger.Warn("subscribe: degrading to fallback",
		zap.String("deviceId", deviceID), zap.String("reason", reason))
	now := time.Now()
	sm.transition(ctx, &dev, gbmodels.SubscribeFallback, now)
}

func (sm *StateMachine) transition(ctx context.Context, dev *gbmodels.GbDevice, target gbmodels.SubscribeCapability, now time.Time) {
	sm.db.WithContext(ctx).Model(dev).Updates(map[string]any{
		"subscribe_capability": target,
		"subscribe_last_test":  now,
	})
}

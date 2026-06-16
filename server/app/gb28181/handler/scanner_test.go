package handler_test

import (
	"context"
	"testing"
	"time"

	gbdevice "uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// TestOfflineScan_TimeoutToOffline T6-测1(AC-5): Redis 在线态消失 → 扫描置离线
func TestOfflineScan_TimeoutToOffline(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000077"
	cleanupDevice(did)
	defer cleanupDevice(did)
	ctx := context.Background()

	// 建一个"在线"设备(MySQL status=1),但不写 Redis 在线态(模拟 TTL 已过期)
	now := time.Now()
	_ = gbmodels.Upsert(ctx, &gbmodels.GbDevice{
		DeviceID: did, Status: gbmodels.DeviceStatusOnline, RegisterTime: &now,
	})
	_ = app.Cache.Del(ctx, gbdevice.OnlineKey(did)) // 确保无在线态

	// 扫描一次
	scanner := gbdevice.NewOfflineScanner(30)
	scanner.ScanOnceForTest()

	d, _ := gbmodels.FindByDeviceID(ctx, did)
	if d == nil || d.Status != gbmodels.DeviceStatusOffline {
		t.Errorf("期望扫描后置离线,实际 status=%v", d)
	}
}

// TestOfflineScan_OnlineNotKilled T6-测3: 在线设备(Redis 有在线态)不被误判离线
func TestOfflineScan_OnlineNotKilled(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000076"
	cleanupDevice(did)
	defer cleanupDevice(did)
	ctx := context.Background()

	now := time.Now()
	_ = gbmodels.Upsert(ctx, &gbmodels.GbDevice{
		DeviceID: did, Status: gbmodels.DeviceStatusOnline, RegisterTime: &now,
	})
	// 写 Redis 在线态(模拟设备活着)
	_ = app.Cache.Set(ctx, gbdevice.OnlineKey(did), "1", 180*time.Second)

	scanner := gbdevice.NewOfflineScanner(30)
	scanner.ScanOnceForTest()

	d, _ := gbmodels.FindByDeviceID(ctx, did)
	if d == nil || d.Status != gbmodels.DeviceStatusOnline {
		t.Errorf("在线设备不应被置离线,实际 status=%v", d)
	}
}

package handler_test

import (
	"context"
	"testing"
	"time"

	gbdevice "uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// TestOfflineScan_TimeoutToOffline T6-测1(AC-5): keepalive_time 超时 → 扫描置离线
func TestOfflineScan_TimeoutToOffline(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000077"
	cleanupDevice(did)
	defer cleanupDevice(did)
	ctx := context.Background()

	// 建一个"在线"设备,但 keepalive_time 是 10 分钟前(远超 60×3+15=195s 阈值)
	old := time.Now().Add(-10 * time.Minute)
	_ = gbmodels.Upsert(ctx, &gbmodels.GbDevice{
		DeviceID: did, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60,
	})

	// 扫描(容忍3次,宽限15s)
	scanner := gbdevice.NewOfflineScanner(30, 3, 15)
	scanner.ScanOnceForTest()

	d, _ := gbmodels.FindByDeviceID(ctx, did)
	if d == nil || d.Status != gbmodels.DeviceStatusOffline {
		t.Errorf("期望扫描后置离线,实际 status=%v", d)
	}
	if d != nil && d.OfflineAt == nil {
		t.Error("置离线后 offline_at 未记录")
	}
}

// TestOfflineScan_OnlineNotKilled T6-测3: keepalive_time 新鲜的设备不被误判离线
func TestOfflineScan_OnlineNotKilled(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000076"
	cleanupDevice(did)
	defer cleanupDevice(did)
	ctx := context.Background()

	// keepalive_time = 刚刚(在阈值内)
	now := time.Now()
	_ = gbmodels.Upsert(ctx, &gbmodels.GbDevice{
		DeviceID: did, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &now, KeepaliveInterval: 60,
	})

	scanner := gbdevice.NewOfflineScanner(30, 3, 15)
	scanner.ScanOnceForTest()

	d, _ := gbmodels.FindByDeviceID(ctx, did)
	if d == nil || d.Status != gbmodels.DeviceStatusOnline {
		t.Errorf("在线设备不应被置离线,实际 status=%v", d)
	}
}

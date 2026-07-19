package handler_test

import (
	"context"
	"testing"
	"time"

	gbdevice "uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
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

// TestOfflineScan_CascadesChannels 设备超时离线时,其通道也必须先置离线,
// 等重新收到 Catalog 的 ON/OFF 后再恢复各自状态。
func TestOfflineScan_CascadesChannels(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000078"
	cleanupDevice(did)
	defer cleanupDevice(did)
	defer app.GormDbMysql.Unscoped().Where("device_id = ?", did).Delete(&gbmodels.GbChannel{})
	ctx := context.Background()

	old := time.Now().Add(-10 * time.Minute)
	_ = gbmodels.Upsert(ctx, &gbmodels.GbDevice{
		DeviceID: did, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60,
	})
	for _, channelID := range []string{"34020000001310000078", "34020000001310000079"} {
		if err := app.GormDbMysql.Create(&gbmodels.GbChannel{
			DeviceID: did, ChannelID: channelID, Name: channelID,
			Status: gbmodels.ChannelStatusOnline,
		}).Error; err != nil {
			t.Fatalf("创建测试通道失败: %v", err)
		}
	}

	gbdevice.NewOfflineScanner(30, 3, 15).ScanOnceForTest()

	var channels []gbmodels.GbChannel
	if err := app.GormDbMysql.Where("device_id = ?", did).Find(&channels).Error; err != nil {
		t.Fatalf("查询测试通道失败: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("期望 2 个测试通道,实际 %d", len(channels))
	}
	for _, channel := range channels {
		if channel.Status != gbmodels.ChannelStatusOffline {
			t.Errorf("设备离线后通道 %s 仍为在线 status=%d", channel.ChannelID, channel.Status)
		}
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

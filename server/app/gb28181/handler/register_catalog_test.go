package handler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// fakeTrigger 计数版,验证调用次数(替代真实 UAC,绕开网络)
type fakeTrigger struct {
	calls         atomic.Int32
	lastID        atomic.Value // string
	lastDest      atomic.Value // string
	lastTransport atomic.Value // string
}

func (f *fakeTrigger) Trigger(_ context.Context, deviceID, dest, transport string) {
	f.calls.Add(1)
	f.lastID.Store(deviceID)
	f.lastDest.Store(dest)
	f.lastTransport.Store(transport)
}

type fakeSubscriptionWaker struct {
	calls  atomic.Int32
	lastID atomic.Value // string
}

func (f *fakeSubscriptionWaker) WakeDeviceByCode(_ context.Context, deviceID string) error {
	f.calls.Add(1)
	f.lastID.Store(deviceID)
	return nil
}

// startServerWithTrigger 起一个 SIP server,并把 RegisterHandler 的 trigger 替换成测试用的
func startServerWithTrigger(t *testing.T, ft handler.CatalogTrigger) func() {
	return startServerWithRecoveryTrigger(t, ft, nil)
}

func startServerWithRecoveryTrigger(t *testing.T, ft handler.CatalogTrigger, waker handler.SubscriptionWaker) func() {
	srv, err := gbsip.NewServer(testCfg())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetCatalogTrigger(ft)
	srv.SetSubscriptionWaker(waker)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// TestRegisterFirstTriggersCatalog T3补:首次注册成功 → 调一次 Catalog trigger
func TestRegisterFirstTriggersCatalog(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000091"
	cleanupDevice(did)
	defer cleanupDevice(did)

	ft := &fakeTrigger{}
	stop := startServerWithTrigger(t, ft)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("期望 200,实际 %d", code)
	}
	if !waitCalls(&ft.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("首次注册应触发 1 次 Catalog,实际 %d", ft.calls.Load())
	}
	if got, _ := ft.lastID.Load().(string); got != did {
		t.Errorf("trigger 收到 deviceID 不符: %q != %q", got, did)
	}
}

// TestRegisterRefreshSkipsCatalog T3补:已在线设备刷新注册 → 不再触发
func TestRegisterRefreshSkipsCatalog(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000092"
	cleanupDevice(did)
	defer cleanupDevice(did)

	ft := &fakeTrigger{}
	stop := startServerWithTrigger(t, ft)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第一次期望 200,实际 %d", code)
	}
	if !waitCalls(&ft.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("第一次应触发 1 次,实际 %d", ft.calls.Load())
	}

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第二次期望 200,实际 %d", code)
	}
	time.Sleep(400 * time.Millisecond)
	if got := ft.calls.Load(); got != 1 {
		t.Errorf("刷新注册不应再触发,期望 1 实际 %d", got)
	}
}

// TestRegisterReOnlineTriggersCatalog T3补:离线后再注册 → 视为新注册,再触发
func TestRegisterReOnlineTriggersCatalog(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000093"
	cleanupDevice(did)
	defer cleanupDevice(did)

	ft := &fakeTrigger{}
	stop := startServerWithTrigger(t, ft)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第一次注册失败: %d", code)
	}
	if !waitCalls(&ft.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("首次应触发,实际 %d", ft.calls.Load())
	}

	if err := gbmodels.MarkOffline(context.Background(), did); err != nil {
		t.Fatalf("模拟离线失败: %v", err)
	}

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第二次注册失败: %d", code)
	}
	if !waitCalls(&ft.calls, 2, 1500*time.Millisecond) {
		t.Errorf("离线后再注册应再触发,期望 2 实际 %d", ft.calls.Load())
	}
}

func TestRegisterReOnlineWakesSubscriptions(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000096"
	cleanupDevice(did)
	defer cleanupDevice(did)

	ft := &fakeTrigger{}
	waker := &fakeSubscriptionWaker{}
	stop := startServerWithRecoveryTrigger(t, ft, waker)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第一次注册失败: %d", code)
	}
	if !waitCalls(&waker.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("首次上线应唤醒订阅,实际 %d", waker.calls.Load())
	}
	if got, _ := waker.lastID.Load().(string); got != did {
		t.Errorf("waker 收到 deviceID 不符: %q != %q", got, did)
	}

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("在线续注册失败: %d", code)
	}
	time.Sleep(400 * time.Millisecond)
	if got := waker.calls.Load(); got != 1 {
		t.Errorf("在线续注册不应重复唤醒,期望 1 实际 %d", got)
	}

	if err := gbmodels.MarkOffline(context.Background(), did); err != nil {
		t.Fatalf("模拟离线失败: %v", err)
	}
	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("恢复注册失败: %d", code)
	}
	if !waitCalls(&waker.calls, 2, 1500*time.Millisecond) {
		t.Errorf("离线恢复应再次唤醒订阅,实际 %d", waker.calls.Load())
	}
}

// TestKeepaliveReOnlineTriggersCatalog 离线设备仅靠 Keepalive 恢复在线时,
// 也必须重新拉 Catalog,不能只恢复设备状态而让通道永远停在离线。
// skipIfChannelSnapshotStale 通道快照 T5 加:测试库缺 snapshot_url 列时 skip
// (共享测试库 schema 落后代码时的兜底,pending 跑 migration 2026-07-20-channel-snapshot.sql)
func skipIfChannelSnapshotStale(t *testing.T) {
	t.Helper()
	if app.GormDbMysql != nil && !app.GormDbMysql.Migrator().HasColumn(&gbmodels.GbChannel{}, "snapshot_url") {
		t.Skip("跳过(MySQL gb_channel 缺 snapshot_url 列,请先跑 migration 2026-07-20-channel-snapshot.sql)")
	}
}

func TestKeepaliveReOnlineTriggersCatalog(t *testing.T) {
	skipIfChannelSnapshotStale(t)
	setupEnv(t)
	const did = "34020000001320000094"
	cleanupDevice(did)
	defer cleanupDevice(did)
	defer app.GormDbMysql.Unscoped().Where("device_id = ?", did).Delete(&gbmodels.GbChannel{})

	ft := &fakeTrigger{}
	stop := startServerWithTrigger(t, ft)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第一次注册失败: %d", code)
	}
	if !waitCalls(&ft.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("首次应触发 1 次,实际 %d", ft.calls.Load())
	}
	if err := app.GormDbMysql.Create(&gbmodels.GbChannel{
		DeviceID: did, ChannelID: "34020000001310000094", Name: "测试通道",
		Status: gbmodels.ChannelStatusOnline,
	}).Error; err != nil {
		t.Fatalf("创建测试通道失败: %v", err)
	}
	if err := gbmodels.MarkOffline(context.Background(), did); err != nil {
		t.Fatalf("模拟离线失败: %v", err)
	}

	var channel gbmodels.GbChannel
	if err := app.GormDbMysql.Where("device_id = ?", did).First(&channel).Error; err != nil {
		t.Fatalf("查询测试通道失败: %v", err)
	}
	if channel.Status != gbmodels.ChannelStatusOffline {
		t.Fatalf("设备离线后通道应为离线,实际 status=%d", channel.Status)
	}

	if code := sendKeepalive(t, did); code != 200 {
		t.Fatalf("离线后心跳期望 200,实际 %d", code)
	}
	if !waitCalls(&ft.calls, 2, 1500*time.Millisecond) {
		t.Fatalf("离线后心跳恢复应再次触发 Catalog,实际 %d", ft.calls.Load())
	}

	if err := app.GormDbMysql.First(&channel, channel.ID).Error; err != nil {
		t.Fatalf("重新查询测试通道失败: %v", err)
	}
	if channel.Status != gbmodels.ChannelStatusOffline {
		t.Fatalf("Catalog 返回前不应凭设备上线直接恢复通道,实际 status=%d", channel.Status)
	}
}

func TestKeepaliveReOnlineWakesSubscriptions(t *testing.T) {
	setupEnv(t)
	const did = "34020000001320000097"
	cleanupDevice(did)
	defer cleanupDevice(did)

	ft := &fakeTrigger{}
	waker := &fakeSubscriptionWaker{}
	stop := startServerWithRecoveryTrigger(t, ft, waker)
	defer stop()

	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("第一次注册失败: %d", code)
	}
	if !waitCalls(&waker.calls, 1, 1500*time.Millisecond) {
		t.Fatalf("首次上线应唤醒订阅,实际 %d", waker.calls.Load())
	}
	if err := gbmodels.MarkOffline(context.Background(), did); err != nil {
		t.Fatalf("模拟离线失败: %v", err)
	}

	if code := sendKeepalive(t, did); code != 200 {
		t.Fatalf("恢复心跳失败: %d", code)
	}
	if !waitCalls(&waker.calls, 2, 1500*time.Millisecond) {
		t.Errorf("离线恢复心跳应再次唤醒订阅,实际 %d", waker.calls.Load())
	}

	if code := sendKeepalive(t, did); code != 200 {
		t.Fatalf("在线心跳失败: %d", code)
	}
	time.Sleep(400 * time.Millisecond)
	if got := waker.calls.Load(); got != 2 {
		t.Errorf("在线心跳不应重复唤醒,期望 2 实际 %d", got)
	}
}

// TestRegisterUnregisterMarksDeviceOffline REGISTER Expires=0 后设备与通道都应立即离线。
func TestRegisterUnregisterMarksDeviceOffline(t *testing.T) {
	skipIfChannelSnapshotStale(t)
	setupEnv(t)
	const did = "34020000001320000095"
	cleanupDevice(did)
	defer cleanupDevice(did)
	defer app.GormDbMysql.Unscoped().Where("device_id = ?", did).Delete(&gbmodels.GbChannel{})

	stop := startTestServer(t, testCfg())
	defer stop()
	if code := doRegister(t, did, testPassword, 3600); code != 200 {
		t.Fatalf("首次注册失败: %d", code)
	}
	if err := app.GormDbMysql.Create(&gbmodels.GbChannel{
		DeviceID: did, ChannelID: "34020000001310000095", Name: "测试通道",
		Status: gbmodels.ChannelStatusOnline,
	}).Error; err != nil {
		t.Fatalf("创建测试通道失败: %v", err)
	}

	if code := doRegister(t, did, testPassword, 0); code != 200 {
		t.Fatalf("注销请求失败: %d", code)
	}
	d, err := gbmodels.FindByDeviceID(context.Background(), did)
	if err != nil || d == nil {
		t.Fatalf("查询注销后的设备失败: err=%v device=%v", err, d)
	}
	if d.Status != gbmodels.DeviceStatusOffline {
		t.Fatalf("注销后设备应为离线,实际 status=%d", d.Status)
	}
	var channel gbmodels.GbChannel
	if err := app.GormDbMysql.Where("device_id = ?", did).First(&channel).Error; err != nil {
		t.Fatalf("查询注销后的通道失败: %v", err)
	}
	if channel.Status != gbmodels.ChannelStatusOffline {
		t.Fatalf("注销后通道应为离线,实际 status=%d", channel.Status)
	}
}

func waitCalls(c *atomic.Int32, target int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.Load() >= target {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return c.Load() >= target
}

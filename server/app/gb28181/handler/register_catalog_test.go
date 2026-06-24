package handler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
)

// fakeTrigger 计数版,验证调用次数(替代真实 UAC,绕开网络)
type fakeTrigger struct {
	calls    atomic.Int32
	lastID   atomic.Value // string
	lastDest atomic.Value // string
}

func (f *fakeTrigger) Trigger(_ context.Context, deviceID, dest string) {
	f.calls.Add(1)
	f.lastID.Store(deviceID)
	f.lastDest.Store(dest)
}

// startServerWithTrigger 起一个 SIP server,并把 RegisterHandler 的 trigger 替换成测试用的
func startServerWithTrigger(t *testing.T, ft handler.CatalogTrigger) func() {
	srv, err := gbsip.NewServer(testCfg())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetCatalogTrigger(ft)
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

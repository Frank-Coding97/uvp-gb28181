package play

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// TestMain 初始化测试环境:app.ZapLog 必须非 nil 才能安全调用 Info/Warn
func TestMain(m *testing.M) {
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
	os.Exit(m.Run())
}

// ===== mocks =====

type mockZLM struct {
	openCalls     atomic.Int32
	lastOnlyTrack atomic.Int32
	closeCalls    atomic.Int32
	openErr       error
	closeErr      error
	port          int
	online        atomic.Bool
	onlineCalls   atomic.Int32
	onlineErr     error
}

func (m *mockZLM) OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int, onlyTrack int) (*zlm.OpenRtpServerResult, error) {
	m.openCalls.Add(1)
	m.lastOnlyTrack.Store(int32(onlyTrack))
	if m.openErr != nil {
		return nil, m.openErr
	}
	p := m.port
	if p == 0 {
		p = 40000
	}
	return &zlm.OpenRtpServerResult{Port: p}, nil
}
func (m *mockZLM) CloseRtpServer(ctx context.Context, streamID string) error {
	m.closeCalls.Add(1)
	return m.closeErr
}
func (m *mockZLM) IsMediaOnline(ctx context.Context, app, stream string) (bool, error) {
	m.onlineCalls.Add(1)
	return m.online.Load(), m.onlineErr
}

type mockInviter struct {
	inviteCalls atomic.Int32
	byeCalls    atomic.Int32
	inviteErr   error
	byeErr      error
	onInvite    func(*uac.Session)
	lastBody    string
}

func (m *mockInviter) Invite(ctx context.Context, sm *uac.SessionManager, s *uac.Session, body string) error {
	m.inviteCalls.Add(1)
	m.lastBody = body
	if m.onInvite != nil {
		m.onInvite(s)
	}
	return m.inviteErr
}
func (m *mockInviter) Bye(ctx context.Context, sm *uac.SessionManager, streamID string) error {
	m.byeCalls.Add(1)
	return m.byeErr
}

type fakeDevices struct{ d *gbmodels.GbDevice }

func (f fakeDevices) FindByDeviceID(ctx context.Context, deviceID string) (*gbmodels.GbDevice, error) {
	return f.d, nil
}

type fakeChannels struct {
	c               *gbmodels.GbChannel
	updateErr       error
	clearErr        error
	clearedStreamID string
}

func (f *fakeChannels) FindChannel(ctx context.Context, deviceID, channelID string) (*gbmodels.GbChannel, error) {
	return f.c, nil
}

func (f *fakeChannels) FindChannelByStream(ctx context.Context, streamID string) (*gbmodels.GbChannel, error) {
	if f.c != nil && f.c.StreamID == streamID {
		return f.c, nil
	}
	return nil, nil
}

func (f *fakeChannels) UpdateStream(ctx context.Context, deviceID, channelID, streamID string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.c != nil {
		f.c.StreamID = streamID
	}
	return nil
}

func (f *fakeChannels) ClearStream(ctx context.Context, streamID string) error {
	f.clearedStreamID = streamID
	if f.clearErr != nil {
		return f.clearErr
	}
	if f.c != nil && f.c.StreamID == streamID {
		f.c.StreamID = ""
	}
	return nil
}

// ===== fixtures =====

func onlineDevice() *gbmodels.GbDevice {
	return &gbmodels.GbDevice{
		DeviceID: "34020000001320000002",
		IP:       "192.168.10.203",
		Port:     5060,
		Status:   gbmodels.DeviceStatusOnline,
	}
}
func aChannel() *gbmodels.GbChannel {
	return &gbmodels.GbChannel{DeviceID: "34020000001320000002", ChannelID: "12345678911116666661"}
}

func testCfg() gbconfig.Config {
	return gbconfig.Config{
		SIP:   gbconfig.SIPConfig{ServerID: "34020000002000000001", Domain: "3402000000"},
		ZLM:   gbconfig.ZLMConfig{Host: "192.168.10.222", HTTPPort: 80, RTPPort: 40000},
		Media: gbconfig.MediaConfig{StreamNoneReaderTimeout: 20},
	}
}

func newSvc(t *testing.T, z ZLM, inv Inviter, dev *gbmodels.GbDevice, ch *gbmodels.GbChannel) (*Service, *stream.Notifier, *fakeChannels) {
	t.Helper()
	n := stream.NewNotifier()
	sm := uac.NewSessionManager()
	channels := &fakeChannels{c: ch}
	s := New(testCfg(), z, inv, sm, n, fakeDevices{dev}, channels)
	s.SetReadyTimings(800*time.Millisecond, 50*time.Millisecond)
	return s, n, channels
}

// ===== tests =====

// TestStartHappyPath T6.3-测1: 在线设备 + 通道 + hook 100ms 内到 → 返回播放地址
func TestStartHappyPath(t *testing.T) {
	z := &mockZLM{port: 40000} // online 始终 false,只能靠 hook
	inv := &mockInviter{}
	ch := aChannel()
	s, n, _ := newSvc(t, z, inv, onlineDevice(), ch)

	// INVITE 之后 100ms 让 hook 触发(以 session 里的 StreamID 为准)
	inv.onInvite = func(sess *uac.Session) {
		go func(streamID string) {
			time.Sleep(100 * time.Millisecond)
			n.Publish(streamID)
		}(sess.StreamID)
	}

	res, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("应成功;err=%v", err)
	}
	if res.StreamID == "" || res.SSRC == "" {
		t.Errorf("StreamID/SSRC 应非空: %+v", res)
	}
	if res.WSFlvURL == "" {
		t.Errorf("WSFlvURL 应非空: %+v", res)
	}
	if z.closeCalls.Load() != 0 {
		t.Errorf("happy path 不应回滚 closeRtpServer,实际 %d", z.closeCalls.Load())
	}
	if ch.StreamID != res.StreamID {
		t.Errorf("点播成功应记录通道 stream_id, got %q want %q", ch.StreamID, res.StreamID)
	}
}

func TestStartUsesSeparateReceiveAndPlaybackHosts(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	dev, ch := onlineDevice(), aChannel()
	notifier := stream.NewNotifier()
	sm := uac.NewSessionManager()
	channels := &fakeChannels{c: ch}
	cfg := testCfg()
	cfg.ZLM.ReceiveHost = "203.0.113.10"
	cfg.ZLM.PlaybackHost = "play.example.com"
	s := New(cfg, z, inv, sm, notifier, fakeDevices{dev}, channels)
	s.SetReadyTimings(800*time.Millisecond, 50*time.Millisecond)
	inv.onInvite = func(sess *uac.Session) {
		go func(streamID string) {
			time.Sleep(50 * time.Millisecond)
			notifier.Publish(streamID)
		}(sess.StreamID)
	}

	result, err := s.Start(context.Background(), dev.DeviceID, ch.ChannelID)
	if err != nil {
		t.Fatalf("Start 应成功: %v", err)
	}
	if !strings.Contains(inv.lastBody, "c=IN IP4 203.0.113.10\r\n") {
		t.Fatalf("SDP 应使用设备收流地址:\n%s", inv.lastBody)
	}
	if !strings.HasPrefix(result.WSFlvURL, "ws://play.example.com:") {
		t.Fatalf("播放地址应使用播放访问地址: %q", result.WSFlvURL)
	}
}

func TestStartDisablesAudioPerChannel(t *testing.T) {
	dev := &gbmodels.GbDevice{DeviceID: "34020000001320000002", Status: gbmodels.DeviceStatusOnline, IP: "127.0.0.1", Port: 5060}
	ch := &gbmodels.GbChannel{DeviceID: dev.DeviceID, ChannelID: "12345678911116666661", AudioEnabled: false}
	z := &mockZLM{}
	inv := &mockInviter{}
	s, notifier, _ := newSvc(t, z, inv, dev, ch)
	inv.onInvite = func(sess *uac.Session) { notifier.Publish(sess.StreamID) }
	z.online.Store(true)

	if _, err := s.Start(context.Background(), dev.DeviceID, ch.ChannelID); err != nil {
		t.Fatalf("Start 应成功: %v", err)
	}
	if got := z.lastOnlyTrack.Load(); got != 2 {
		t.Fatalf("关闭音频时 only_track 应为2,实际%d", got)
	}
}

// TestStartDeviceOffline T6.3-测2: 设备离线 → 直接拒绝,不开 RTP
func TestStartDeviceOffline(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	dev := onlineDevice()
	dev.Status = gbmodels.DeviceStatusOffline
	s, _, _ := newSvc(t, z, inv, dev, aChannel())

	_, err := s.Start(context.Background(), dev.DeviceID, "any-channel")
	if !errors.Is(err, ErrDeviceOffline) {
		t.Errorf("应返 ErrDeviceOffline,实际 %v", err)
	}
	if z.openCalls.Load() != 0 {
		t.Error("设备离线不应调用 openRtpServer")
	}
	if inv.inviteCalls.Load() != 0 {
		t.Error("设备离线不应发 INVITE")
	}
}

// TestStartChannelNotFound T6.3-测3: 通道不存在 → 直接拒绝
func TestStartChannelNotFound(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), nil) // 没通道

	_, err := s.Start(context.Background(), "34020000001320000002", "no-such-channel")
	if !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("应返 ErrChannelNotFound,实际 %v", err)
	}
	if z.openCalls.Load() != 0 {
		t.Error("通道不存在不应 openRtpServer")
	}
}

// TestStartInviteFailRollsBackRtp T6.3-测4: INVITE 失败 → 自动 closeRtpServer 回滚
func TestStartInviteFailRollsBackRtp(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{inviteErr: errors.New("设备拒绝")}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())

	_, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err == nil {
		t.Fatal("INVITE 失败应返错")
	}
	if z.openCalls.Load() != 1 {
		t.Errorf("应申请过 RTP 端口,实际 %d", z.openCalls.Load())
	}
	if z.closeCalls.Load() != 1 {
		t.Errorf("INVITE 失败应回滚 RTP 端口,实际 close=%d", z.closeCalls.Load())
	}
	if inv.byeCalls.Load() != 0 {
		t.Error("INVITE 失败前没建立会话,不应发 BYE")
	}
}

// TestStartStreamNotReadyRollsBackAll T6.3-测5: 流就绪等待超时 → 发 BYE + 关 RTP
func TestStartStreamNotReadyRollsBackAll(t *testing.T) {
	z := &mockZLM{port: 40000} // online 永远 false
	inv := &mockInviter{}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	// hook 也永不来

	_, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if !errors.Is(err, ErrStreamNotReady) {
		t.Errorf("应返 ErrStreamNotReady,实际 %v", err)
	}
	if z.closeCalls.Load() != 1 {
		t.Errorf("超时应关 RTP,实际 %d", z.closeCalls.Load())
	}
	if inv.byeCalls.Load() != 1 {
		t.Errorf("超时应发 BYE,实际 %d", inv.byeCalls.Load())
	}
}

// TestStartReadyViaPolling T6.3-测6: hook 不来,轮询发现 online=true → 仍成功
func TestStartReadyViaPolling(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, _, _ := newSvc(t, z, inv, onlineDevice(), aChannel())

	// INVITE 后 150ms 让 ZLM "上线"
	inv.onInvite = func(_ *uac.Session) {
		go func() {
			time.Sleep(150 * time.Millisecond)
			z.online.Store(true)
		}()
	}

	res, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("应通过轮询发现就绪;err=%v", err)
	}
	if res.StreamID == "" {
		t.Error("应返 streamID")
	}
	if z.onlineCalls.Load() < 2 {
		t.Errorf("应轮询多次(立即+ticker),实际 %d", z.onlineCalls.Load())
	}
}

// TestStop T6.3-测7: Stop 同时发 BYE + closeRtpServer
func TestStop(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	s, _, channels := newSvc(t, z, inv, onlineDevice(), aChannel())
	if err := s.Stop(context.Background(), "fake-stream"); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if inv.byeCalls.Load() != 1 || z.closeCalls.Load() != 1 {
		t.Errorf("应各调一次,bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
	if channels.clearedStreamID != "fake-stream" {
		t.Errorf("Stop 应清空通道 stream_id,实际 %q", channels.clearedStreamID)
	}
}

func TestStartUpdateStreamFailRollsBackAll(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, n, channels := newSvc(t, z, inv, onlineDevice(), aChannel())
	channels.updateErr = errors.New("db unavailable")
	inv.onInvite = func(sess *uac.Session) {
		go n.Publish(sess.StreamID)
	}

	_, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err == nil {
		t.Fatal("记录 stream_id 失败时应返回错误")
	}
	if inv.byeCalls.Load() != 1 || z.closeCalls.Load() != 1 {
		t.Errorf("记录失败应回滚 BYE 和 RTP, bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
}

// ===== 通道快照 T4 tests =====

type fakeSnapshotSvc struct {
	called   atomic.Int32
	lastArgs atomic.Value // snapshotArgs
}
type snapshotArgs struct {
	nodeID, streamID, deviceID, channelID string
}

func (f *fakeSnapshotSvc) FireAfterPlay(ctx context.Context, nodeID, streamID, deviceID, channelID string) {
	f.called.Add(1)
	f.lastArgs.Store(snapshotArgs{nodeID, streamID, deviceID, channelID})
}

// TestStartTriggersSnapshotAfterWaitReady 通道快照 T4:play 成功后应异步调 FireAfterPlay
func TestStartTriggersSnapshotAfterWaitReady(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, n, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	fake := &fakeSnapshotSvc{}
	s.snapshotSvc = fake // 直接注入(WithSnapshotService 走 NewWithScheduler 路径)

	inv.onInvite = func(sess *uac.Session) {
		go func(sid string) {
			time.Sleep(50 * time.Millisecond)
			n.Publish(sid)
		}(sess.StreamID)
	}

	_, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("Start 应成功: %v", err)
	}
	if fake.called.Load() != 1 {
		t.Errorf("FireAfterPlay 应被调 1 次,实际 %d", fake.called.Load())
	}
	got, _ := fake.lastArgs.Load().(snapshotArgs)
	if got.deviceID != "34020000001320000002" || got.channelID != "12345678911116666661" {
		t.Errorf("传参不对: %+v", got)
	}
	// 单节点路径下 nodeID 为空字符串
	if got.nodeID != "" {
		t.Errorf("单节点路径 nodeID 应为空,实际 %q", got.nodeID)
	}
}

// TestStartNoSnapshotSvcStillWorks 通道快照 T4:未注入 snapshotSvc 时 Start 主链路不受影响
func TestStartNoSnapshotSvcStillWorks(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, n, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	// 故意不设 s.snapshotSvc

	inv.onInvite = func(sess *uac.Session) {
		go func(sid string) {
			time.Sleep(50 * time.Millisecond)
			n.Publish(sid)
		}(sess.StreamID)
	}

	res, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("nil snapshotSvc 时 Start 也应正常: %v", err)
	}
	if res.StreamID == "" {
		t.Error("Start 结果应正常返回")
	}
}

// TestWithSnapshotServiceOption 通道快照 T4:WithSnapshotService option 装配后可用
func TestWithSnapshotServiceOption(t *testing.T) {
	fake := &fakeSnapshotSvc{}
	// 单独构造一个只走 New() 后手工注入 option 的 service —— 覆盖 Option 函数
	s := &Service{}
	WithSnapshotService(fake)(s)
	if s.snapshotSvc != fake {
		t.Error("WithSnapshotService 应把 fake 注入到 snapshotSvc")
	}
}

// TestStartReuseExistingStream 流复用 T1:通道已在播放且流在线 → 直接返回现有地址,不发 INVITE
func TestStartReuseExistingStream(t *testing.T) {
	z := &mockZLM{port: 40000}
	z.online.Store(true) // 流在线
	inv := &mockInviter{}
	ch := aChannel()
	ch.StreamID = "existing-stream-id" // 通道已有 streamID
	s, _, _ := newSvc(t, z, inv, onlineDevice(), ch)

	// 第一次调用应复用,不应发 INVITE
	res, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("复用流应成功: %v", err)
	}
	if res.StreamID != "existing-stream-id" {
		t.Errorf("应返回现有 streamID,got %q", res.StreamID)
	}
	if inv.inviteCalls.Load() != 0 {
		t.Errorf("复用流不应发 INVITE,实际调用 %d 次", inv.inviteCalls.Load())
	}
	if z.openCalls.Load() != 0 {
		t.Errorf("复用流不应 openRtpServer,实际调用 %d 次", z.openCalls.Load())
	}
	if z.onlineCalls.Load() != 1 {
		t.Errorf("应检查流是否在线,实际调用 %d 次", z.onlineCalls.Load())
	}
	// UpdateStream 不应被调用(因为没发新 INVITE)
	if ch.StreamID != "existing-stream-id" {
		t.Errorf("复用流不应修改通道 streamID,got %q", ch.StreamID)
	}
}

// TestStartReuseStreamOffline 流复用 T2:通道有 streamID 但流已下线 → 清理残留后重新 INVITE
func TestStartReuseStreamOffline(t *testing.T) {
	z := &mockZLM{port: 40000}
	z.online.Store(false) // 流已下线
	inv := &mockInviter{}
	ch := aChannel()
	ch.StreamID = "stale-stream-id" // 通道有残留 streamID
	s, n, channels := newSvc(t, z, inv, onlineDevice(), ch)

	inv.onInvite = func(sess *uac.Session) {
		go func(streamID string) {
			time.Sleep(50 * time.Millisecond)
			n.Publish(streamID)
		}(sess.StreamID)
	}

	res, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("清理残留后应成功: %v", err)
	}
	// 应生成新 streamID
	if res.StreamID == "stale-stream-id" {
		t.Errorf("应生成新 streamID,不应复用残留 %q", res.StreamID)
	}
	// 应发新 INVITE
	if inv.inviteCalls.Load() != 1 {
		t.Errorf("应发 INVITE,实际调用 %d 次", inv.inviteCalls.Load())
	}
	// 应调用 Stop 清理残留(包含 Bye + CloseRtpServer)
	if inv.byeCalls.Load() != 1 {
		t.Errorf("应发 BYE 清理残留,实际调用 %d 次", inv.byeCalls.Load())
	}
	if channels.clearedStreamID != "stale-stream-id" {
		t.Errorf("应清理残留 streamID,实际清理 %q", channels.clearedStreamID)
	}
	// 最终通道应记录新 streamID
	if ch.StreamID != res.StreamID {
		t.Errorf("通道应记录新 streamID,got %q want %q", ch.StreamID, res.StreamID)
	}
}

// TestStartReuseMultipleClients 流复用 T3:多个客户端同时点播同一通道 → 第二个请求复用第一个的流
func TestStartReuseMultipleClients(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	ch := aChannel()
	s, n, _ := newSvc(t, z, inv, onlineDevice(), ch)

	// 第一个客户端点播
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true) // INVITE 后流变在线
		go func(streamID string) {
			time.Sleep(50 * time.Millisecond)
			n.Publish(streamID)
		}(sess.StreamID)
	}

	res1, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("第一次点播应成功: %v", err)
	}

	// 第二个客户端点播同一通道
	res2, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("第二次点播应成功(复用): %v", err)
	}

	// 应返回相同 streamID
	if res1.StreamID != res2.StreamID {
		t.Errorf("第二次点播应复用第一次的流,got %q want %q", res2.StreamID, res1.StreamID)
	}
	// 只应发一次 INVITE
	if inv.inviteCalls.Load() != 1 {
		t.Errorf("应只发一次 INVITE,实际调用 %d 次", inv.inviteCalls.Load())
	}
	// 只应开一次 RTP 端口
	if z.openCalls.Load() != 1 {
		t.Errorf("应只开一次 RTP 端口,实际调用 %d 次", z.openCalls.Load())
	}
}

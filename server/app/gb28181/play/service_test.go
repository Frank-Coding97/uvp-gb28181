package play

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

// ===== mocks =====

type mockZLM struct {
	openCalls   atomic.Int32
	closeCalls  atomic.Int32
	openErr     error
	closeErr    error
	port        int
	online      atomic.Bool
	onlineCalls atomic.Int32
	onlineErr   error
}

func (m *mockZLM) OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int) (*zlm.OpenRtpServerResult, error) {
	m.openCalls.Add(1)
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
}

func (m *mockInviter) Invite(ctx context.Context, sm *uac.SessionManager, s *uac.Session, body string) error {
	m.inviteCalls.Add(1)
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

type fakeChannels struct{ c *gbmodels.GbChannel }

func (f fakeChannels) FindChannel(ctx context.Context, deviceID, channelID string) (*gbmodels.GbChannel, error) {
	return f.c, nil
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
		SIP: gbconfig.SIPConfig{ServerID: "34020000002000000001", Domain: "3402000000"},
		ZLM: gbconfig.ZLMConfig{Host: "192.168.10.222", HTTPPort: 80, RTPPort: 40000},
		Media: gbconfig.MediaConfig{StreamNoneReaderTimeout: 20},
	}
}

func newSvc(t *testing.T, z ZLM, inv Inviter, dev *gbmodels.GbDevice, ch *gbmodels.GbChannel) (*Service, *stream.Notifier) {
	t.Helper()
	n := stream.NewNotifier()
	sm := uac.NewSessionManager()
	s := New(testCfg(), z, inv, sm, n, fakeDevices{dev}, fakeChannels{ch})
	s.SetReadyTimings(800*time.Millisecond, 50*time.Millisecond)
	return s, n
}

// ===== tests =====

// TestStartHappyPath T6.3-测1: 在线设备 + 通道 + hook 100ms 内到 → 返回播放地址
func TestStartHappyPath(t *testing.T) {
	z := &mockZLM{port: 40000} // online 始终 false,只能靠 hook
	inv := &mockInviter{}
	s, n := newSvc(t, z, inv, onlineDevice(), aChannel())

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
}

// TestStartDeviceOffline T6.3-测2: 设备离线 → 直接拒绝,不开 RTP
func TestStartDeviceOffline(t *testing.T) {
	z := &mockZLM{}
	inv := &mockInviter{}
	dev := onlineDevice()
	dev.Status = gbmodels.DeviceStatusOffline
	s, _ := newSvc(t, z, inv, dev, aChannel())

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
	s, _ := newSvc(t, z, inv, onlineDevice(), nil) // 没通道

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
	s, _ := newSvc(t, z, inv, onlineDevice(), aChannel())

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
	s, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
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
	s, _ := newSvc(t, z, inv, onlineDevice(), aChannel())

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
	s, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	if err := s.Stop(context.Background(), "fake-stream"); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if inv.byeCalls.Load() != 1 || z.closeCalls.Load() != 1 {
		t.Errorf("应各调一次,bye=%d close=%d", inv.byeCalls.Load(), z.closeCalls.Load())
	}
}

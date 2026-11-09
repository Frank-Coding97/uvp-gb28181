package play

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// cross-review round-3 修复回归:
// #1 TCP-Active 不得静默降级 / #2 stale RTP 关闭失败保 SSRC / #3 总超时覆盖前置阶段

// ⚠️ 口径变更（2026-11-09）：本用例原先断言「TCP-Active 一律显式拒绝」。
// 客户验收要求补齐主动收流后，拒绝不再是合同 —— 但**原意保留**：
// TCP-Active 绝不能按 UDP/被动静默处理。现在断言改为「必须真的按主动模式走」：
// offer 写 a=setup:active、ZLM 用 tcp_mode=2、并按对端应答回连一次。
func TestStartTcpActiveNeverFallsBackToPassive(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &mockZLM{port: 40000}
	z.online.Store(true)
	inviter := &mockInviter{outcome: uac.InviteOutcome{
		RequestID: "req-tcp-active", RequestSent: true, FinalStatus: 200,
		AckSucceeded: true, AnswerSDP: answerSDPWithMedia("192.168.10.203", 30000, "passive"),
	}}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	service, _, _ := newFixedSvc(t, z, inviter, onlineDevice(), ch)

	_, err := service.Start(context.Background(), onlineDevice().DeviceID, ch.ChannelID)
	if err != nil {
		t.Fatalf("TCP-Active 应走主动收流;err=%v", err)
	}
	if got := z.lastTCPMode.Load(); got != 2 {
		t.Fatalf("TCP-Active 必须以 tcp_mode=2 开收流端, got=%d", got)
	}
	if !strings.Contains(inviter.lastBody, "a=setup:active") {
		t.Fatalf("offer 必须声明 a=setup:active:\n%s", inviter.lastBody)
	}
	if strings.Contains(inviter.lastBody, "a=setup:passive") {
		t.Fatalf("TCP-Active 不能同时声明 passive:\n%s", inviter.lastBody)
	}
	if z.connectCalls.Load() != 1 {
		t.Fatalf("必须回连一次设备媒体端口, connect=%d", z.connectCalls.Load())
	}
}

func TestStaleRTPCloseFailureKeepsSSRCLeased(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	z := &mockZLM{port: 40000, closeErr: errors.New("zlm close failed")}
	z.online.Store(true)
	inviter := &mockInviter{inviteErr: fmt.Errorf("晚到: %w", uac.ErrStaleInviteGeneration)}
	loc := &fakeLocationStore{nodeID: 2} // 新代次在 node 2,旧代次在 node 1 → 异节点关旧 RTP
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithScheduler(testCfg(), fixedTestPicker{mediaNode: mediaNode}, fixedTestRegistry{mediaNode}, loc,
		inviter, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, &fakeChannels{c: aChannel()},
		WithPlayTokenIssuer(testPlayAuthorization(signer)),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80}})),
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)
	allocator, err := newRealtimeSSRCAllocator("3402000000", 100)
	if err != nil {
		t.Fatal(err)
	}
	service.ssrcAllocator = allocator

	_, err = service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: mediaNode.ID,
	})
	if !errors.Is(err, uac.ErrStaleInviteGeneration) {
		t.Fatalf("error=%v, want ErrStaleInviteGeneration", err)
	}
	if z.closeCalls.Load() != 1 {
		t.Fatalf("stale rollback must close old RTP, close=%d", z.closeCalls.Load())
	}
	allocator.mu.Lock()
	leased := len(allocator.leased)
	allocator.mu.Unlock()
	if leased != 1 {
		t.Fatalf("RTP close failed: SSRC must stay leased, leased=%d", leased)
	}
}

// blockingDevices 挂起 FindByDeviceID 直到 ctx 结束,模拟前置查询阻塞
type blockingDevices struct{}

func (blockingDevices) FindByDeviceID(ctx context.Context, _ string) (*gbmodels.GbDevice, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestStartPreflightHonorsPlayTimeout(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	previous := app.ConfigYml
	source := playbackSource(50) // 50ms 总超时
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = source

	z := &mockZLM{port: 40000}
	z.online.Store(true)
	service := NewWithScheduler(testCfg(), fixedTestPicker{}, fixedTestRegistry{},
		stream.NewLocationMap(), &mockInviter{}, uac.NewSessionManager(), stream.NewNotifier(),
		blockingDevices{}, &fakeChannels{c: aChannel()},
		WithNodeClientFactory(func(*node.Node) ZLM { return z }))

	_, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline from preflight budget", err)
	}
	if z.openCalls.Load() != 0 {
		t.Fatalf("preflight timeout must abort before RTP open, open=%d", z.openCalls.Load())
	}
}

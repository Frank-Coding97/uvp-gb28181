package play

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// cross-review round-3 修复回归:
// #1 TCP-Active 显式拒绝 / #2 stale RTP 关闭失败保 SSRC / #3 总超时覆盖前置阶段

func TestStartRejectsTCPActiveTransport(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	z.online.Store(true)
	inviter := &mockInviter{}
	ch := aChannel()
	ch.StreamTransport = "TCP-Active"
	service := NewWithScheduler(testCfg(), fixedTestPicker{mediaNode: nil}, fixedTestRegistry{},
		stream.NewLocationMap(), inviter, uac.NewSessionManager(), stream.NewNotifier(),
		fakeDevices{onlineDevice()}, &fakeChannels{c: ch},
		WithNodeClientFactory(func(*node.Node) ZLM { return z }))

	_, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: ch.ChannelID,
		Trigger: "on_stream_not_found",
	})
	if err == nil || !strings.Contains(err.Error(), "TCP-Active") {
		t.Fatalf("error=%v, want explicit TCP-Active rejection", err)
	}
	if z.openCalls.Load() != 0 {
		t.Fatalf("TCP-Active must be rejected before RTP open, open=%d", z.openCalls.Load())
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
		WithPlayTokenIssuer(signer),
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

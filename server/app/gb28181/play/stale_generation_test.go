package play

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// Round1 cross-review verify 补修:
// stale INVITE 错误不得进入按 streamID 的通用回滚 —— Bye/CloseRtpServer
// 会选中新代次会话与流,误杀已建立的播放。
func TestStartStaleInviteGenerationDoesNotKillCurrentSession(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	z := &mockZLM{port: 40000}
	z.online.Store(true)
	inviter := &mockInviter{inviteErr: fmt.Errorf("晚到: %w", uac.ErrStaleInviteGeneration)}
	picker := fixedTestPicker{mediaNode: mediaNode}
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithScheduler(testCfg(), picker, fixedTestRegistry{mediaNode}, stream.NewLocationMap(),
		inviter, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, &fakeChannels{c: aChannel()},
		WithPlayTokenIssuer(signer),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80}})),
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)

	_, err = service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: mediaNode.ID,
	})
	if !errors.Is(err, uac.ErrStaleInviteGeneration) {
		t.Fatalf("error=%v, want ErrStaleInviteGeneration", err)
	}
	if inviter.byeCalls.Load() != 0 {
		t.Fatalf("stale rollback BYE calls=%d, want 0(会误杀新代次会话)", inviter.byeCalls.Load())
	}
	if z.closeCalls.Load() != 0 {
		t.Fatalf("stale rollback RTP close calls=%d, want 0(会误杀新代次流)", z.closeCalls.Load())
	}
}

// fakeLocationStore 固定返回指定节点的 LocationStore(模拟新代次已绑定到该节点)
type fakeLocationStore struct{ nodeID int64 }

func (f *fakeLocationStore) Bind(string, int64)                {}
func (f *fakeLocationStore) Lookup(string) (int64, bool)       { return f.nodeID, f.nodeID != 0 }
func (f *fakeLocationStore) Unbind(string)                     {}

func staleService(t *testing.T, inviter *mockInviter, loc LocationStore) (*Service, *mockZLM, *node.Node) {
	t.Helper()
	withFixedAddressPlaybackSettings(t, true, false)
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	z := &mockZLM{port: 40000}
	z.online.Store(true)
	picker := fixedTestPicker{mediaNode: mediaNode}
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithScheduler(testCfg(), picker, fixedTestRegistry{mediaNode}, loc,
		inviter, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, &fakeChannels{c: aChannel()},
		WithPlayTokenIssuer(signer),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80}})),
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)
	return service, z, mediaNode
}

// 新代次已迁到另一节点:旧节点的 RTP listener 必须按旧 client 关闭,否则永久泄漏.
func TestStartStaleInviteClosesRTPOnDifferentNode(t *testing.T) {
	inviter := &mockInviter{inviteErr: fmt.Errorf("晚到: %w", uac.ErrStaleInviteGeneration)}
	loc := &fakeLocationStore{nodeID: 2} // 新代次在 node 2
	service, z, mediaNode := staleService(t, inviter, loc)

	_, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: mediaNode.ID,
	})
	if !errors.Is(err, uac.ErrStaleInviteGeneration) {
		t.Fatalf("error=%v, want ErrStaleInviteGeneration", err)
	}
	if inviter.byeCalls.Load() != 0 {
		t.Fatalf("stale rollback BYE calls=%d, want 0", inviter.byeCalls.Load())
	}
	if z.closeCalls.Load() != 1 {
		t.Fatalf("stale rollback on different node must close old RTP once, got %d", z.closeCalls.Load())
	}
}

// 新代次仍在同一节点:关 RTP 会误杀新代次的流,不得调用.
func TestStartStaleInviteSkipsRTPCloseOnSameNode(t *testing.T) {
	inviter := &mockInviter{inviteErr: fmt.Errorf("晚到: %w", uac.ErrStaleInviteGeneration)}
	loc := &fakeLocationStore{nodeID: 1} // 新代次仍在 node 1(与本代次同节点)
	service, z, mediaNode := staleService(t, inviter, loc)

	_, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: mediaNode.ID,
	})
	if !errors.Is(err, uac.ErrStaleInviteGeneration) {
		t.Fatalf("error=%v, want ErrStaleInviteGeneration", err)
	}
	if z.closeCalls.Load() != 0 {
		t.Fatalf("same-node stale rollback must not close RTP, got %d", z.closeCalls.Load())
	}
}

// BYE/Close 未确认成功时 SSRC 必须保持租约,防止回池后被新代次复用造成双流冲突.
func TestStartStaleInviteCleanupFailureKeepsSSRCLeased(t *testing.T) {
	inviter := &mockInviter{inviteErr: fmt.Errorf("晚到: %w", errors.Join(uac.ErrStaleInviteGeneration, uac.ErrStaleInviteCleanupFailed))}
	loc := &fakeLocationStore{nodeID: 1}
	service, _, mediaNode := staleService(t, inviter, loc)
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
	allocator.mu.Lock()
	leased := len(allocator.leased)
	allocator.mu.Unlock()
	if leased != 1 {
		t.Fatalf("cleanup-failed SSRC must stay leased, leased=%d", leased)
	}
}

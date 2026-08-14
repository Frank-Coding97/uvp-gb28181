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

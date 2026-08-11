package play

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type authorizationReadyRegistry struct {
	fixedTestRegistry
	ready bool
}

func (r authorizationReadyRegistry) IsAutoOnDemandReady(id int64) bool {
	return r.ready && r.mediaNode != nil && r.mediaNode.ID == id
}

func newFixedAuthorizationService(t *testing.T, ready bool, z *mockZLM, inviter *mockInviter) (*Service, *playauth.AuthorizationService, *playauth.AuthorizationRegistry, *countingFixedPicker, *stream.Notifier) {
	t.Helper()
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	authorizationRegistry := playauth.NewAuthorizationRegistry()
	authorization := playauth.NewAuthorizationService(signer, authorizationRegistry)
	picker := &countingFixedPicker{mediaNode: mediaNode}
	notifier := stream.NewNotifier()
	service := NewWithScheduler(
		testCfg(), picker, authorizationReadyRegistry{fixedTestRegistry: fixedTestRegistry{mediaNode}, ready: ready}, stream.NewLocationMap(),
		inviter, uac.NewSessionManager(), notifier, fakeDevices{onlineDevice()}, &fakeChannels{c: aChannel()},
		WithPlayTokenIssuer(authorization),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80, HLSEnabled: true, FMP4Enabled: true}})),
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)
	service.SetReadyTimings(800*time.Millisecond, 20*time.Millisecond)
	return service, authorization, authorizationRegistry, picker, notifier
}

func tokenFromFixedAuthorization(t *testing.T, result *Result) string {
	t.Helper()
	parsed, err := url.Parse(valueOrEmpty(result.URLs.HTTPFMP4))
	if err != nil {
		t.Fatal(err)
	}
	token := parsed.Query().Get(playauth.QueryParameter)
	if token == "" {
		t.Fatal("fixed authorization URL has no play token")
	}
	return token
}

func TestAuthorizeFixedPlaybackHasNoMediaSideEffects(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, authorization, registry, picker, _ := newFixedAuthorizationService(t, true, z, inviter)

	result, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "203.0.113.9")
	if err != nil {
		t.Fatalf("authorize fixed playback: %v", err)
	}
	wantStream, _ := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if result.StreamID != wantStream || result.SSRC != "" || result.Generation != 0 || result.Node == nil || result.Node.ID != 1 {
		t.Fatalf("unexpected authorization result: %+v", result)
	}
	if result.AuthorizationExpiresAt == 0 || registry.Size() != 1 || picker.calls.Load() != 1 {
		t.Fatalf("authorization was not registered: result=%+v registry=%d picker=%d", result, registry.Size(), picker.calls.Load())
	}
	token := tokenFromFixedAuthorization(t, result)
	if _, err := authorization.VerifyForAutoStart(token, playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: wantStream, MediaServerID: "node-a",
	}); err != nil {
		t.Fatalf("issued preauthorization did not verify: %v", err)
	}
	if z.openCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 {
		t.Fatalf("preauthorization opened media: open=%d invite=%d", z.openCalls.Load(), inviter.inviteCalls.Load())
	}
}

func TestAuthorizeFixedPlaybackRequiresAllRuntimeGates(t *testing.T) {
	tests := []struct {
		name      string
		fixed     bool
		auto      bool
		auth      bool
		nodeReady bool
	}{
		{name: "fixed disabled", auto: true, auth: true, nodeReady: true},
		{name: "auto disabled", fixed: true, auth: true, nodeReady: true},
		{name: "auth disabled", fixed: true, auto: true, nodeReady: true},
		{name: "node not auto-ready", fixed: true, auto: true, auth: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFixedAddressPlaybackSettings(t, tt.fixed, tt.auto)
			withPlayAuthorization(t, tt.auth, false)
			z := &mockZLM{}
			inviter := &mockInviter{}
			service, _, registry, picker, _ := newFixedAuthorizationService(t, tt.nodeReady, z, inviter)

			_, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "")
			if !errors.Is(err, ErrPlayAuthorizationUnavailable) {
				t.Fatalf("error=%v, want ErrPlayAuthorizationUnavailable", err)
			}
			if registry.Size() != 0 || z.openCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 {
				t.Fatalf("rejected authorization had side effects: registry=%d open=%d invite=%d", registry.Size(), z.openCalls.Load(), inviter.inviteCalls.Load())
			}
			if (!tt.fixed || !tt.auto || !tt.auth) && picker.calls.Load() != 0 {
				t.Fatalf("configuration rejection selected a node %d times", picker.calls.Load())
			}
		})
	}
}

func TestAutoStartAuthorizationBindsBeforeMediaAndTerminatesOnFailure(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{openErr: errors.New("open failed")}
	inviter := &mockInviter{}
	service, authorization, _, _, _ := newFixedAuthorizationService(t, true, z, inviter)
	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFromFixedAuthorization(t, preauthorized)
	binding := playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
	}
	claims, err := authorization.VerifyForAutoStart(token, binding)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: 1,
		AuthorizationID: claims.AuthorizationGeneration,
	})
	if err == nil {
		t.Fatal("auto start unexpectedly succeeded")
	}
	if z.openCalls.Load() != 1 || inviter.inviteCalls.Load() != 0 {
		t.Fatalf("unexpected media effects: open=%d invite=%d", z.openCalls.Load(), inviter.inviteCalls.Load())
	}
	if _, err := authorization.VerifyForAutoStart(token, binding); !errors.Is(err, playauth.ErrAuthorizationTerminal) {
		t.Fatalf("failed generation authorization state=%v, want terminal", err)
	}
}

func TestAutoStartAuthorizationTerminatesWhenGenerationStops(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, authorization, _, _, notifier := newFixedAuthorizationService(t, true, z, inviter)
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFromFixedAuthorization(t, preauthorized)
	binding := playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
	}
	claims, err := authorization.VerifyForAutoStart(token, binding)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: 1,
		AuthorizationID: claims.AuthorizationGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound := binding
	bound.MediaGeneration = result.Generation
	if _, err := authorization.Verify(token, bound); err != nil {
		t.Fatalf("bound token did not verify: %v", err)
	}
	if err := service.Stop(context.Background(), result.StreamID); err != nil {
		t.Fatal(err)
	}
	if _, err := authorization.Verify(token, bound); !errors.Is(err, playauth.ErrAuthorizationTerminal) {
		t.Fatalf("stopped generation authorization state=%v, want terminal", err)
	}
}

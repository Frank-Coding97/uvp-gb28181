package play

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type fixedTestPicker struct{ mediaNode *node.Node }

func (p fixedTestPicker) Pick(context.Context, PickContext) (*node.Node, error) {
	return p.mediaNode, nil
}

type countingFixedPicker struct {
	mediaNode *node.Node
	calls     atomic.Int32
}

func (p *countingFixedPicker) Pick(context.Context, PickContext) (*node.Node, error) {
	p.calls.Add(1)
	return p.mediaNode, nil
}

type fixedTestRegistry struct{ mediaNode *node.Node }

func (r fixedTestRegistry) Get(id int64) (*node.Node, bool) {
	return r.mediaNode, r.mediaNode != nil && r.mediaNode.ID == id
}

func (r fixedTestRegistry) List() []*node.Node {
	if r.mediaNode == nil {
		return nil
	}
	return []*node.Node{r.mediaNode}
}

func (r fixedTestRegistry) ListActive() []*node.Node {
	if r.mediaNode == nil || !r.mediaNode.IsActive() {
		return nil
	}
	return []*node.Node{r.mediaNode}
}

func newFixedSvc(t *testing.T, z ZLM, inv Inviter, dev *gbmodels.GbDevice, ch *gbmodels.GbChannel) (*Service, *stream.Notifier, *fakeChannels) {
	t.Helper()
	notifier := stream.NewNotifier()
	channels := &fakeChannels{c: ch}
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithScheduler(testCfg(), fixedTestPicker{mediaNode}, fixedTestRegistry{mediaNode}, stream.NewLocationMap(),
		inv, uac.NewSessionManager(), notifier, fakeDevices{dev}, channels,
		WithPlayTokenIssuer(signer),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80, HLSEnabled: true, FMP4Enabled: true}})),
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)
	service.SetReadyTimings(800*time.Millisecond, 50*time.Millisecond)
	return service, notifier, channels
}

func withFixedAddressPlaybackSettings(t *testing.T, fixed, auto bool) *playbackSettingsSource {
	t.Helper()
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	source := playbackSource(10000)
	source.values[gbconfig.FixedAddressEnabledConfigKey] = fixed
	source.values[gbconfig.AutoOnDemandEnabledConfigKey] = auto
	app.ConfigYml = source
	return source
}

func TestStartFixedAddressUsesStableStreamIDAndSeparateSSRC(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(sess.StreamID)
	}

	result, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatalf("fixed start: %v", err)
	}
	want, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if result.StreamID != want {
		t.Fatalf("streamID=%q, want fixed %q", result.StreamID, want)
	}
	if result.SSRC == result.StreamID || len(result.SSRC) != 10 {
		t.Fatalf("fixed streamID and session SSRC must be separate: %+v", result)
	}
	if got, _ := z.lastStreamID.Load().(string); got != want {
		t.Fatalf("openRtpServer streamID=%q, want %q", got, want)
	}
	if got, _ := z.lastSSRC.Load().(string); got != result.SSRC {
		t.Fatalf("openRtpServer ssrc=%q, want current session %q", got, result.SSRC)
	}
	if inv.lastSession == nil || inv.lastSession.SSRC != result.SSRC || inv.lastSession.SSRC == result.StreamID {
		t.Fatalf("SIP session must use current SSRC, session=%+v result=%+v", inv.lastSession, result)
	}
	if !strings.Contains(inv.lastBody, "y="+result.SSRC) {
		t.Fatalf("SDP must use session SSRC %q:\n%s", result.SSRC, inv.lastBody)
	}
}

func TestStartFixedAddressHookOnlyWakesUntilMediaIsOnline(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	done := make(chan error, 1)

	go func() {
		_, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
		done <- err
	}()
	deadline := time.After(time.Second)
	for z.onlineCalls.Load() == 0 {
		select {
		case err := <-done:
			t.Fatalf("start returned before readiness check: %v", err)
		case <-deadline:
			t.Fatal("initial media readiness check was not reached")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	notifier.Publish(streamID)
	select {
	case err := <-done:
		t.Fatalf("offline media became ready from hook alone: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	z.online.Store(true)
	notifier.Publish(streamID)
	if err := <-done; err != nil {
		t.Fatalf("start after verified media online: %v", err)
	}
}

func TestStartModeSwitchKeepsCurrentGenerationAndAppliesAfterStop(t *testing.T) {
	tests := []struct {
		name        string
		initial     bool
		next        bool
		initialMode LiveMode
		nextMode    LiveMode
	}{
		{name: "dynamic to fixed", initial: false, next: true, initialMode: LiveModeDynamic, nextMode: LiveModeFixed},
		{name: "fixed to dynamic", initial: true, next: false, initialMode: LiveModeFixed, nextMode: LiveModeDynamic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := withFixedAddressPlaybackSettings(t, tt.initial, false)
			z := &mockZLM{port: 40000}
			inv := &mockInviter{}
			s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
			inv.onInvite = func(sess *uac.Session) {
				z.online.Store(true)
				go notifier.Publish(sess.StreamID)
			}

			first, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
			if err != nil {
				t.Fatalf("initial start: %v", err)
			}
			if first.ModeAtStart != tt.initialMode {
				t.Fatalf("initial mode=%q, want %q", first.ModeAtStart, tt.initialMode)
			}
			source.Set(gbconfig.FixedAddressEnabledConfigKey, tt.next)
			source.Set(gbconfig.AutoOnDemandEnabledConfigKey, false)

			reused, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
			if err != nil {
				t.Fatalf("reuse after mode switch: %v", err)
			}
			if reused.StreamID != first.StreamID || reused.SSRC != first.SSRC ||
				reused.Generation != first.Generation || reused.ModeAtStart != first.ModeAtStart ||
				inv.inviteCalls.Load() != 1 {
				t.Fatalf("mode switch must reuse current generation: first=%+v reused=%+v invites=%d", first, reused, inv.inviteCalls.Load())
			}

			z.online.Store(false)
			if err := s.Stop(context.Background(), first.StreamID); err != nil {
				t.Fatalf("stop current generation: %v", err)
			}
			next, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
			if err != nil {
				t.Fatalf("next generation: %v", err)
			}
			if next.ModeAtStart != tt.nextMode || next.Generation == first.Generation {
				t.Fatalf("next generation did not apply switched mode: first=%+v next=%+v", first, next)
			}
		})
	}
}

func TestStartFixedAddressRejectsInvalidGBIDs(t *testing.T) {
	tests := []struct {
		name      string
		deviceID  string
		channelID string
	}{
		{name: "invalid device", deviceID: "device", channelID: aChannel().ChannelID},
		{name: "invalid channel", deviceID: onlineDevice().DeviceID, channelID: "channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFixedAddressPlaybackSettings(t, true, false)
			device := onlineDevice()
			device.DeviceID = tt.deviceID
			channel := aChannel()
			channel.DeviceID = tt.deviceID
			channel.ChannelID = tt.channelID
			z := &mockZLM{}
			s, _, _ := newFixedSvc(t, z, &mockInviter{}, device, channel)

			_, err := s.Start(context.Background(), tt.deviceID, tt.channelID)
			if !errors.Is(err, ErrInvalidFixedStreamID) {
				t.Fatalf("error=%v, want ErrInvalidFixedStreamID", err)
			}
			if z.openCalls.Load() != 0 {
				t.Fatalf("invalid fixed ID opened RTP %d times", z.openCalls.Load())
			}
		})
	}
}

func TestEnsureLiveRequiredNodeRejectsInactiveNodeBeforeMediaSideEffects(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateOffline, RTPPortStart: 40000,
	}
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	picker := &countingFixedPicker{mediaNode: &node.Node{
		ID: 2, Name: "node-b", Host: "192.168.10.223", PlaybackHost: "192.168.10.223",
		MediaServerUUID: "node-b", State: node.StateActive, RTPPortStart: 41000,
	}}
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
	if !errors.Is(err, ErrRequiredNodeUnavailable) {
		t.Fatalf("error=%v, want ErrRequiredNodeUnavailable", err)
	}
	if z.openCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 {
		t.Fatalf("inactive required node caused side effects: open=%d invite=%d", z.openCalls.Load(), inviter.inviteCalls.Load())
	}
	if picker.calls.Load() != 0 {
		t.Fatalf("required node path called scheduler %d times", picker.calls.Load())
	}
}

func TestStartFixedAddressReuseReturnsCurrentSSRC(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{}
	z.online.Store(true)
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000001"
	s, _, _ := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), channel)

	result, err := s.Start(context.Background(), channel.DeviceID, channel.ChannelID)
	if err != nil {
		t.Fatalf("fixed reuse: %v", err)
	}
	if !result.Reused || result.StreamID != streamID || result.SSRC != channel.CurrentSSRC {
		t.Fatalf("reuse result=%+v, want stream=%q ssrc=%q", result, streamID, channel.CurrentSSRC)
	}
}

func TestStartFixedAddressNewGenerationKeepsPathAndChangesSSRC(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true)
		go func(streamID string) {
			time.Sleep(time.Millisecond)
			notifier.Publish(streamID)
		}(sess.StreamID)
	}

	first, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	z.online.Store(false)
	if err := s.Stop(context.Background(), first.StreamID); err != nil {
		t.Fatalf("stop first generation: %v", err)
	}
	second, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if first.StreamID != second.StreamID {
		t.Fatalf("fixed path changed: first=%q second=%q", first.StreamID, second.StreamID)
	}
	if first.SSRC == second.SSRC || first.Generation == second.Generation {
		t.Fatalf("two fixed generations must change SSRC and generation: first=%+v second=%+v", first, second)
	}
}

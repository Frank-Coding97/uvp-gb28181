package play

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

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
	s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
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
	s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
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
			s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
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
			if reused != first || inv.inviteCalls.Load() != 1 {
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
			s, _, _ := newSvc(t, z, &mockInviter{}, device, channel)

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
	s, _, _ := newSvc(t, z, &mockInviter{}, onlineDevice(), channel)

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
	s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
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

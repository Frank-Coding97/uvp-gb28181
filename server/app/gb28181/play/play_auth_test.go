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

type failingPlayTokenIssuer struct{}

func (failingPlayTokenIssuer) IssueDirect(playauth.Binding) (playauth.Grant, error) {
	return playauth.Grant{}, errors.New("signing unavailable")
}

func TestFixedPlaybackResultUsesShortLivedAuthorizationOnEveryURL(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"), playauth.WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	mediaNode := &node.Node{ID: 22, Name: "edge-22", Host: "10.0.0.22", MediaServerUUID: "node-a"}
	service := &Service{
		cfg: testCfg(), sessions: uac.NewSessionManager(), tokenIssuer: signer,
		urlResolver: NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{
			HTTPPort: 28080, HTTPSPort: 28443, RTSPPort: 10554, RTMPPort: 11935,
			RTSPEnabled: true, RTMPEnabled: true, HLSEnabled: true, TSEnabled: true, FMP4Enabled: true,
		}}),
	}
	deviceID := "37010301021320000014"
	channelID := "37010301021320000001"
	streamID, err := FixedStreamID(deviceID, channelID)
	if err != nil {
		t.Fatal(err)
	}
	result := service.buildNodeResult(context.Background(), streamID, "0200000001", mediaNode, false)
	result.ModeAtStart = LiveModeFixed
	if err := service.authorizeFixedResult(result, deviceID, channelID, mediaNode); err != nil {
		t.Fatalf("authorize fixed result: %v", err)
	}

	for protocol, raw := range result.URLs.AsMap() {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("%s URL parse: %v", protocol, err)
		}
		token := parsed.Query().Get(playauth.QueryParameter)
		if token == "" {
			t.Fatalf("%s URL missing %s: %s", protocol, playauth.QueryParameter, raw)
		}
		if _, err := signer.Verify(token, playauth.Binding{
			DeviceID: deviceID, ChannelID: channelID, App: "rtp", Stream: streamID, MediaServerID: mediaNode.MediaServerUUID,
		}); err != nil {
			t.Fatalf("%s token verify: %v", protocol, err)
		}
	}
	if result.HTTPFlvURL != *result.URLs.HTTPFLV || result.WSFlvURL != *result.URLs.WSFLV || result.HLSURL != *result.URLs.HLS {
		t.Fatalf("legacy URL fields are not synchronized: %+v", result)
	}
	if result.URL == "" {
		t.Fatal("selected playback URL was not rebuilt after authorization")
	}
}

func TestDynamicPlaybackResultIsNotChangedByFixedAddressAuthorization(t *testing.T) {
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{tokenIssuer: signer}
	raw := "http://node/rtp/0200000001.live.flv"
	result := &Result{StreamID: "0200000001", App: "rtp", ModeAtStart: LiveModeDynamic, URLs: PlaybackURLs{HTTPFLV: &raw}}
	if err := service.authorizeFixedResult(result, "37010301021320000014", "37010301021320000001", &node.Node{MediaServerUUID: "node-a"}); err != nil {
		t.Fatal(err)
	}
	if *result.URLs.HTTPFLV != raw {
		t.Fatalf("dynamic URL changed: %q", *result.URLs.HTTPFLV)
	}
}

func TestFixedPlaybackAuthorizationFailsBeforeMediaSideEffects(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	tests := []struct {
		name            string
		issuer          playauth.DirectIssuer
		resolverFailure bool
		want            error
	}{
		{name: "missing issuer", want: ErrPlayAuthorizationUnavailable},
		{name: "issuer failure", issuer: failingPlayTokenIssuer{}, want: ErrPlayAuthorizationUnavailable},
		{name: "resolver failure", resolverFailure: true, want: playauth.ErrURLInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := &mockZLM{port: 40000}
			inv := &mockInviter{}
			s, _, channels := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
			if !tt.resolverFailure {
				s.tokenIssuer = tt.issuer
			} else {
				s.urlResolver = NewURLResolver(fakeServerConfigProvider{err: errors.New("config unavailable")})
			}

			_, err := s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
			if z.openCalls.Load() != 0 || inv.inviteCalls.Load() != 0 {
				t.Fatalf("authorization failure opened media: open=%d invite=%d", z.openCalls.Load(), inv.inviteCalls.Load())
			}
			if channels.c.StreamID != "" || channels.c.CurrentSSRC != "" {
				t.Fatalf("authorization failure persisted session: %+v", channels.c)
			}
		})
	}
}

func TestFixedPlaybackDeprecatedSingleNodeFailsClosed(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(testCfg(), z, inv, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, &fakeChannels{c: aChannel()}, WithPlayTokenIssuer(signer))

	_, err = s.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if !errors.Is(err, ErrFixedPlaybackRequiresManagedNode) {
		t.Fatalf("error=%v, want ErrFixedPlaybackRequiresManagedNode", err)
	}
	if z.openCalls.Load() != 0 || inv.inviteCalls.Load() != 0 {
		t.Fatalf("deprecated fixed playback opened media: open=%d invite=%d", z.openCalls.Load(), inv.inviteCalls.Load())
	}
}

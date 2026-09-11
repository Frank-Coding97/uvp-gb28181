package play

import (
	"context"
	"errors"
	"testing"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeServerConfigProvider struct {
	cfg node.ServerConfig
	err error
}

func (f fakeServerConfigProvider) Get(context.Context, int64) (node.ServerConfig, error) {
	return f.cfg, f.err
}

func (f fakeServerConfigProvider) Refresh(context.Context, int64) (node.ServerConfig, error) {
	return f.cfg, f.err
}

func TestURLResolverUsesMediaPortsFromTargetNode(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{
		HTTPPort:    18080,
		HTTPSPort:   18443,
		RTSPPort:    10554,
		RTSPSPort:   10555,
		RTMPPort:    11935,
		RTMPSPort:   11936,
		RTSPEnabled: true,
		RTMPEnabled: true,
		HLSEnabled:  true,
		TSEnabled:   true,
		FMP4Enabled: true,
	}})

	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 7, Host: "192.168.10.220"}, "rtp", "stream-1")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	assertURL(t, urls.WSFLV, "ws://192.168.10.220:18080/rtp/stream-1.live.flv")
	assertURL(t, urls.HTTPFLV, "http://192.168.10.220:18080/rtp/stream-1.live.flv")
	assertURL(t, urls.WSFMP4, "ws://192.168.10.220:18080/rtp/stream-1.live.mp4")
	assertURL(t, urls.HTTPFMP4, "http://192.168.10.220:18080/rtp/stream-1.live.mp4")
	assertURL(t, urls.WSSFLV, "wss://192.168.10.220:18443/rtp/stream-1.live.flv")
	assertURL(t, urls.HTTPSFLV, "https://192.168.10.220:18443/rtp/stream-1.live.flv")
	assertURL(t, urls.WSSFMP4, "wss://192.168.10.220:18443/rtp/stream-1.live.mp4")
	assertURL(t, urls.HTTPSFMP4, "https://192.168.10.220:18443/rtp/stream-1.live.mp4")
	assertURL(t, urls.HLS, "http://192.168.10.220:18080/rtp/stream-1/hls.m3u8")
	assertURL(t, urls.HTTPSHLS, "https://192.168.10.220:18443/rtp/stream-1/hls.m3u8")
	assertURL(t, urls.WSTS, "ws://192.168.10.220:18080/rtp/stream-1.live.ts")
	assertURL(t, urls.HTTPSTS, "http://192.168.10.220:18080/rtp/stream-1.live.ts")
	assertURL(t, urls.WSSTS, "wss://192.168.10.220:18443/rtp/stream-1.live.ts")
	assertURL(t, urls.HTTPSSTS, "https://192.168.10.220:18443/rtp/stream-1.live.ts")
	assertURL(t, urls.WebRTC, "http://192.168.10.220:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play")
	assertURL(t, urls.WebRTCS, "https://192.168.10.220:18443/index/api/webrtc?app=rtp&stream=stream-1&type=play")
	assertURL(t, urls.RTMP, "rtmp://192.168.10.220:11935/rtp/stream-1")
	assertURL(t, urls.RTMPS, "rtmps://192.168.10.220:11936/rtp/stream-1")
	assertURL(t, urls.RTSP, "rtsp://192.168.10.220:10554/rtp/stream-1")
	assertURL(t, urls.RTSPS, "rtsps://192.168.10.220:10555/rtp/stream-1")
}

func TestURLResolverUsesPlaybackHostWithoutChangingNodeAPIHost(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 18080}})
	mediaNode := &node.Node{ID: 7, Host: "10.0.0.2", PlaybackHost: "play.example.com"}
	urls, warnings := resolver.Resolve(context.Background(), mediaNode, "rtp", "stream-1")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	assertURL(t, urls.HTTPFLV, "http://play.example.com:18080/rtp/stream-1.live.flv")
	if mediaNode.Host != "10.0.0.2" {
		t.Fatalf("resolver must not mutate API host: %q", mediaNode.Host)
	}
}

func TestURLResolverLeavesUnavailableProtocolsNull(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 18080, RTSPEnabled: true, RTMPEnabled: true, HLSEnabled: true, TSEnabled: true, FMP4Enabled: true}})
	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 8, Host: "zlm.local"}, "rtp", "stream-2")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if urls.RTMPS != nil || urls.RTSPS != nil || urls.RTMP != nil || urls.RTSP != nil {
		t.Fatalf("disabled protocols must be null: %+v", urls)
	}
}

func TestURLResolverFiltersDisabledProtocolFamilies(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{
		HTTPPort: 18080, HTTPSPort: 18443, RTMPPort: 11935, RTMPSPort: 11936, RTSPPort: 10554, RTSPSPort: 10555,
		RTSPEnabled: false, RTMPEnabled: false, HLSEnabled: false, TSEnabled: false, FMP4Enabled: false,
	}})
	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 10, Host: "zlm.local"}, "rtp", "stream")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	for name, value := range map[string]*string{
		"hls": urls.HLS, "httpsHls": urls.HTTPSHLS, "wsTs": urls.WSTS, "httpTs": urls.HTTPSTS, "wssTs": urls.WSSTS, "httpsTs": urls.HTTPSSTS,
		"wsFmp4": urls.WSFMP4, "httpFmp4": urls.HTTPFMP4, "wssFmp4": urls.WSSFMP4, "httpsFmp4": urls.HTTPSFMP4,
		"rtmp": urls.RTMP, "rtmps": urls.RTMPS, "rtsp": urls.RTSP, "rtsps": urls.RTSPS,
	} {
		if value != nil {
			t.Errorf("%s should be unavailable", name)
		}
	}
	if urls.WSFLV == nil || urls.HTTPFLV == nil || urls.WebRTC == nil || urls.WebRTCS == nil {
		t.Fatalf("FLV/WebRTC should remain available: %+v", urls)
	}
}

func TestURLResolverEscapesPathAndQuery(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 18080, RTSPEnabled: true, RTMPEnabled: true, HLSEnabled: true, TSEnabled: true, FMP4Enabled: true}})
	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 11, Host: "zlm.local"}, "app name/片段", "stream/?&")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	assertURL(t, urls.HTTPFLV, "http://zlm.local:18080/app%20name%2F%E7%89%87%E6%AE%B5/stream%2F%3F&.live.flv")
	assertURL(t, urls.WebRTC, "http://zlm.local:18080/index/api/webrtc?app=app+name%2F%E7%89%87%E6%AE%B5&stream=stream%2F%3F%26&type=play")
}

func TestURLResolverReturnsWarningsWithoutInventingURLs(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{err: errors.New("node unavailable")})
	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 9, Host: "zlm.local"}, "rtp", "stream-3")
	if urls.WSFLV != nil || urls.HTTPFLV != nil || urls.HLS != nil || urls.WebRTC != nil || urls.RTMP != nil || urls.RTSP != nil {
		t.Fatalf("config failure must not invent URLs: %+v", urls)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings=%v", warnings)
	}
}

func TestBuildPlaybackURLsAndSelectPlaybackSource(t *testing.T) {
	urls := BuildPlaybackURLs("play.example.com", node.ServerConfig{
		HTTPPort: 8080, HTTPSPort: 8443, HLSEnabled: true,
	}, "app name", "stream/?&")
	assertURL(t, urls.WSFLV, "ws://play.example.com:8080/app%20name/stream%2F%3F&.live.flv")
	assertURL(t, urls.HTTPSHLS, "https://play.example.com:8443/app%20name/stream%2F%3F&/hls.m3u8")

	selected := SelectPlaybackSource(urls, "webrtc", true)
	if selected.Protocol != "webrtc" || selected.URL != "https://play.example.com:8443/index/api/webrtc?app=app+name&stream=stream%2F%3F%26&type=play" || !selected.ZLMWebRTC {
		t.Fatalf("selected=%+v", selected)
	}

	urls.WebRTCS = nil
	selected = SelectPlaybackSource(urls, "webrtc", true)
	if selected.Protocol != "ws-flv" || selected.URL != "wss://play.example.com:8443/app%20name/stream%2F%3F&.live.flv" || selected.ZLMWebRTC {
		t.Fatalf("secure fallback=%+v", selected)
	}
}

func TestSelectPlaybackSourceDoesNotUseInsecureURLOnHTTPS(t *testing.T) {
	ws := "ws://node/live.flv"
	selected := SelectPlaybackSource(PlaybackURLs{WSFLV: &ws}, "ws-flv", true)
	if selected.URL != "" || selected.Protocol != "" || selected.ZLMWebRTC {
		t.Fatalf("selected=%+v", selected)
	}
}

func TestReuseResultKeepsOriginalNodeAndItsPorts(t *testing.T) {
	mediaNode := &node.Node{ID: 22, Name: "edge-22", Host: "10.0.0.22"}
	service := &Service{
		cfg:         testCfg(),
		sessions:    uac.NewSessionManager(),
		urlResolver: NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 28080, RTSPEnabled: true, RTMPEnabled: true, HLSEnabled: true, TSEnabled: true, FMP4Enabled: true}}),
	}

	result := service.buildReuseResult(context.Background(), &gbmodels.GbChannel{
		StreamID:    "existing",
		CurrentSSRC: "0200000001",
	}, mediaNode)
	if !result.Reused || result.Node == nil || result.Node.ID != 22 || result.Node.Name != "edge-22" {
		t.Fatalf("unexpected reused node result: %+v", result)
	}
	if result.HTTPFlvURL != "http://10.0.0.22:28080/rtp/existing.live.flv" {
		t.Fatalf("HTTPFlvURL=%q", result.HTTPFlvURL)
	}
}

func assertURL(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		if got == nil {
			t.Fatalf("url=<nil>, want %q", want)
		}
		t.Fatalf("url=%q, want %q", *got, want)
	}
}

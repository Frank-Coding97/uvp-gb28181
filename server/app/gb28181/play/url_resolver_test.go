package play

import (
	"context"
	"errors"
	"testing"

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

func TestURLResolverUsesMediaPortsFromTargetNode(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{
		HTTPPort: 18080,
		RTSPPort: 10554,
		RTMPPort: 11935,
	}})

	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 7, Host: "192.168.10.220"}, "rtp", "stream-1")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	assertURL(t, urls.WSFLV, "ws://192.168.10.220:18080/rtp/stream-1.live.flv")
	assertURL(t, urls.HTTPFLV, "http://192.168.10.220:18080/rtp/stream-1.live.flv")
	assertURL(t, urls.HLS, "http://192.168.10.220:18080/rtp/stream-1/hls.m3u8")
	assertURL(t, urls.WebRTC, "http://192.168.10.220:18080/index/api/webrtc?app=rtp&stream=stream-1&type=play")
	assertURL(t, urls.RTMP, "rtmp://192.168.10.220:11935/rtp/stream-1")
	assertURL(t, urls.RTSP, "rtsp://192.168.10.220:10554/rtp/stream-1")
}

func TestURLResolverLeavesUnavailableProtocolsNull(t *testing.T) {
	resolver := NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 18080}})
	urls, warnings := resolver.Resolve(context.Background(), &node.Node{ID: 8, Host: "zlm.local"}, "rtp", "stream-2")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if urls.RTMP != nil || urls.RTSP != nil {
		t.Fatalf("disabled protocols must be null: %+v", urls)
	}
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

func TestReuseResultKeepsOriginalNodeAndItsPorts(t *testing.T) {
	mediaNode := &node.Node{ID: 22, Name: "edge-22", Host: "10.0.0.22"}
	service := &Service{
		cfg:         testCfg(),
		sessions:    uac.NewSessionManager(),
		urlResolver: NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 28080}}),
	}

	result := service.buildReuseResult(context.Background(), "existing", mediaNode)
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
		t.Fatalf("url=%v, want %q", got, want)
	}
}

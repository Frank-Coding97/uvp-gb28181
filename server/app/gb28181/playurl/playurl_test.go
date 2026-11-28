package playurl

import (
	"testing"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

func TestBuildBracketsIPv6PlaybackHost(t *testing.T) {
	urls := Build("2001:db8::20", node.ServerConfig{HTTPPort: 18080, RTSPPort: 10554, RTSPEnabled: true}, "rtp", "stream-1")
	if got := *urls.HTTPFLV; got != "http://[2001:db8::20]:18080/rtp/stream-1.live.flv" {
		t.Fatalf("HTTP-FLV URL = %q", got)
	}
	if got := *urls.RTSP; got != "rtsp://[2001:db8::20]:10554/rtp/stream-1" {
		t.Fatalf("RTSP URL = %q", got)
	}
}

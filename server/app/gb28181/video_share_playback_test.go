package gb28181

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type videoShareLifetimeConfig struct {
	app.YmlConfigInterf
	ttl int
}

func (c videoShareLifetimeConfig) GetInt(string) int { return c.ttl }

func TestVideoShareLifetimeDefaultsForExistingInstallations(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = videoShareLifetimeConfig{}
	require.Equal(t, 5*time.Minute, videoShareLifetime(), "old installations have no video_share configuration")
	app.ConfigYml = videoShareLifetimeConfig{ttl: 420}
	require.Equal(t, 7*time.Minute, videoShareLifetime())
}

func TestVideoShareInternalURLsUseRegisteredHTTPIngress(t *testing.T) {
	publicFLV := "https://public.example/rtp/stream.live.flv?play_token=fresh-token"
	publicHLS := "https://public.example/rtp/stream/hls.m3u8?play_token=fresh-token"
	result := &play.Result{App: "rtp", StreamID: "stream", Node: &play.ResultNode{ID: 7}, URLs: play.PlaybackURLs{HTTPFLV: &publicFLV, HLS: &publicHLS}}
	mediaNode := &node.Node{ID: 7, Host: "10.0.0.7", APIPort: 18080, PlaybackHost: "public.example", MediaServerUUID: "node-7"}
	flv, hls, err := videoShareInternalURLs(result, mediaNode, node.ServerConfig{HTTPPort: 80, HLSEnabled: true}, true)
	require.NoError(t, err)
	for _, raw := range []string{flv, hls} {
		parsed, err := url.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, "http", parsed.Scheme)
		require.Equal(t, "10.0.0.7:18080", parsed.Host, "registered API ingress may map the ZLM HTTP listener")
		require.Equal(t, "fresh-token", parsed.Query().Get("play_token"))
		require.NotContains(t, raw, "public.example")
	}
	require.Equal(t, "/rtp/stream.live.flv", mustVideoShareURL(t, flv).Path)
	require.Equal(t, "/rtp/stream/hls.m3u8", mustVideoShareURL(t, hls).Path)
}

func TestVideoShareInternalURLsRejectUnverifiedOrUnsupportedSources(t *testing.T) {
	result := &play.Result{App: "rtp", StreamID: "stream", Node: &play.ResultNode{ID: 7}}
	mediaNode := &node.Node{ID: 7, Host: "10.0.0.7", APIPort: 18080, MediaServerUUID: "node-7"}
	_, _, err := videoShareInternalURLs(result, mediaNode, node.ServerConfig{HTTPPort: 80}, true)
	require.ErrorIs(t, err, videoshare.ErrViewerUnavailable, "enabled auth must not become a bare internal URL")
	for _, mutate := range []func(*node.Node){
		func(n *node.Node) { n.ID = 8 },
		func(n *node.Node) { n.Host = "public.example/forward" },
		func(n *node.Node) { n.Host = "user:secret@private" },
		func(n *node.Node) { n.APIPort = 0 },
		func(n *node.Node) { n.MediaServerUUID = "" },
		func(n *node.Node) { n.State = node.StateOffline },
	} {
		copy := *mediaNode
		mutate(&copy)
		_, _, err = videoShareInternalURLs(result, &copy, node.ServerConfig{HTTPPort: 80}, false)
		require.ErrorIs(t, err, videoshare.ErrViewerUnavailable)
	}
	_, _, err = videoShareInternalURLs(result, mediaNode, node.ServerConfig{}, false)
	require.ErrorIs(t, err, videoshare.ErrViewerUnavailable)
}

func TestVideoShareInternalURLsSupportIPv6AndDisabledHLS(t *testing.T) {
	result := &play.Result{App: "rtp", StreamID: "stream", Node: &play.ResultNode{ID: 7}}
	mediaNode := &node.Node{ID: 7, Host: "::1", APIPort: 18080, MediaServerUUID: "node-7"}
	flv, hls, err := videoShareInternalURLs(result, mediaNode, node.ServerConfig{HTTPPort: 80}, false)
	require.NoError(t, err)
	require.Equal(t, "http://[::1]:18080/rtp/stream.live.flv", flv)
	require.Empty(t, hls)
}

func TestVideoShareInternalURLsRejectMixedAuthorization(t *testing.T) {
	flv := "https://public.example/rtp/stream.live.flv?play_token=first"
	hls := "https://public.example/rtp/stream/hls.m3u8?play_token=second"
	result := &play.Result{App: "rtp", StreamID: "stream", Node: &play.ResultNode{ID: 7}, URLs: play.PlaybackURLs{HTTPFLV: &flv, HLS: &hls}}
	mediaNode := &node.Node{ID: 7, Host: "10.0.0.7", APIPort: 18080, MediaServerUUID: "node-7"}
	_, _, err := videoShareInternalURLs(result, mediaNode, node.ServerConfig{HTTPPort: 80, HLSEnabled: true}, true)
	require.ErrorIs(t, err, videoshare.ErrViewerUnavailable)
}

func mustVideoShareURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

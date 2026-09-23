package node_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestParseServerConfig_DefaultsProtocolGenerationToEnabled(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{"http.port": "18080"})

	require.True(t, cfg.RTSPEnabled)
	require.True(t, cfg.RTMPEnabled)
	require.True(t, cfg.HLSEnabled)
	require.True(t, cfg.TSEnabled)
	require.True(t, cfg.FMP4Enabled)
}

func TestParseServerConfig_DisablesOnlyExplicitZero(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{
		"protocol.enable_rtsp": "0",
		"protocol.enable_rtmp": "0",
		"protocol.enable_hls":  "0",
		"protocol.enable_ts":   "0",
		"protocol.enable_fmp4": "0",
	})

	require.False(t, cfg.RTSPEnabled)
	require.False(t, cfg.RTMPEnabled)
	require.False(t, cfg.HLSEnabled)
	require.False(t, cfg.TSEnabled)
	require.False(t, cfg.FMP4Enabled)
}

func TestParseServerConfig_TreatsUnknownProtocolValuesAsEnabled(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{
		"protocol.enable_rtsp": "",
		"protocol.enable_rtmp": "true",
		"protocol.enable_hls":  "invalid",
		"protocol.enable_ts":   " 0 ",
		"protocol.enable_fmp4": "1",
	})

	require.True(t, cfg.RTSPEnabled)
	require.True(t, cfg.RTMPEnabled)
	require.True(t, cfg.HLSEnabled)
	require.True(t, cfg.TSEnabled)
	require.True(t, cfg.FMP4Enabled)
}

func TestParseServerConfig_PreservesMediaPorts(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{
		"http.port":      "18080",
		"http.sslport":   "18443",
		"rtmp.port":      "11935",
		"rtmp.sslport":   "11936",
		"rtsp.port":      "10554",
		"rtsp.sslport":   "10555",
		"rtp_proxy.port": "30000",
		"onvif.port":     "18000",
	})

	require.Equal(t, 18080, cfg.HTTPPort)
	require.Equal(t, 18443, cfg.HTTPSPort)
	require.Equal(t, 11935, cfg.RTMPPort)
	require.Equal(t, 11936, cfg.RTMPSPort)
	require.Equal(t, 10554, cfg.RTSPPort)
	require.Equal(t, 10555, cfg.RTSPSPort)
	require.Equal(t, 30000, cfg.RTPProxyPort)
	require.Equal(t, 18000, cfg.ONVIFPort)
}

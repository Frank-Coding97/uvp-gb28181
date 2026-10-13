package node_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
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

// 收流范围是**平台登记值的真源**：探活/首启拿到的这份配置决定
// meta_node.rtp_port_start/end 该是多少（见 rtp_range.go）。
func TestParseServerConfig_PreservesRTPPortRange(t *testing.T) {
	cfg := node.ParseServerConfig(map[string]string{"rtp_proxy.port_range": "51014-51063"})

	require.Equal(t, 51014, cfg.RTPPortRangeStart)
	require.Equal(t, 51063, cfg.RTPPortRangeEnd)
}

// 读不到 / 读不懂时必须留 0（"没有值"），不许猜一个默认段 ——
// 猜出来的值会被写进登记表，然后按它算端口容量。
func TestParseServerConfig_UnusableRTPPortRangeStaysZero(t *testing.T) {
	for _, value := range []string{"", "0-0", "51063-51014", "1-65536", "30000", "abc-def", "51014-51063 "} {
		t.Run(value, func(t *testing.T) {
			cfg := node.ParseServerConfig(map[string]string{"rtp_proxy.port_range": value})
			require.Zero(t, cfg.RTPPortRangeStart)
			require.Zero(t, cfg.RTPPortRangeEnd)
		})
	}

	cfg := node.ParseServerConfig(map[string]string{"http.port": "18080"})
	require.Zero(t, cfg.RTPPortRangeStart, "键缺失同样是 0")
	require.Zero(t, cfg.RTPPortRangeEnd)
}

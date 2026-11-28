package talk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestBroadcastTransportWarning(t *testing.T) {
	tests := []struct {
		name          string
		mode          models.TalkSessionMode
		senderMode    string
		remoteMediaIP string
		deviceIP      string
		want          string
	}{
		{
			name: "udp 同网段不提示",
			mode: models.TalkSessionModeBroadcast, senderMode: "udp",
			remoteMediaIP: "192.168.1.64", deviceIP: "192.168.1.10",
		},
		{
			name: "udp 跨网段提示",
			mode: models.TalkSessionModeBroadcast, senderMode: "udp",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
			want: "设备用 UDP 传音频，与平台不在同一网段，声音大概率传不到设备。请改用 Talk 模式，或在设备侧把媒体传输改成 TCP 主动（active）。",
		},
		{
			name: "设备主动连平台不提示",
			mode: models.TalkSessionModeBroadcast, senderMode: "tcp_passive",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
		},
		{
			name: "设备等平台连且跨网段要提示",
			mode: models.TalkSessionModeBroadcast, senderMode: "tcp_active",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
			want: "设备要求由平台主动连接，跨网段时连不进去。请改用 Talk 模式，或在设备侧把媒体传输改成 TCP 主动（active）。",
		},
		{
			name: "Talk 模式不判断",
			mode: models.TalkSessionModeTalk, senderMode: "udp",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
		},
		{
			name: "还没协商出传输方式不判断",
			mode: models.TalkSessionModeBroadcast, senderMode: "",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
		},
		{
			name: "媒体地址缺失不判断",
			mode: models.TalkSessionModeBroadcast, senderMode: "udp",
			remoteMediaIP: "", deviceIP: "203.0.113.7",
		},
		{
			name: "设备来源地址缺失不判断",
			mode: models.TalkSessionModeBroadcast, senderMode: "udp",
			remoteMediaIP: "192.168.1.64", deviceIP: "",
		},
		{
			name: "同一地址不提示",
			mode: models.TalkSessionModeBroadcast, senderMode: "udp",
			remoteMediaIP: "203.0.113.7", deviceIP: "203.0.113.7",
		},
		{
			name: "传输方式大小写不敏感",
			mode: models.TalkSessionModeBroadcast, senderMode: "TCP_PASSIVE",
			remoteMediaIP: "192.168.1.64", deviceIP: "203.0.113.7",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := BroadcastTransportWarning(test.mode, test.senderMode, test.remoteMediaIP, test.deviceIP)
			require.Equal(t, test.want, got)
		})
	}
}

func TestSameIPv4Subnet24(t *testing.T) {
	tests := []struct {
		a, b  string
		same  bool
		known bool
	}{
		{a: "192.168.1.10", b: "192.168.1.200", same: true, known: true},
		{a: "192.168.1.10", b: "192.168.2.10", same: false, known: true},
		{a: "10.0.0.1", b: "10.0.0.2", same: true, known: true},
		{a: "192.168.1.10", b: "not-an-ip", same: false, known: false},
		{a: "", b: "192.168.1.10", same: false, known: false},
	}
	for _, test := range tests {
		same, known := sameIPv4Subnet24(test.a, test.b)
		require.Equal(t, test.known, known, "known(%q,%q)", test.a, test.b)
		require.Equal(t, test.same, same, "same(%q,%q)", test.a, test.b)
	}
}

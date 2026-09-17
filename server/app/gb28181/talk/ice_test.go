package talk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// rtc.externIP 是「对端实际能连的地址」,也是跨网段开关:填了才下发 STUN。
func TestBuildICEServersDerivesSTUNFromExternalIP(t *testing.T) {
	got := BuildICEServers(node.ServerConfig{RTCICEPort: 3478, RTCExternalIP: "203.0.113.7"})
	require.Equal(t, []ICEServer{{URLs: []string{"stun:203.0.113.7:3478"}}}, got)
}

// externIP 被登记成 host:port 时,STUN 端口仍由 rtc.icePort 决定,不能拼成两段端口。
func TestBuildICEServersStripsExternalIPPort(t *testing.T) {
	got := BuildICEServers(node.ServerConfig{RTCICEPort: 3478, RTCExternalIP: "203.0.113.7:18443"})
	require.Equal(t, []ICEServer{{URLs: []string{"stun:203.0.113.7:3478"}}}, got)
}

func TestBuildICEServersBracketsIPv6(t *testing.T) {
	got := BuildICEServers(node.ServerConfig{RTCICEPort: 3478, RTCExternalIP: "fe80::1"})
	require.Equal(t, []ICEServer{{URLs: []string{"stun:[fe80::1]:3478"}}}, got)
}

// ⛔ 四种情况都返回 nil —— 调用方据此保持「不带 iceServers」的行为:
//   - 未声明 externIP(同网段部署,host candidate 就够;这是默认形态)
//   - 节点未开 RTC / 端口越界
//   - externIP 是空白串
//
// 「未声明 externIP」这条是本文件的重点:它曾经会退化成「用节点内网地址拼一个 STUN」,
// 结果节点 2 的 STUN 端口不可达,浏览器白等收集超时,对讲直接失败。
func TestBuildICEServersReturnsNilWhenNotDeclaredOrUnusable(t *testing.T) {
	cases := []struct {
		name   string
		config node.ServerConfig
	}{
		{"未声明 externIP(同网段)", node.ServerConfig{RTCICEPort: 3478}},
		{"externIP 只有空白", node.ServerConfig{RTCICEPort: 3478, RTCExternalIP: "   "}},
		{"节点未开 RTC", node.ServerConfig{RTCExternalIP: "203.0.113.7"}},
		{"端口越界", node.ServerConfig{RTCICEPort: 70000, RTCExternalIP: "203.0.113.7"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Nil(t, BuildICEServers(tc.config))
		})
	}
}

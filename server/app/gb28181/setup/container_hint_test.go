package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainerAddressWarning(t *testing.T) {
	bridgeContainer := []NetworkAddress{
		{IP: "0.0.0.0", CIDR: "0.0.0.0/0", ListenOnly: true},
		{IP: "172.17.0.2", InterfaceName: "eth0", CIDR: "172.17.0.2/16"},
	}
	hostNetworkContainer := []NetworkAddress{
		{IP: "0.0.0.0", CIDR: "0.0.0.0/0", ListenOnly: true},
		{IP: "192.168.10.52", InterfaceName: "enp1s0f0", CIDR: "192.168.10.52/24", Recommended: true},
		{IP: "172.17.0.1", InterfaceName: "docker0", CIDR: "172.17.0.1/16", Virtual: true, More: true},
	}

	cases := []struct {
		name        string
		items       []NetworkAddress
		inContainer bool
		wantEmpty   bool
	}{
		{name: "容器 + 只看得到容器自己的 eth0 ⇒ 提示", items: bridgeContainer, inContainer: true},
		{name: "容器 + 能看见 docker0（host 网络）⇒ 不提示", items: hostNetworkContainer, inContainer: true, wantEmpty: true},
		{name: "裸机 ⇒ 不提示", items: bridgeContainer, inContainer: false, wantEmpty: true},
		{name: "裸机上只有一张物理网卡 ⇒ 不提示", items: hostNetworkContainer, inContainer: false, wantEmpty: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContainerAddressWarning(tc.items, tc.inContainer)
			if tc.wantEmpty {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, containerAddressWarning, got)
			// 提示语必须点出"容器"与"改填宿主机地址"两件事，否则用户不知道怎么办。
			assert.Contains(t, got, "容器")
			assert.Contains(t, got, "宿主机")
		})
	}
}

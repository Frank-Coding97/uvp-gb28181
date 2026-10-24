package sip

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// TestCascadeClientSendsFromSharedListenerPort 锁住级联的「源端口对称」契约。
//
// 背景（验收实测）：向上级平台注册时 Via/Contact 声明的是平台 SIP 端口，
// 但 sipgo 客户端默认会为每个请求 **新建一个临时端口的 socket**，于是对端看到
// 的源端口与报文里声明的不一致。宽松的对端（LiveGBS/EasyGBS）照样处理；严格的
// 对端（WVP）按「来源地址不认识」整包丢弃 —— 现象是注册成功、心跳正常，但目录
// 推送过去 0 通道，且对端日志无任何错误。
//
// 监听 IP 用 0.0.0.0（线上默认），这正是缺陷的触发条件：通配监听在连接池里的键
// 是 "[::]:port"，与客户端钉住的 "具体IP:port" 字符串永远不相等，因此必须按端口
// 回退到监听 socket 才能发出正确的源端口。
func TestCascadeClientSendsFromSharedListenerPort(t *testing.T) {
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	sipPort := probe.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, probe.Close())

	cfg := testConfig()
	cfg.SIP.ListenIP = "0.0.0.0"
	cfg.SIP.AdvertiseIP = "127.0.0.1"
	cfg.SIP.Port = sipPort
	cfg.SIP.Transport = []string{"udp"}

	server, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	time.Sleep(100 * time.Millisecond)

	receiver, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = receiver.Close() }()

	factory, err := sipclient.NewRequestFactory(sipclient.Identity{
		UpstreamServerID: "34020000002000000001",
		UpstreamDomain:   "3402000000",
		Host:             "127.0.0.1",
		Port:             receiver.LocalAddr().(*net.UDPAddr).Port,
		LocalDeviceID:    "34020000002000000002",
		LocalDomain:      "3402000000",
		LocalIP:          "127.0.0.1",
		LocalPort:        sipPort,
		Transport:        "UDP",
		Profile:          protocol.Version2022,
	})
	require.NoError(t, err)
	request, err := factory.BuildMessage([]byte("<Query><CmdType>Keepalive</CmdType></Query>"), "cascade-source-port-call")
	require.NoError(t, err)
	require.Equal(t, sipPort, request.Laddr.Port, "请求必须钉住声明的本地端口")

	client, err := server.NewCascadeClient()
	require.NoError(t, err)
	require.NoError(t, client.WriteRequest(request))
	require.Equal(t, sipPort, request.Via().Port, "Via 必须声明平台 SIP 端口")

	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 64*1024)
	n, remote, err := receiver.ReadFrom(buf)
	require.NoError(t, err)
	require.Contains(t, string(buf[:n]), "MESSAGE sip:")
	_, sourcePort, err := net.SplitHostPort(remote.String())
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(sipPort), sourcePort,
		"级联请求必须从共享监听 socket 发出，源端口要与 Via/Contact 声明一致")
}

package sip

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"

	siplib "github.com/emiago/sipgo/sip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingLinkSink 记录被上抛的每一次「可靠传输断开」。
//
// 注意 sip.DeviceLinkSink 的方法是在 close observer 的调用栈上**同步**触发的,
// 所以这里不需要 channel/等待 —— 断言可以直接跑在调用之后。
type recordingLinkSink struct {
	mu    sync.Mutex
	calls []linkLossCall
}

type linkLossCall struct {
	transport  string
	remoteAddr string
}

func (s *recordingLinkSink) DeviceLinkLost(_ context.Context, transport, remoteAddr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, linkLossCall{transport: transport, remoteAddr: remoteAddr})
}

func (s *recordingLinkSink) snapshot() []linkLossCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]linkLossCall(nil), s.calls...)
}

// reliableProps 造一个「某可靠传输上的远端端点」props。
// transport 传空则默认 TCP。
func reliableProps(t *testing.T, transport, addr string) siplib.TransportReadProps {
	t.Helper()
	if transport == "" {
		transport = "TCP"
	}
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	return siplib.TransportReadProps{
		Transport:  transport,
		RemoteAddr: &net.TCPAddr{IP: net.ParseIP(host), Port: port},
	}
}

func TestDecideLinkLossFiresOnReliableDisconnect(t *testing.T) {
	addr, decision := decideLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"), false, nil)
	assert.Equal(t, linkLossFire, decision)
	assert.Equal(t, "203.0.113.5:51234", addr)
}

// 这条是整块改动的**主要防回归点**:平台侧先建好新连接、旧连接的 read loop 才退出时,
// 若不问「同端点是否已有新连接」,一次正常的设备自愈重连会被记成掉线。
func TestDecideLinkLossSkipsWhenSameEndpointReconnected(t *testing.T) {
	probe := func(transport, addr string) bool {
		assert.Equal(t, "TCP", transport)
		assert.Equal(t, "203.0.113.5:51234", addr)
		return true
	}
	addr, decision := decideLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"), false, probe)
	assert.Equal(t, linkLossSkipSuperseded, decision)
	assert.Equal(t, "203.0.113.5:51234", addr)
}

func TestDecideLinkLossSkipsUnreliableTransport(t *testing.T) {
	// UDP 没有「通道断开」这回事:同样的 props 换成 UDP 就必须被忽略,
	// 否则一台 UDP 设备会因为「某个 UDP 报文没收到」被判掉线。
	_, decision := decideLinkLoss(reliableProps(t, "UDP", "203.0.113.5:51234"), false, nil)
	assert.Equal(t, linkLossSkipNotReliable, decision)
}

func TestDecideLinkLossSkipsMissingRemoteAddr(t *testing.T) {
	_, decision := decideLinkLoss(siplib.TransportReadProps{Transport: "TCP"}, false, nil)
	assert.Equal(t, linkLossSkipNoAddr, decision)

	_, decision = decideLinkLoss(siplib.TransportReadProps{Transport: "TCP", RemoteAddr: &net.TCPAddr{}}, false, nil)
	assert.Equal(t, linkLossSkipNoAddr, decision, "远端地址为空串时同样无法定位设备")
}

func TestDecideLinkLossSkipsWhileMuted(t *testing.T) {
	// 关停中即使同端点没有新连接也不该判 —— 连接就是平台自己关的。
	_, decision := decideLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"), true, nil)
	assert.Equal(t, linkLossSkipMuted, decision)
}

func TestNotifyLinkLossForwardsToSink(t *testing.T) {
	sink := &recordingLinkSink{}
	s := &Server{deviceLinkSink: sink}

	s.notifyLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"))

	calls := sink.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, "TCP", calls[0].transport)
	assert.Equal(t, "203.0.113.5:51234", calls[0].remoteAddr)
}

func TestNotifyLinkLossSilentWhileShuttingDown(t *testing.T) {
	sink := &recordingLinkSink{}
	s := &Server{deviceLinkSink: sink}
	// Shutdown 一开始就会置这个位(见 Server.Shutdown),否则一次平台重启会给
	// 每台 TCP 设备都写一条「链路断开」。
	s.linkLossMuted.Store(true)

	s.notifyLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"))

	assert.Empty(t, sink.snapshot())
}

func TestNotifyLinkLossIsNoopWithoutSink(t *testing.T) {
	// 没装配 sink 时必须与历史版本行为一致:静默丢弃,不 panic。
	assert.NotPanics(t, func() {
		(&Server{}).notifyLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"))
		var nilServer *Server
		nilServer.notifyLinkLoss(reliableProps(t, "TCP", "203.0.113.5:51234"))
	})
}

func TestWithDeviceLinkSinkInstallsSink(t *testing.T) {
	sink := &recordingLinkSink{}
	options := serverOptions{}
	WithDeviceLinkSink(sink)(&options)
	assert.Same(t, sink, options.deviceLinkSink)
}

// HasConnection 是「同端点是否已有新连接」的唯一数据来源,它在 ua 缺失时必须
// 安全地返回 false —— 因为 false 的含义是「判定为掉线」,宁可多判也不能漏判。
func TestHasConnectionWithoutUAReturnsFalse(t *testing.T) {
	s := &Server{}
	assert.False(t, s.HasConnection("TCP", "203.0.113.5:51234"))
	assert.False(t, s.HasConnection("UDP", "203.0.113.5:51234"))
	assert.False(t, s.HasConnection("TCP", ""))
	assert.False(t, (*Server)(nil).HasConnection("TCP", "203.0.113.5:51234"))
}

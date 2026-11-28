package sip

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

// TestCascadeRegisterKeepsDeclaredPortAcrossDigestChallenge 覆盖真实对端的鉴权
// 路径：上级平台先回 401，级联必须用**同一个源端口**重发带 Authorization 的
// REGISTER，并且 Via 里的 host/port 仍是声明值。
//
// 401 重试是 sipgo 内部行为：它会删掉我们自建的 Via、按客户端默认 host 重建
// (端口由真实连接补齐)。若客户端没有钉住本地 socket/主机地址，重试报文要么换
// 了源端口（严格对端按来源地址不认识而丢弃），要么把通配地址 "::" 写进 Via。
func TestCascadeRegisterKeepsDeclaredPortAcrossDigestChallenge(t *testing.T) {
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	sipPort := probe.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, probe.Close())

	cfg := testConfig()
	cfg.SIP.ListenIP = "0.0.0.0"
	// 线上默认就是「监听通配地址 + 不配对外地址」，这样 sipgo 拿不到可用的
	// 主机名，只能靠请求自带的 Laddr 生成 Via —— 缺陷正是在这个组合下暴露。
	cfg.SIP.AdvertiseIP = ""
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

	platform := newFakeUpstreamPlatform(t)

	client, err := server.NewCascadeClient()
	require.NoError(t, err)
	transport, err := sipclient.NewSipgoTransport(client)
	require.NoError(t, err)
	factory, err := sipclient.NewRequestFactory(sipclient.Identity{
		UpstreamServerID: "34020000002000000001",
		UpstreamDomain:   "3402000000",
		Host:             "127.0.0.1",
		Port:             platform.port,
		LocalDeviceID:    "34020000002000000002",
		LocalDomain:      "3402000000",
		LocalIP:          "127.0.0.1",
		LocalPort:        sipPort,
		Transport:        "UDP",
		Profile:          protocol.Version2022,
	})
	require.NoError(t, err)
	transactions, err := sipclient.NewTransactionClient(factory, transport,
		sipclient.Credentials{Username: "34020000002000000002", Password: "secret"}, 3*time.Second)
	require.NoError(t, err)

	result := transactions.Register(context.Background(), 3600, "cascade-digest-call")
	require.NoError(t, result.BuildErr)
	require.NoError(t, result.TransportErr)
	require.True(t, result.Retried, "对端回了 401，应重发带鉴权的 REGISTER")
	require.True(t, result.Success, "鉴权后的 REGISTER 应成功")

	challenged, authenticated := platform.requests()
	for name, request := range map[string]fakePlatformRequest{"首次": challenged, "鉴权后": authenticated} {
		require.Equal(t, strconv.Itoa(sipPort), request.sourcePort, name+" REGISTER 的源端口必须等于声明端口")
		viaHost, viaPort := request.viaHostPort()
		require.Equal(t, "127.0.0.1", viaHost, name+" REGISTER 的 Via host 不能是通配地址")
		require.Equal(t, sipPort, viaPort, name+" REGISTER 的 Via port 必须是声明端口")
	}
}

type fakePlatformRequest struct {
	raw        []byte
	sourcePort string
}

func (r fakePlatformRequest) header(name string) string {
	for _, line := range strings.Split(string(r.raw), "\r\n") {
		if separator := strings.IndexByte(line, ':'); separator > 0 &&
			strings.EqualFold(strings.TrimSpace(line[:separator]), name) {
			return strings.TrimSpace(line[separator+1:])
		}
	}
	return ""
}

func (r fakePlatformRequest) viaHostPort() (string, int) {
	value := r.header("Via")
	if separator := strings.LastIndexByte(value, ';'); separator > 0 {
		value = value[:separator]
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(value[strings.LastIndexByte(value, ' ')+1:]))
	if err != nil {
		return value, 0
	}
	parsed, _ := strconv.Atoi(port)
	return host, parsed
}

type fakeUpstreamPlatform struct {
	conn          net.PacketConn
	port          int
	first         chan fakePlatformRequest
	second        chan fakePlatformRequest
	authenticated chan fakePlatformRequest
	done          chan struct{}
}

// newFakeUpstreamPlatform 模拟上级平台：第一包回 401 挑战，第二包回 200 OK。
func newFakeUpstreamPlatform(t *testing.T) *fakeUpstreamPlatform {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	platform := &fakeUpstreamPlatform{
		conn:          conn,
		port:          conn.LocalAddr().(*net.UDPAddr).Port,
		first:         make(chan fakePlatformRequest, 1),
		second:        make(chan fakePlatformRequest, 1),
		authenticated: make(chan fakePlatformRequest, 1),
		done:          make(chan struct{}),
	}
	t.Cleanup(func() {
		close(platform.done)
		_ = conn.Close()
	})
	go platform.serve()
	return platform
}

func (p *fakeUpstreamPlatform) requests() (fakePlatformRequest, fakePlatformRequest) {
	select {
	case first := <-p.first:
		return first, <-p.second
	case <-time.After(5 * time.Second):
		return fakePlatformRequest{}, fakePlatformRequest{}
	}
}

func (p *fakeUpstreamPlatform) serve() {
	buffer := make([]byte, 64*1024)
	for attempt := 0; attempt < 2; attempt++ {
		_ = p.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, source, err := p.conn.ReadFrom(buffer)
		if err != nil {
			return
		}
		raw := append([]byte(nil), buffer[:n]...)
		request := fakePlatformRequest{raw: raw}
		if _, port, splitErr := net.SplitHostPort(source.String()); splitErr == nil {
			request.sourcePort = port
		}
		if attempt == 0 {
			p.first <- request
			_, _ = p.conn.WriteTo(buildChallengeResponse(request, "401", "Unauthorized"), source)
			continue
		}
		p.second <- request
		if request.header("Authorization") != "" {
			p.authenticated <- request
		}
		_, _ = p.conn.WriteTo(buildChallengeResponse(request, "200", "OK"), source)
	}
}

func buildChallengeResponse(request fakePlatformRequest, code, reason string) []byte {
	var response bytes.Buffer
	fmt.Fprintf(&response, "SIP/2.0 %s %s\r\n", code, reason)
	response.WriteString("Via: " + request.header("Via") + "\r\n")
	response.WriteString("From: " + request.header("From") + "\r\n")
	to := request.header("To")
	if !strings.Contains(to, "tag=") {
		to += ";tag=cascade-platform-1"
	}
	response.WriteString("To: " + to + "\r\n")
	response.WriteString("Call-ID: " + request.header("Call-ID") + "\r\n")
	response.WriteString("CSeq: " + request.header("CSeq") + "\r\n")
	if code == "401" {
		response.WriteString(`WWW-Authenticate: Digest realm="3402000000", nonce="cascade-nonce-1", algorithm=MD5` + "\r\n")
	}
	response.WriteString("Content-Length: 0\r\n\r\n")
	return response.Bytes()
}

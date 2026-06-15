package sip

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

func init() {
	// SIP server 用到 app.ZapLog,测试里给个 no-op logger
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
}

func testConfig() gbconfig.Config {
	return gbconfig.Config{
		Enabled: true,
		SIP: gbconfig.SIPConfig{
			IP:        "127.0.0.1",
			Port:      15060, // 测试用高端口,避开 5060
			Transport: []string{"udp", "tcp"},
			Domain:    "3402000000",
			ServerID:  "34020000002000000001",
			Password:  "12345678",
		},
		Device: gbconfig.DeviceConfig{
			KeepaliveInterval:     60,
			KeepaliveTimeoutCount: 3,
			OfflineScanInterval:   30,
		},
	}
}

// TestServerDualStackListen T3-测1: 启动后 UDP+TCP 端口均监听
func TestServerDualStackListen(t *testing.T) {
	s, err := NewServer(testConfig())
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()

	time.Sleep(300 * time.Millisecond) // 等监听就绪

	// UDP:15060 可拨号
	udpConn, err := net.DialTimeout("udp", "127.0.0.1:15060", time.Second)
	if err != nil {
		t.Errorf("UDP 15060 拨号失败: %v", err)
	} else {
		udpConn.Close()
	}

	// TCP:15060 可建连
	tcpConn, err := net.DialTimeout("tcp", "127.0.0.1:15060", time.Second)
	if err != nil {
		t.Errorf("TCP 15060 建连失败(TCP 监听未起): %v", err)
	} else {
		tcpConn.Close()
	}
}

// TestServerReceiveRegister T3-测2/3: 用 sipgo client 发 REGISTER,handler 被触发回 200
func TestServerReceiveRegister(t *testing.T) {
	s, err := NewServer(testConfig())
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
	time.Sleep(300 * time.Millisecond)

	// 用 sipgo client 发 REGISTER(它正确处理 Via/事务/响应路由)
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("test-client"))
	if err != nil {
		t.Fatalf("client UA 失败: %v", err)
	}
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname("127.0.0.1"))
	if err != nil {
		t.Fatalf("NewClient 失败: %v", err)
	}
	defer client.Close()

	recipient := sip.Uri{}
	sip.ParseUri("sip:34020000002000000001@127.0.0.1:15060", &recipient)
	req := sip.NewRequest(sip.REGISTER, recipient)
	req.AppendHeader(sip.NewHeader("Contact", "<sip:34020000001320000001@127.0.0.1>"))
	req.SetTransport("UDP")

	ctx := context.Background()
	tx, err := client.TransactionRequest(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		t.Fatalf("发起事务失败: %v", err)
	}
	defer tx.Terminate()

	select {
	case res := <-tx.Responses():
		if res == nil {
			t.Fatal("收到 nil 响应")
		}
		t.Logf("收到响应: %d %s", res.StatusCode, res.Reason)
		// handler 已接入真实注册逻辑:无 Authorization 的 REGISTER 应回 401 挑战
		// 这里只验证"报文进入 handler 并有响应",401 即证明 handler 工作
		// (完整 401→鉴权→建档流程在 handler 包的 T4 测试覆盖)
		if res.StatusCode != 401 && res.StatusCode != 200 {
			t.Errorf("期望 401 挑战或 200,实际 %d", res.StatusCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("超时未收到 SIP 响应(handler 未触发?)")
	}
}

// TestServerGracefulShutdown T3-测4: Shutdown 后端口释放
func TestServerGracefulShutdown(t *testing.T) {
	s, err := NewServer(testConfig())
	if err != nil {
		t.Fatalf("NewServer 失败: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	// 关闭后 TCP 应拒绝连接
	conn, err := net.DialTimeout("tcp", "127.0.0.1:15060", 500*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Errorf("Shutdown 后 TCP 15060 仍可连接(端口未释放)")
	}
}

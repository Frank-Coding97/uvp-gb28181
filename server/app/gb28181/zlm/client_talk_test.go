package zlm

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestStartSendRtpPassiveTalkParameters(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/startSendRtpPassive" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		want := map[string]string{
			"vhost": "__defaultVhost__", "app": "talk", "stream": "source-1",
			"ssrc": "0200000001", "type": "0", "only_audio": "1", "pt": "8",
			"is_udp": "0", "recv_stream_id": "recv-1", "enable_origin_recv_limit": "1",
			"close_delay_ms": "12000",
		}
		for key, value := range want {
			if got := r.URL.Query().Get(key); got != value {
				t.Errorf("%s=%q, want %q", key, got, value)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"local_port":32100}`))
	})
	defer server.Close()

	result, err := c.StartSendRtpPassive(context.Background(), TalkSendRtpRequest{
		VHost: "__defaultVhost__", App: "talk", SourceStream: "source-1",
		RecvStreamID: "recv-1", SSRC: "0200000001", CloseDelayMS: 12000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.LocalPort != 32100 {
		t.Fatalf("localPort=%d", result.LocalPort)
	}
}

func TestStartBroadcastSendRtpParameters(t *testing.T) {
	for _, test := range []struct {
		name  string
		isUDP bool
		want  string
	}{{name: "udp", isUDP: true, want: "1"}, {name: "tcp active", isUDP: false, want: "0"}} {
		t.Run(test.name, func(t *testing.T) {
			c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/index/api/startSendRtp" {
					t.Fatalf("path=%s", r.URL.Path)
				}
				want := map[string]string{
					"vhost": "__defaultVhost__", "app": "talk", "stream": "source-1",
					"ssrc": "0200000001", "dst_url": "192.0.2.20", "dst_port": "30000",
					"is_udp": test.want, "only_audio": "1", "pt": "8", "use_ps": "0",
				}
				for key, value := range want {
					if got := r.URL.Query().Get(key); got != value {
						t.Errorf("%s=%q, want %q", key, got, value)
					}
				}
				_, _ = w.Write([]byte(`{"code":0,"local_port":31000}`))
			})
			defer server.Close()

			result, err := c.StartBroadcastSendRtp(context.Background(), BroadcastSendRtpRequest{
				VHost: "__defaultVhost__", App: "talk", SourceStream: "source-1", SSRC: "0200000001",
				RemoteIP: "192.0.2.20", RemotePort: 30000, IsUDP: test.isUDP,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.LocalPort != 31000 {
				t.Fatalf("localPort=%d", result.LocalPort)
			}
		})
	}
}

func TestStartSendRtpPassiveRejectsInvalidCloseDelay(t *testing.T) {
	c := &Client{}
	_, err := c.StartSendRtpPassive(context.Background(), TalkSendRtpRequest{
		VHost: "__defaultVhost__", App: "talk", SourceStream: "source",
		RecvStreamID: "recv", SSRC: "1", CloseDelayMS: 9000,
	})
	if err == nil {
		t.Fatal("closeDelayMS outside 10000..15000 must fail before request")
	}
}

func TestStartSendRtpPassiveMissingSourceReturnsZLMError(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the source stream","local_port":0}`))
	})
	defer server.Close()

	result, err := c.StartSendRtpPassive(context.Background(), TalkSendRtpRequest{
		VHost: "__defaultVhost__", App: "talk", SourceStream: "missing-source",
		RecvStreamID: "recv-1", SSRC: "0200000001", CloseDelayMS: 12000,
	})
	if err == nil || result != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStopSendRtpIncludesSSRCAndMissingIsIdempotent(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/stopSendRtp" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ssrc"); got != "0200000001" {
			t.Fatalf("ssrc=%q", got)
		}
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	if err := c.StopSendRtp(context.Background(), "__defaultVhost__", "talk", "source-1", "0200000001"); err != nil {
		t.Fatalf("missing sender must be idempotent: %v", err)
	}
}

func TestStopSendRtpRequiresSSRC(t *testing.T) {
	c := &Client{}
	if err := c.StopSendRtp(context.Background(), "__defaultVhost__", "talk", "source-1", ""); err == nil {
		t.Fatal("empty SSRC could stop unrelated senders")
	}
}

// TestStopSendRtpTreatsMissingSenderAsIdempotent 锚定真机行为：**流还在、但该 ssrc 没有发送会话**
// 时 ZLM 回的是 `code=-1 "stopSendRtp failed"`，而不是流不存在时的 `-500`。
//
// 设备从未应答的对讲会话（local_port=0）正是这个形态；若当失败处理，清理链路会把它记成
// "必须重试"，租约吊在 stopping、永不落终态 ⇒ DELETE 返回 500「语音对讲操作失败」。
// 现场证据：2026-09-17 会话 error 列原文 `stopSendRtp code=-1 msg=stopSendRtp failed`。
func TestStopSendRtpTreatsMissingSenderAsIdempotent(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"stopSendRtp failed"}`))
	})
	defer server.Close()

	if err := c.StopSendRtp(context.Background(), "__defaultVhost__", "talk", "source-1", "0200000001"); err != nil {
		t.Fatalf("no matching sender must be idempotent: %v", err)
	}
}

// TestStopSendRtpStillReportsExceptionCode 是反向锚点：幂等只覆盖已定义的"无物可停"返回码，
// -400（对端抛异常）必须继续报错，避免把幂等放宽成"任何非零码都算成功"。
func TestStopSendRtpStillReportsExceptionCode(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-400,"msg":"process exception"}`))
	})
	defer server.Close()

	err := c.StopSendRtp(context.Background(), "__defaultVhost__", "talk", "source-1", "0200000001")
	if err == nil {
		t.Fatal("-400 exception must not be swallowed")
	}
	if !strings.Contains(err.Error(), "exception") {
		t.Fatalf("error should carry the semantic code label, got: %v", err)
	}
}

func TestCloseTalkSourceUsesForcedCloseStreams(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/close_streams" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		want := map[string]string{
			"vhost": "__defaultVhost__", "app": "talk", "stream": "source-1", "force": "1",
		}
		for key, value := range want {
			if got := r.URL.Query().Get(key); got != value {
				t.Errorf("%s=%q, want %q", key, got, value)
			}
		}
		_, _ = w.Write([]byte(`{"code":-500,"msg":"not found"}`))
	})
	defer server.Close()

	if err := c.CloseTalkSource(context.Background(), "__defaultVhost__", "talk", "source-1"); err != nil {
		t.Fatalf("missing source must be idempotent: %v", err)
	}
}

package zlm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestClientProxyAddSendsTypedParameters(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/addStreamProxy" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		want := map[string]string{
			"secret":      "test-secret",
			"vhost":       "__defaultVhost__",
			"app":         "live",
			"stream":      "camera/1",
			"url":         "rtsp://user:pass@example.com/live/camera?token=a&x=1",
			"retry_count": "3",
			"rtp_type":    "2",
			"timeout_sec": "2.5",
		}
		for key, expected := range want {
			if got := r.URL.Query().Get(key); got != expected {
				t.Errorf("%s=%q, want %q", key, got, expected)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"key":"__defaultVhost__/live/camera/1"}}`))
	})
	defer server.Close()

	result, err := client.AddStreamProxy(context.Background(), StreamProxyRequest{
		VHost: "__defaultVhost__", App: "live", Stream: "camera/1",
		URL:        "rtsp://user:pass@example.com/live/camera?token=a&x=1",
		RetryCount: 3, RTPType: 2, TimeoutSec: 2.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Key != "__defaultVhost__/live/camera/1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientProxyLifecycle(t *testing.T) {
	t.Run("pull list and delete", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/index/api/listStreamProxy":
				_, _ = w.Write([]byte(`{"code":0,"data":{"proxy-key":{"url":"rtsp://example.com/live","status":0,"status_str":"online","liveSecs":12,"rePullCount":2,"totalReaderCount":4,"bytesSpeed":100,"totalBytes":200,"src":{"vhost":"__defaultVhost__","app":"live","stream":"camera"}}}}`))
			case "/index/api/delStreamProxy":
				if got := r.URL.Query().Get("key"); got != "proxy-key" {
					t.Errorf("key=%q", got)
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"flag":true}}`))
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
		})
		defer server.Close()

		list, err := client.ListStreamProxy(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Key != "proxy-key" || list[0].StatusStr != "online" || list[0].Src == nil || list[0].Src.Stream != "camera" {
			t.Fatalf("list=%+v", list)
		}
		deleted, err := client.DeleteStreamProxyWithResult(context.Background(), "proxy-key")
		if err != nil {
			t.Fatal(err)
		}
		if deleted == nil || !deleted.Hit || deleted.Key != "proxy-key" {
			t.Fatalf("deleted=%+v", deleted)
		}
	})

	t.Run("push add list and delete", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/index/api/addStreamPusherProxy":
				want := map[string]string{
					"schema": "rtmp", "vhost": "__defaultVhost__", "app": "live",
					"stream": "camera", "dst_url": "rtmp://user:pass@example.net/live/camera",
					"retry_count": "2", "rtp_type": "1", "timeout_sec": "3",
				}
				for key, expected := range want {
					if got := r.URL.Query().Get(key); got != expected {
						t.Errorf("%s=%q, want %q", key, got, expected)
					}
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"key":"push-key"}}`))
			case "/index/api/listStreamPusherProxy":
				_, _ = w.Write([]byte(`{"code":0,"data":[{"key":"push-key","url":"rtmp://example.com/live","status":1,"liveSecs":8,"rePublishCount":1,"bytesSpeed":10,"totalBytes":20,"src":{"vhost":"__defaultVhost__","app":"live","stream":"camera"}}]}`))
			case "/index/api/delStreamPusherProxy":
				if got := r.URL.Query().Get("key"); got != "push-key" {
					t.Errorf("key=%q", got)
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"flag":true}}`))
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
		})
		defer server.Close()

		result, err := client.AddStreamPusherProxy(context.Background(), StreamPusherProxyRequest{
			Schema: "rtmp", VHost: "__defaultVhost__", App: "live", Stream: "camera",
			DstURL: "rtmp://user:pass@example.net/live/camera", RetryCount: 2, RTPType: 1, TimeoutSec: 3,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || result.Key != "push-key" {
			t.Fatalf("result=%+v", result)
		}

		list, err := client.ListStreamPusherProxy(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Key != "push-key" || list[0].Status != 1 || list[0].Src == nil {
			t.Fatalf("list=%+v", list)
		}
		deleted, err := client.DeleteStreamPusherProxyWithResult(context.Background(), "push-key")
		if err != nil {
			t.Fatal(err)
		}
		if deleted == nil || !deleted.Hit || deleted.Key != "push-key" {
			t.Fatalf("deleted=%+v", deleted)
		}
	})
}

func TestClientFFmpegSourceLifecycle(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/addFFmpegSource":
			want := map[string]string{
				"src_url":        "https://user:pass@example.com/live.m3u8?token=abc&x=1",
				"dst_url":        "rtmp://127.0.0.1/live/camera",
				"timeout_ms":     "10000",
				"ffmpeg_cmd_key": "ffmpeg.cmd_hd",
				"enable_hls":     "1",
				"enable_mp4":     "0",
			}
			for key, expected := range want {
				if got := r.URL.Query().Get(key); got != expected {
					t.Errorf("%s=%q, want %q", key, got, expected)
				}
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"key":"ffmpeg-key"}}`))
		case "/index/api/listFFmpegSource":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"key":"ffmpeg-key","src_url":"https://example.com/live.m3u8","dst_url":"rtmp://127.0.0.1/live/camera","cmd":"ffmpeg --secret-leak","ffmpeg_cmd_key":"ffmpeg.cmd_hd"}]}`))
		case "/index/api/delFFmpegSource":
			if got := r.URL.Query().Get("key"); got != "ffmpeg-key" {
				t.Errorf("key=%q", got)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"flag":true}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	defer server.Close()

	result, err := client.AddFFmpegSource(context.Background(), FFmpegSourceRequest{
		SrcURL: "https://user:pass@example.com/live.m3u8?token=abc&x=1",
		DstURL: "rtmp://127.0.0.1/live/camera", TimeoutMS: 10000,
		FFmpegCmdKey: "ffmpeg.cmd_hd", EnableHLS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Key != "ffmpeg-key" {
		t.Fatalf("result=%+v", result)
	}

	list, err := client.ListFFmpegSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Key != "ffmpeg-key" || list[0].FFmpegCmdKey != "ffmpeg.cmd_hd" {
		t.Fatalf("list=%+v", list)
	}
	if err := client.DeleteFFmpegSource(context.Background(), "ffmpeg-key"); err != nil {
		t.Fatal(err)
	}
}

func TestClientListsRTPServersWithIdentityAndReleaseState(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/listRtpServer" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":[{"vhost":"__defaultVhost__","app":"rtp","stream_id":"34020000001320000001","port":30000,"ssrc":123456,"tcp_mode":1,"only_track":2,"released":true}]}`))
	})
	defer server.Close()

	list, err := client.ListRtpServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list=%+v", list)
	}
	got := list[0]
	if got.VHost != "__defaultVhost__" || got.App != "rtp" || got.StreamID != "34020000001320000001" || got.Port != 30000 || got.SSRC != "123456" || got.TCPMode != 1 || got.OnlyTrack != 2 || !got.Released {
		t.Fatalf("server=%+v", got)
	}
}

func TestClientRTPCloseReportsReleasedResource(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/closeRtpServer" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		for key, want := range map[string]string{
			"vhost": "__defaultVhost__", "app": "rtp", "stream_id": "stream-1",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Errorf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"hit":0}`))
	})
	defer server.Close()

	result, err := client.CloseRtpServerWithResult(context.Background(), "__defaultVhost__", "rtp", "stream-1")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Hit || !result.Released || result.StreamID != "stream-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientProxyRedaction(t *testing.T) {
	const sourceURL = "rtsp://user:pass@example.com/live/camera?token=abc&x=1"
	const targetURL = "rtmp://user:pass@example.net/live/camera?sig=xyz"
	client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"cannot connect ` + sourceURL + ` -> ` + targetURL + ` secret=test-secret"}`))
	})
	defer server.Close()

	_, err := client.AddStreamProxy(context.Background(), StreamProxyRequest{
		VHost: "__defaultVhost__", App: "live", Stream: "camera", URL: sourceURL,
	})
	if err == nil {
		t.Fatal("expected ZLM error")
	}
	for _, secret := range []string{"user", "pass", "abc", "xyz", "test-secret", sourceURL, targetURL} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks %q: %q", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "addStreamProxy") {
		t.Fatalf("error should retain operation name: %q", err)
	}
}

func TestClientProxyRedactsURLsFromTransportErrors(t *testing.T) {
	const sourceURL = "rtsp://user:pass@example.com/live/camera?token=abc&x=1"
	client, server := newMockClient(t, func(http.ResponseWriter, *http.Request) {})
	defer server.Close()
	client.http = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New(request.URL.String())
	})}

	_, err := client.AddStreamProxy(context.Background(), StreamProxyRequest{
		VHost: "__defaultVhost__", App: "live", Stream: "camera", URL: sourceURL,
	})
	if err == nil {
		t.Fatal("expected transport error")
	}
	for _, sensitive := range []string{"user", "pass", "abc", sourceURL} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("transport error leaks %q: %q", sensitive, err)
		}
	}
}

func TestClientProxyValidation(t *testing.T) {
	client := &Client{}
	_, err := client.AddStreamProxy(context.Background(), StreamProxyRequest{})
	if !errors.Is(err, ErrIngressInvalidRequest) {
		t.Fatalf("err=%v, want ErrIngressInvalidRequest", err)
	}
}

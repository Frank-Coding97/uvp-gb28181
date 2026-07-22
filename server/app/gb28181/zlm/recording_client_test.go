package zlm

import (
	"context"
	"net/http"
	"testing"
)

func TestStartRecordSendsMP4Parameters(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/index/api/startRecord" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		for key, want := range map[string]string{
			"type": "1", "vhost": "__defaultVhost__", "app": "rtp",
			"stream": "stream-1", "max_second": "0",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("%s=%q, want %q", key, got, want)
			}
		}
		_, _ = w.Write([]byte(`{"code":0,"result":true,"msg":"success"}`))
	})
	defer server.Close()

	if err := client.StartRecord(context.Background(), "__defaultVhost__", "rtp", "stream-1", 0); err != nil {
		t.Fatalf("StartRecord failed: %v", err)
	}
}

func TestIsRecordingAndMissingStream(t *testing.T) {
	t.Run("recording", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"status":true}`))
		})
		defer server.Close()
		got, err := client.IsRecording(context.Background(), "__defaultVhost__", "rtp", "stream")
		if err != nil || !got {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		client, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
		})
		defer server.Close()
		got, err := client.IsRecording(context.Background(), "__defaultVhost__", "rtp", "missing")
		if err != nil || got {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
}

func TestStopRecordIsIdempotentForMissingStream(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/stopRecord" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	if err := client.StopRecord(context.Background(), "__defaultVhost__", "rtp", "missing"); err != nil {
		t.Fatalf("missing stream should be idempotent: %v", err)
	}
}

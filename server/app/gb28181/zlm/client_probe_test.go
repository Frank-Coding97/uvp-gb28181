package zlm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type delayedProbeRoundTripper func(*http.Request) (*http.Response, error)

func (f delayedProbeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProbeHTTPTimeoutFollowsSamplingDuration(t *testing.T) {
	if got, want := probeHTTPTimeout(3000), 13*time.Second; got != want {
		t.Fatalf("3s probe timeout=%s, want %s", got, want)
	}
	if got, want := probeHTTPTimeout(60000), 70*time.Second; got != want {
		t.Fatalf("60s probe timeout=%s, want %s", got, want)
	}
}

func TestAddProbeDoesNotUseShortControlPlaneTimeout(t *testing.T) {
	c := NewClientForNode(&node.Node{Host: "127.0.0.1", APIPort: 80, APISecret: "test-secret"})
	c.http = &http.Client{Timeout: time.Millisecond, Transport: delayedProbeRoundTripper(func(*http.Request) (*http.Response, error) {
		time.Sleep(20 * time.Millisecond)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":[]}`)), Header: make(http.Header)}, nil
	})}
	if _, err := c.AddProbe(context.Background(), "__defaultVhost__", "rtp", "stream-1", 3000); err != nil {
		t.Fatalf("addProbe should use duration-aware timeout: %v", err)
	}
}

func TestAddProbeParsesFramesAndUsesFixedDuration(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index/api/addProbe" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("vhost") != "__defaultVhost__" || query.Get("app") != "rtp" || query.Get("stream") != "stream-1" || query.Get("probe_ms") != "3000" {
			t.Fatalf("unexpected query: %v", query)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":[{"codec":"H264","track_type":"video","dts":100,"pts":110,"recv_stamp":20,"frame_size":4096,"index":224,"key_frame":true,"config_frame":false}]}`))
	})
	defer server.Close()

	frames, err := c.AddProbe(context.Background(), "__defaultVhost__", "rtp", "stream-1", 3000)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Codec != "H264" || !frames[0].KeyFrame || frames[0].PTS != 110 || frames[0].FrameSize != 4096 {
		t.Fatalf("unexpected frames: %+v", frames)
	}
}

func TestAddProbeReturnsStableErrorForMissingStream(t *testing.T) {
	c, server := newMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer server.Close()

	if _, err := c.AddProbe(context.Background(), "__defaultVhost__", "rtp", "missing", 3000); err == nil {
		t.Fatal("expected error")
	}
}

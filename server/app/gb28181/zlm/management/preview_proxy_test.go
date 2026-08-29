package management

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestManagementPreviewSecureURLRequiresRuntimeSecurePort(t *testing.T) {
	identity := MediaIdentity{Schema: "rtsp", Vhost: "vhost-a", App: "camera", Stream: "stream/a"}
	_, err := BuildSecurePreviewURL("media.example", node.ServerConfig{HTTPPort: 8080}, identity, "https-flv", "signed-token")
	if !errors.Is(err, ErrPreviewUnsupported) {
		t.Fatalf("insecure fallback error=%v", err)
	}

	raw, err := BuildSecurePreviewURL("media.example", node.ServerConfig{HTTPPort: 8080, HTTPSPort: 8443}, identity, "https-flv", "signed-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "https://media.example:8443/") || strings.Contains(raw, ":8080") {
		t.Fatalf("secure URL downgraded or wrong port: %s", raw)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Query().Get(PreviewQueryParameter) != "signed-token" || parsed.Query().Get("vhost") != "vhost-a" {
		t.Fatalf("secure URL query=%v err=%v", parsed.Query(), err)
	}
}

func TestManagementPreviewSecureURLUsesEveryRuntimeSecureProtocolPort(t *testing.T) {
	identity := MediaIdentity{Schema: "rtsp", Vhost: "vhost-a", App: "camera", Stream: "stream/a"}
	config := node.ServerConfig{
		HTTPSPort: 8443, RTMPSPort: 11936, RTSPSPort: 10555,
		RTMPEnabled: true, RTSPEnabled: true, HLSEnabled: true, FMP4Enabled: true,
	}
	wants := map[string]string{
		"https-flv":  "https://media.example:8443/",
		"wss-flv":    "wss://media.example:8443/",
		"https-fmp4": "https://media.example:8443/",
		"wss-fmp4":   "wss://media.example:8443/",
		"https-hls":  "https://media.example:8443/",
		"rtmps":      "rtmps://media.example:11936/",
		"rtsps":      "rtsps://media.example:10555/",
	}
	for protocol, prefix := range wants {
		t.Run(protocol, func(t *testing.T) {
			token := protocol + "-signed-token"
			raw, err := BuildSecurePreviewURL("media.example", config, identity, protocol, token)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(raw, prefix) || !strings.Contains(raw, "media_access_token="+url.QueryEscape(token)) {
				t.Fatalf("protocol=%s url=%s", protocol, raw)
			}
		})
	}
	for protocol := range wants {
		if _, err := BuildSecurePreviewURL("media.example", node.ServerConfig{HTTPPort: 8080}, identity, protocol, protocol+"-signed-token"); !errors.Is(err, ErrPreviewUnsupported) {
			t.Fatalf("protocol=%s missing secure port error=%v", protocol, err)
		}
	}
}

func TestManagementPreviewJPEGProxyValidatesMIMEAndMagicAndBounds(t *testing.T) {
	const token = "must-not-appear"
	validJPEG := []byte{0xff, 0xd8, 0xff, 0xd9}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", r.URL.Query().Get("mode"))
		if r.URL.Query().Get("mode") == "image/jpeg" {
			_, _ = w.Write(validJPEG)
			return
		}
		_, _ = w.Write([]byte("upstream-secret-body"))
	}))
	defer server.Close()

	identity := MediaIdentity{Schema: "rtsp", Vhost: "vhost-a", App: "camera", Stream: "stream-1"}
	builder := func(context.Context, *node.Node, MediaIdentity) (string, error) {
		return server.URL + "/snapshot?mode=image/jpeg&token=" + token, nil
	}
	proxy := NewJPEGProxy(builder)
	result, err := proxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Body) != string(validJPEG) || result.ContentType != "image/jpeg" {
		t.Fatalf("result=%+v", result)
	}

	for name, mode := range map[string]string{
		"wrong-mime": "image/png", "missing-mime": "", "mime-parameters": "image/jpeg; charset=utf-8",
	} {
		t.Run(name, func(t *testing.T) {
			bad := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) {
				return server.URL + "/snapshot?mode=" + mode, nil
			})
			_, err := bad.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity)
			if !errors.Is(err, ErrSnapshotInvalidResponse) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "upstream-secret-body") || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("error leaked sensitive data: %v", err)
			}
		})
	}
	non200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "upstream-secret-body")
	}))
	defer non200.Close()
	non200Proxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return non200.URL, nil })
	_, err = non200Proxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity)
	if !errors.Is(err, ErrSnapshotInvalidResponse) {
		t.Fatalf("non-200 error=%v", err)
	}
	if strings.Contains(err.Error(), "upstream-secret-body") || strings.Contains(err.Error(), non200.URL) {
		t.Fatalf("non-200 leaked response details: %v", err)
	}

	builderErr := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) {
		return "http://internal.example/snapshot?token=" + token, errors.New("internal builder details: " + token)
	})
	if _, err := builderErr.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity); !errors.Is(err, ErrSnapshotUpstream) {
		t.Fatalf("builder error=%v", err)
	} else if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "internal.example") {
		t.Fatalf("builder error leaked details: %v", err)
	}

	magicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "not-jpeg")
	}))
	defer magicServer.Close()
	magicProxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return magicServer.URL, nil })
	if _, err := magicProxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity); !errors.Is(err, ErrSnapshotInvalidResponse) {
		t.Fatalf("magic error=%v", err)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(append([]byte{0xff, 0xd8, 0xff}, make([]byte, 2<<20)...))
	}))
	defer largeServer.Close()
	largeProxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return largeServer.URL, nil })
	if _, err := largeProxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity); !errors.Is(err, ErrSnapshotTooLarge) {
		t.Fatalf("large error=%v", err)
	}
	clamped := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return largeServer.URL, nil }, WithSnapshotMaxBytes(8<<20), WithSnapshotTimeout(time.Hour))
	if clamped.maxBytes != MaxSnapshotBytes || clamped.timeout != MaxSnapshotTimeout {
		t.Fatalf("unsafe options were not bounded: max=%d timeout=%s", clamped.maxBytes, clamped.timeout)
	}
}

func TestManagementPreviewJPEGProxyRejectsRedirectAndTimeoutAndMarksBeforeWrite(t *testing.T) {
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xff, 0xd8, 0xff})
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	identity := MediaIdentity{Schema: "rtsp", Vhost: "vhost-a", App: "camera", Stream: "stream-1"}
	proxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return redirect.URL, nil })
	_, err := proxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity)
	if !errors.Is(err, ErrSnapshotInvalidResponse) || targetHits != 0 {
		t.Fatalf("redirect was followed: err=%v targetHits=%d", err, targetHits)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer slow.Close()
	timeoutProxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return slow.URL, nil }, WithSnapshotTimeout(10*time.Millisecond))
	_, err = timeoutProxy.Fetch(context.Background(), &node.Node{MediaServerUUID: "node-a"}, identity)
	if !errors.Is(err, ErrSnapshotTimeout) {
		t.Fatalf("timeout error=%v", err)
	}

	valid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
	}))
	defer valid.Close()
	marked := false
	markerProxy := NewJPEGProxy(func(context.Context, *node.Node, MediaIdentity) (string, error) { return valid.URL, nil }, WithSnapshotMarker(func(metadata map[string]any) {
		marked = true
		if _, exists := metadata["body"]; exists {
			t.Fatal("snapshot body entered audit metadata")
		}
	}))
	writer := &markerWriter{onWrite: func() {
		if !marked {
			t.Fatal("sensitive operation must be marked before response write")
		}
	}}
	if err := markerProxy.Proxy(context.Background(), writer, &node.Node{MediaServerUUID: "node-a"}, identity); err != nil {
		t.Fatal(err)
	}
	if writer.status != http.StatusOK || writer.header.Get("Cache-Control") != "no-store" || writer.header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("response=%+v", writer)
	}
}

type markerWriter struct {
	header  http.Header
	status  int
	body    []byte
	onWrite func()
}

func (w *markerWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *markerWriter) WriteHeader(status int) { w.status = status }

func (w *markerWriter) Write(body []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite()
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, body...)
	return len(body), nil
}

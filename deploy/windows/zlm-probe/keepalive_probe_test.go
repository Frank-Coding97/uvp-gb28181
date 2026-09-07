package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetServerKeepaliveUsesAuthenticatedAPIWithoutReturningCapabilityURL(t *testing.T) {
	var received map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/index/api/setServerConfig" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		received = request.URL.Query()
		_, _ = writer.Write([]byte(`{"code":-1,"msg":"rejected"}`))
	}))
	defer server.Close()

	capabilityURL := "http://127.0.0.1:19000/index/hook/on_server_keepalive?node=test&cap=must-not-escape"
	client := &apiClient{baseURL: server.URL, http: server.Client()}
	err := setServerKeepalive(client, "probe-secret", capabilityURL)
	if err == nil {
		t.Fatal("setServerKeepalive accepted a non-zero API response")
	}
	if strings.Contains(err.Error(), "must-not-escape") || strings.Contains(err.Error(), "cap=") {
		t.Fatal("setServerKeepalive leaked capability URL in error")
	}
	if received["secret"][0] != "probe-secret" {
		t.Fatal("authenticated secret was not sent")
	}
	if received["hook.on_server_keepalive"][0] != capabilityURL {
		t.Fatal("keepalive Hook URL was not sent intact")
	}
	if received["hook.alive_interval"][0] != keepaliveProbeInterval {
		t.Fatal("unexpected keepalive interval")
	}
}

func TestReadServerKeepaliveValueReadsOnlyConfigurationValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/index/api/getServerConfig" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":[{"hook.on_server_keepalive":"http://127.0.0.1/index/hook/on_server_keepalive?node=n&cap=secret"}]}`))
	}))
	defer server.Close()

	value, err := readServerKeepaliveValue(&apiClient{baseURL: server.URL, http: server.Client()}, "probe-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value, "on_server_keepalive") {
		t.Fatal("unexpected keepalive value")
	}
}

func TestWaitForKeepaliveQuietRejectsUnexpectedCallback(t *testing.T) {
	receiver := &hookReceiver{}
	if err := waitForKeepaliveQuiet(receiver, 0, 75*time.Millisecond); err != nil {
		t.Fatalf("quiet receiver was rejected: %v", err)
	}

	receiver.mu.Lock()
	receiver.observations = append(receiver.observations, hookObservation{event: hookOnServerKeepalive})
	receiver.mu.Unlock()
	if err := waitForKeepaliveQuiet(receiver, 0, time.Second); err == nil {
		t.Fatal("unexpected keepalive callback was accepted")
	}
}

func TestProbeLogsContainCapabilityURLReturnsOnlyRedactionResult(t *testing.T) {
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "probe-stdout.log"), []byte("GET_CONFIG hook.on_server_keepalive=http://127.0.0.1/index/hook/on_server_keepalive?node=n&cap=hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	leaked, err := probeLogsContainCapabilityURL(stage)
	if err != nil || !leaked {
		t.Fatalf("capability URL was not detected: leaked=%v err=%v", leaked, err)
	}

	if err := os.WriteFile(filepath.Join(stage, "probe-stdout.log"), []byte("server started without credentials"), 0o600); err != nil {
		t.Fatal(err)
	}
	if leaked, err := probeLogsContainCapabilityURL(stage); err != nil || leaked {
		t.Fatalf("ordinary process log was treated as a leak: leaked=%v err=%v", leaked, err)
	}
}

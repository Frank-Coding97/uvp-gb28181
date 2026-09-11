package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if received["hook.enable"][0] != "1" {
		t.Fatal("enable state was not sent for the hot-enable request")
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

func TestKeepaliveAuditProxyRecordsGenerationAndCapabilityValidity(t *testing.T) {
	target, err := newHookReceiver("test-node", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer target.close(time.Second)
	proxy, err := newKeepaliveAuditProxy(target)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.close(time.Second)

	body := strings.NewReader(`{"mediaServerId":"test-node","hook_index":1,"data":{}}`)
	request, err := http.NewRequest(http.MethodPost, proxy.hookURL("a"), body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("keepalive audit proxy did not forward the authenticated Hook")
	}

	count, recorded, ok := proxy.generationSnapshot("a")
	if count != 1 || !ok || !recorded.capabilityValid {
		t.Fatal("keepalive audit proxy did not record a valid capability request")
	}
	if target.count(hookOnServerKeepalive) != 1 {
		t.Fatal("keepalive audit proxy did not forward the valid request exactly once")
	}

	badURL, err := url.Parse(proxy.hookURL("bad"))
	if err != nil {
		t.Fatal(err)
	}
	badQuery := badURL.Query()
	badQuery.Set("cap", "invalid")
	badURL.RawQuery = badQuery.Encode()
	badRequest, err := http.NewRequest(http.MethodPost, badURL.String(), strings.NewReader(`{"mediaServerId":"test-node","hook_index":2,"data":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	badRequest.Header.Set("Content-Type", "application/json")
	badResponse, err := http.DefaultClient.Do(badRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, badResponse.Body)
	_ = badResponse.Body.Close()
	_, invalid, ok := proxy.generationSnapshot("bad")
	if !ok || invalid.capabilityValid || target.count(hookOnServerKeepalive) != 1 {
		t.Fatal("invalid capability was accepted or not preserved through the audit proxy")
	}
}

func TestKeepaliveAuditProxyAllowsInFlightRequestBeforeBoundary(t *testing.T) {
	proxy := &keepaliveAuditProxy{}
	boundary := time.Now()
	proxy.record(keepaliveAuditRequest{generation: "a", arrivedAt: boundary.Add(-time.Millisecond)})
	if err := proxy.waitForNoRequestsAfter(boundary, 75*time.Millisecond); err != nil {
		t.Fatalf("in-flight request before transition was rejected: %v", err)
	}

	proxy.record(keepaliveAuditRequest{generation: "a", arrivedAt: time.Now().Add(time.Millisecond)})
	if err := proxy.waitForNoRequestsAfter(boundary, 75*time.Millisecond); err == nil {
		t.Fatal("request arriving after transition was accepted")
	}
}

func TestProbeLogsContainCapabilityURLReturnsOnlyRedactionResult(t *testing.T) {
	stage := t.TempDir()
	secret := "runtime-secret-value"
	capability := "runtime-capability-value"
	if err := os.WriteFile(filepath.Join(stage, "probe-stdout.log"), []byte("GET_CONFIG secret="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	leaked, err := probeLogsContainCapabilityURL(stage, secret)
	if err != nil || !leaked {
		t.Fatalf("runtime secret was not detected: leaked=%v err=%v", leaked, err)
	}
	if err := os.WriteFile(filepath.Join(stage, "probe-stdout.log"), []byte("GET_CONFIG capability="+capability), 0o600); err != nil {
		t.Fatal(err)
	}
	if leaked, err := probeLogsContainCapabilityURL(stage, capability); err != nil || !leaked {
		t.Fatalf("runtime capability was not detected: leaked=%v err=%v", leaked, err)
	}

	if err := os.WriteFile(filepath.Join(stage, "probe-stdout.log"), []byte("server started without credentials"), 0o600); err != nil {
		t.Fatal(err)
	}
	if leaked, err := probeLogsContainCapabilityURL(stage); err != nil || leaked {
		t.Fatalf("ordinary process log was treated as a leak: leaked=%v err=%v", leaked, err)
	}
}

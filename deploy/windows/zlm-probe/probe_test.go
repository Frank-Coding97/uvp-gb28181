package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteINIUpdatesSectionSpecificKeysAndAppendsMissingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	input := "[http]\nport=80\n\n[api]\napiDebug=1\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteINI(path, map[string]map[string]string{
		"http": {"port": "18080", "sslport": "0"},
		"api":  {"apiDebug": "0", "secret": "generated"},
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{"port=18080", "sslport=0", "apiDebug=0", "secret=generated"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rewritten config lacks %q:\n%s", expected, text)
		}
	}
}

func TestCopyRuntimeTreePreservesChineseSpacePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "输入 中文 space")
	if err := os.MkdirAll(filepath.Join(root, "www"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.ini"), []byte("[api]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "www", "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MediaServer.exe"), []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	stage, stageExe, stats, err := copyRuntimeTree(root, "MediaServer.exe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if !hasChineseAndSpace(stage) || stageExe == "" || stats.Files != 3 {
		t.Fatalf("stage=%q executable=%q stats=%+v", stage, stageExe, stats)
	}
	if _, err := os.Stat(filepath.Join(stage, "www", "index.html")); err != nil {
		t.Fatal(err)
	}
}

func TestCopyRuntimeTreeRejectsSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "输入 中文 space")
	if err := os.MkdirAll(filepath.Join(root, "www"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.ini"), []byte("[api]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "www", "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "MediaServer.exe"), []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "www", "index.html"), filepath.Join(root, "linked.html")); err != nil {
		t.Skipf("creating a symlink is unavailable: %v", err)
	}
	if _, _, _, err := copyRuntimeTree(root, "MediaServer.exe"); err == nil {
		t.Fatal("copyRuntimeTree accepted a symlink")
	}
}

func TestRewriteINIPreservesCRLFOnReplacedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, []byte("[http]\r\nport=80\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteINI(path, map[string]map[string]string{"http": {"port": "18080"}}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "port=18080\r\n") {
		t.Fatalf("replacement did not preserve CRLF: %q", content)
	}
}

func TestAPIClientDoesNotTreatHTTP200AuthErrorAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("secret") == "bad" {
			_, _ = writer.Write([]byte(`{"code":-100,"msg":"auth"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":["/index/api/getApiList"]}`))
	}))
	defer server.Close()
	client := &apiClient{baseURL: server.URL, http: server.Client()}
	bad, err := client.callSecret(context.Background(), "/index/api/getApiList", "bad")
	if err != nil || bad.Code != -100 {
		t.Fatalf("auth response=%+v err=%v", bad, err)
	}
	good, err := client.callSecret(context.Background(), "/index/api/getApiList", "good")
	if err != nil || good.Code != 0 {
		t.Fatalf("success response=%+v err=%v", good, err)
	}
}

func TestCloseMediaStreamReturnsErrorForNonZeroAPICode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/index/api/close_streams" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"code":-1,"msg":"close failed"}`))
	}))
	defer server.Close()

	closed, err := closeMediaStream(&apiClient{baseURL: server.URL, http: server.Client()}, "secret", "vhost", "app", "stream")
	if err == nil || closed {
		t.Fatalf("non-zero API code was treated as a clean close: closed=%v err=%v", closed, err)
	}
}

func TestWaitMediaOfflineRequiresExplicitFalseOnline(t *testing.T) {
	responses := []string{`{"code":0,"online":true}`, `{"code":0,"online":false}`}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/index/api/isMediaOnline" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.URL.Query().Get("schema") != "fmp4" {
			t.Fatalf("offline query omitted fmp4 schema: %v", request.URL.Query())
		}
		response := responses[len(responses)-1]
		if requests < len(responses) {
			response = responses[requests]
		}
		requests++
		_, _ = writer.Write([]byte(response))
	}))
	defer server.Close()

	err := waitMediaOffline(&apiClient{baseURL: server.URL, http: server.Client()}, "secret", "vhost", "app", "stream")
	if err != nil {
		t.Fatalf("explicit offline response was not accepted: %v", err)
	}
	if requests < 2 {
		t.Fatalf("online=true was incorrectly accepted as offline: requests=%d", requests)
	}
}

func TestProbeReportNeverSerializesSecretFields(t *testing.T) {
	var response apiResponse
	if err := json.Unmarshal([]byte(`{"code":0,"secret":"must-not-appear","data":[]}`), &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "must-not-appear") {
		t.Fatal("API response serialization leaked an unmodeled secret field")
	}
}

func TestCommitMatchesFullAndShortVersionForms(t *testing.T) {
	if !commitMatches("b422fb016da1c3989b725dde3bd0292613b12060", "b422fb0") {
		t.Fatal("full expected commit did not match the short ZLM version commit")
	}
	if !commitMatches("b422fb0", "b422fb016da1c3989b725dde3bd0292613b12060") {
		t.Fatal("short expected commit did not match the full ZLM version commit")
	}
	if commitMatches("b422fb0", "296dceb") {
		t.Fatal("different ZLM commits were treated as equal")
	}
}

func TestRTPLifecycleAcceptsOmittedDataOnlyForEmptyList(t *testing.T) {
	var stream string
	closed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/openRtpServer":
			stream = r.URL.Query().Get("stream_id")
			w.Write([]byte(`{"code":0,"port":12345}`))
		case "/index/api/closeRtpServer":
			closed = true
			w.Write([]byte(`{"code":0,"hit":1}`))
		case "/index/api/listRtpServer":
			if closed {
				w.Write([]byte(`{"code":0}`))
			} else {
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"vhost": "__defaultVhost__", "app": "rtp", "stream_id": stream, "port": 12345}}})
			}
		}
	}))
	defer server.Close()
	client := &apiClient{baseURL: server.URL, http: server.Client()}
	result := checkRTPLifecycle(client, "test-secret")
	if result.Status != "passed" {
		t.Fatalf("empty list contract: %+v", result)
	}
}

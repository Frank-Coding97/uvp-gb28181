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

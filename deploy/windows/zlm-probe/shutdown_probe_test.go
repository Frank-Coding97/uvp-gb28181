package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShutdownHookGateBlocksBeforeIndexAndReleasesBeforeSuccess(t *testing.T) {
	receiver, err := newHookReceiver("shutdown-node", "shutdown-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(5 * time.Second)
	marker := filepath.Join(t.TempDir(), "index.marker")
	gate, err := newShutdownHookGate(receiver, shutdownHookBlockThenSuccess, marker)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.close()

	body := shutdownProbeRecordBody(t)
	result := make(chan hookCallResult, 1)
	go func() {
		status, response, callErr := postShutdownHook(gate.url(), body)
		result <- hookCallResult{status: status, response: response, err: callErr}
	}()
	if err := gate.waitForAttempt(1, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("index marker appeared before hook release: %v", err)
	}
	gate.release()
	call := <-result
	if call.err != nil || call.status != 200 || call.response.Code != 0 {
		t.Fatalf("released hook result: status=%d response=%+v err=%v", call.status, call.response, call.err)
	}
	assertShutdownProbeMarker(t, marker, 1)
}

func TestShutdownHookGateRetriesAfterHTTP500(t *testing.T) {
	receiver, err := newHookReceiver("shutdown-node", "shutdown-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(5 * time.Second)
	marker := filepath.Join(t.TempDir(), "index.marker")
	gate, err := newShutdownHookGate(receiver, shutdownHookRetryThenSuccess, marker)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.close()

	body := shutdownProbeRecordBody(t)
	first := make(chan hookCallResult, 1)
	go func() {
		status, response, callErr := postShutdownHook(gate.url(), body)
		first <- hookCallResult{status: status, response: response, err: callErr}
	}()
	if err := gate.waitForAttempt(1, time.Second); err != nil {
		t.Fatal(err)
	}
	call := <-first
	if call.err != nil || call.status != 500 || call.response.Code != -1 {
		t.Fatalf("first retry result: status=%d response=%+v err=%v", call.status, call.response, call.err)
	}

	second := make(chan hookCallResult, 1)
	go func() {
		status, response, callErr := postShutdownHook(gate.url(), body)
		second <- hookCallResult{status: status, response: response, err: callErr}
	}()
	if err := gate.waitForAttempt(2, time.Second); err != nil {
		t.Fatal(err)
	}
	gate.release()
	call = <-second
	if call.err != nil || call.status != 200 || call.response.Code != 0 {
		t.Fatalf("second retry result: status=%d response=%+v err=%v", call.status, call.response, call.err)
	}
	if got := gate.attempts(); got != 2 {
		t.Fatalf("hook attempts = %d, want 2", got)
	}
	assertShutdownProbeMarker(t, marker, 1)
}

func TestShutdownHookGateFailureDoesNotIndex(t *testing.T) {
	receiver, err := newHookReceiver("shutdown-node", "shutdown-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(5 * time.Second)
	marker := filepath.Join(t.TempDir(), "index.marker")
	gate, err := newShutdownHookGate(receiver, shutdownHookAlwaysFailure, marker)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.close()

	status, response, callErr := postShutdownHook(gate.url(), shutdownProbeRecordBody(t))
	if callErr != nil || status != 500 || response.Code != -1 {
		t.Fatalf("failure hook result: status=%d response=%+v err=%v", status, response, callErr)
	}
	if got := gate.attempts(); got != 1 {
		t.Fatalf("hook attempts = %d, want 1", got)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failure hook wrote index marker: %v", err)
	}
}

func TestShutdownHookGateCleanupReleasesBlockedRequest(t *testing.T) {
	receiver, err := newHookReceiver("shutdown-node", "shutdown-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.close(5 * time.Second)
	gate, err := newShutdownHookGate(receiver, shutdownHookBlockThenSuccess, filepath.Join(t.TempDir(), "index.marker"))
	if err != nil {
		t.Fatal(err)
	}
	defer gate.close()

	body := shutdownProbeRecordBody(t)
	result := make(chan error, 1)
	go func() {
		_, _, callErr := postShutdownHook(gate.url(), body)
		result <- callErr
	}()
	if err := gate.waitForAttempt(1, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := gate.close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("blocked Hook request survived gate cleanup")
	}
}

func TestWaitForMP4SizeWaitsPastSmallHeader(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "record.mp4")
	if err := os.WriteFile(path, make([]byte, 44), 0o600); err != nil {
		t.Fatal(err)
	}
	files, size := waitForMP4Size(root, shutdownRecordingDataMinBytes, 50*time.Millisecond)
	if files != 1 || size != 44 {
		t.Fatalf("small MP4 header inventory = files %d, bytes %d; want 1, 44", files, size)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, shutdownRecordingDataMinBytes-44)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	files, size = waitForMP4Size(root, shutdownRecordingDataMinBytes, time.Second)
	if files != 1 || size < shutdownRecordingDataMinBytes {
		t.Fatalf("grown MP4 inventory = files %d, bytes %d; want 1 and at least %d", files, size, shutdownRecordingDataMinBytes)
	}
}

func shutdownProbeRecordBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"mediaServerId": "shutdown-node",
		"hook_index":    1,
		"vhost":         "__defaultVhost__",
		"app":           "live",
		"stream":        "shutdown-stream",
		"start_time":    1,
		"file_path":     filepath.Join(t.TempDir(), "2026-01-01", "record.mp4"),
		"file_name":     "record.mp4",
		"file_size":     4096,
		"time_len":      1.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func assertShutdownProbeMarker(t *testing.T, path string, lines int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "indexed\n"); got != lines {
		t.Fatalf("index marker lines = %d, want %d: %q", got, lines, data)
	}
}

type hookCallResult struct {
	status   int
	response hookResponse
	err      error
}

func postShutdownHook(endpoint string, body []byte) (int, hookResponse, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, hookResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-VHOST", "__defaultVhost__")
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return 0, hookResponse{}, err
	}
	defer response.Body.Close()
	var decoded hookResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, hookResponseWriteLimit)).Decode(&decoded); err != nil {
		return response.StatusCode, hookResponse{}, err
	}
	return response.StatusCode, decoded, nil
}

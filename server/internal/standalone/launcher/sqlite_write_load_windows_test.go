//go:build windows

package launcher

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// This exercises the real backend under HTTP login write load. WroteRequest
// proves transport submission only, not execution inside a SQLite transaction.
// Three acknowledged sessions must survive; the interrupted request may commit
// or roll back. No test-process database writes or product pause hooks are used.
func t27SQLiteWriteLoadKill(t *testing.T, paths standalone.Paths, run *t18Launch, client t18HTTPClient, loginBody []byte) {
	release, err := standalone.LoadRelease(paths.InstallDir)
	require.NoError(t, err)
	handle := t27BackendHandle(t, release.BackendExe)
	defer windows.CloseHandle(handle)
	configBefore, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	acknowledged := make(chan []string, 1)
	submitted := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		var sessions []string
		for requestNumber := 0; ctx.Err() == nil; requestNumber++ {
			requestCtx := ctx
			if requestNumber == 3 {
				requestCtx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
					WroteRequest: func(info httptrace.WroteRequestInfo) {
						if info.Err == nil {
							close(submitted)
						}
					},
				})
			}
			req, requestErr := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(client.baseURL, "/")+"/api/login", bytes.NewReader(loginBody))
			if requestErr != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", client.origin)
			response, requestErr := client.client.Do(req)
			if requestErr != nil {
				return
			}
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusOK {
				return
			}
			var envelope struct {
				Code int `json:"code"`
				Data struct {
					AccessToken string `json:"accessToken"`
				} `json:"data"`
			}
			if json.Unmarshal(raw, &envelope) != nil || envelope.Code != 0 {
				return
			}
			parts := strings.Split(envelope.Data.AccessToken, ".")
			if len(parts) != 3 {
				return
			}
			claimsRaw, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
			var claims struct {
				SID string `json:"sid"`
			}
			if decodeErr != nil || json.Unmarshal(claimsRaw, &claims) != nil || claims.SID == "" {
				return
			}
			if requestNumber < 3 {
				sessions = append(sessions, claims.SID)
			}
			if requestNumber == 2 {
				acknowledged <- append([]string(nil), sessions...)
			}
		}
	}()
	defer func() { cancel(); <-finished }()
	var sessions []string
	select {
	case sessions = <-acknowledged:
	case <-finished:
		t.Fatal("login write load ended before three acknowledged commits")
	case <-ctx.Done():
		t.Fatal("login write load did not acknowledge three commits")
	}
	select {
	case <-submitted:
	case <-finished:
		t.Fatal("login write load ended before the next request was submitted")
	case <-ctx.Done():
		t.Fatal("next login write request was not submitted")
	}
	require.NoError(t, windows.TerminateProcess(handle, 137))
	state, err := windows.WaitForSingleObject(handle, 10000)
	require.NoError(t, err)
	require.EqualValues(t, windows.WAIT_OBJECT_0, state)
	cancel()
	select {
	case <-run.finished:
	case <-time.After(90 * time.Second):
		t.Fatal("launcher did not finish owned crash cleanup")
	}
	select {
	case launchErr := <-run.done:
		require.Error(t, launchErr)
	default:
		t.Fatal("crashed backend launch did not return its result")
	}
	configAfter, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("crash changed installation configuration")
	}
	marker := t27ObserveRunMarker(t, paths)
	closedCtx, closedCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = LaunchWithBrowser(closedCtx, paths.InstallDir, paths.RecordingsDir, func(status Status) {
		if status.State == Ready {
			t.Error("abnormal restart reached Ready")
		}
	}, func(string) { t.Error("abnormal restart opened browser") })
	closedCancel()
	require.ErrorIs(t, err, standalone.ErrUncleanRecoveryRequired)
	afterMarker := t27ObserveRunMarker(t, paths)
	require.Equal(t, marker.SHA256, afterMarker.SHA256)

	dbURL := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(paths.DatabasePath), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dbURL.String())
	require.NoError(t, err)
	defer db.Close()
	var integrity string
	require.NoError(t, db.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	require.Equal(t, "ok", integrity)
	for _, sid := range sessions {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM sys_user_sessions WHERE sid = ? AND user_id > 0 AND refresh_token_hash IS NOT NULL", sid).Scan(&count))
		require.Equal(t, 1, count, "acknowledged login session was not retained")
	}
}

func t27ObserveRunMarker(t *testing.T, paths standalone.Paths) standalone.RunMarkerObservation {
	t.Helper()
	lock, err := standalone.AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()
	marker, err := standalone.InspectRunMarker(paths)
	require.ErrorIs(t, err, standalone.ErrUncleanRecoveryRequired)
	return marker
}

func t27BackendHandle(t *testing.T, backendPath string) windows.Handle {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	require.NoError(t, err)
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var found windows.Handle
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ParentProcessID != uint32(os.Getpid()) || !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(backendPath)) {
			continue
		}
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		require.NoError(t, openErr)
		name := make([]uint16, 32768)
		size := uint32(len(name))
		queryErr := windows.QueryFullProcessImageName(handle, 0, &name[0], &size)
		if queryErr != nil || !strings.EqualFold(filepath.Clean(windows.UTF16ToString(name[:size])), filepath.Clean(backendPath)) {
			windows.CloseHandle(handle)
			t.Fatal("backend image identity could not be confirmed")
		}
		if found != 0 {
			windows.CloseHandle(handle)
			windows.CloseHandle(found)
			t.Fatal("multiple owned backend processes")
		}
		found = handle
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		if found != 0 {
			windows.CloseHandle(found)
		}
		t.Fatal("backend process enumeration failed")
	}
	if found == 0 {
		t.Fatal("owned backend process was not found")
	}
	return found
}

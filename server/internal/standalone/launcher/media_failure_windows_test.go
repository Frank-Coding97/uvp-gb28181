//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// This opt-in test only mutates a disposable, already initialized t18 fixture.
// The temporary media API secret is restored before owned processes are stopped.
func TestWindowsStandaloneMediaSecretFailureRecovery(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("UVP_T19_FAILURE_INSTALL_DIR"))
	if root == "" {
		t.Skip("requires a completed disposable t18 fixture")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil || release.Version != "t18-setup" {
		t.Fatal("invalid isolated release")
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir})
	if err != nil {
		t.Fatal("invalid isolated paths")
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		t.Fatal("isolated configuration unavailable")
	}
	entries := make(chan string, 1)
	run := t18Start(t, root, entries)
	defer func() {
		run.cancel()
		if !t18WaitFinished(t, run, 90*time.Second) {
			t.Error("owned fixture did not stop cleanly")
		}
	}()
	if _, ok := t18WaitBrowserEntry(run, entries, 90*time.Second); !ok {
		t.Fatal("owned fixture did not start")
	}
	t19WaitBusinessReady(t, run)
	original, temporary := config.ZLMSecret(), t18RandomPassword(t)
	endpoint := "http://" + config.MediaAddress()
	if err := t19MediaConfigRequest(endpoint, original, url.Values{"api.secret": {temporary}}, nil); err != nil {
		t.Fatal("temporary media secret rotation failed")
	}
	restored := false
	defer func() {
		if !restored {
			if err := t19MediaConfigRequest(endpoint, temporary, url.Values{"api.secret": {original}}, nil); err != nil {
				t.Error("media secret restoration failed")
			}
		}
	}()
	var actual []map[string]string
	if err := t19MediaConfigRequest(endpoint, temporary, nil, &actual); err != nil || len(actual) != 1 || actual[0]["api.secret"] != temporary {
		t.Fatal("temporary media secret readback failed")
	}
	if err := t19MediaConfigRequest(endpoint, original, nil, &actual); err == nil {
		t.Fatal("old media secret remained accepted")
	}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	observed := false
	for !observed {
		select {
		case state := <-run.statuses:
			observed = state.State == Ready && !state.BusinessReady && state.BusinessReason == "media_unreachable"
		case <-run.finished:
			t.Fatal("owned launcher exited during media fault")
		case <-timer.C:
			t.Fatal("signed readiness did not report media secret failure")
		}
	}
	t.Log("real media secret rejection produced business_ready=false, reason=media_unreachable")
	if err := t19MediaConfigRequest(endpoint, temporary, url.Values{"api.secret": {original}}, nil); err != nil {
		t.Fatal("media secret restoration failed")
	}
	restored = true
	t19WaitBusinessReady(t, run)
	t.Log("restored secret and a new authenticated Hook recovered business readiness")
}

// All credential-bearing request and response data stays in memory. Errors are
// fixed messages and never reflect URL, form values or media response bodies.
func t19MediaConfigRequest(endpoint, secret string, changes url.Values, target any) error {
	path := "/index/api/getServerConfig"
	values := url.Values{"secret": {secret}}
	if changes != nil {
		path = "/index/api/setServerConfig"
		for key, items := range changes {
			values[key] = items
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, strings.NewReader(values.Encode()))
	if err != nil {
		return errors.New("invalid media request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("media request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errors.New("media response unavailable")
	}
	var result struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(data, &result) != nil || result.Code == nil || *result.Code != 0 {
		return errors.New("media request rejected")
	}
	if target != nil && json.Unmarshal(result.Data, target) != nil {
		return errors.New("media config response invalid")
	}
	return nil
}

package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func newNoticeFixture(t *testing.T) (*ServiceUpgradeController, *recordingUpgradeRPC, string) {
	t.Helper()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "helper-state")
	t.Setenv("UVP_UPDATER_STATE", stateRoot)
	versionPath := filepath.Join(root, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"name":"uvp-gb28181","version":"1.1.0"}`), 0600))
	fixture := &recordingUpgradeRPC{}
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath, RPC: fixture})
	require.False(t, controller.local)
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	return controller, fixture, stateRoot
}

func noticePayload(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code, recorder.Body.String())
	return envelope.Data
}

func callNotice(t *testing.T, controller *ServiceUpgradeController, method, path string, body []byte) (recorder *httptest.ResponseRecorder) {
	t.Helper()
	recorder = httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	if body == nil {
		ctx.Request = httptest.NewRequest(method, path, nil)
	} else {
		ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	// FailAndAbort panics to unwind the handler; in production a middleware
	// recovers it, so the test harness has to do the same.
	defer func() {
		if recovered := recover(); recovered != nil && recovered != consts.RequestAborted {
			panic(recovered)
		}
	}()
	switch {
	case strings.HasSuffix(path, "/notice/auto-check"):
		controller.SetAutoCheck(ctx)
	case method == http.MethodGet:
		controller.Notice(ctx)
	case method == http.MethodPost:
		controller.IgnoreNotice(ctx)
	default:
		controller.ClearIgnoredNotice(ctx)
	}
	return recorder
}

func TestUpgradeNoticeReportsBackgroundCheckResult(t *testing.T) {
	controller, fixture, _ := newNoticeFixture(t)
	checkedAt := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC)
	fixture.onlineCheck = true
	fixture.watch = &upgrade.UpdateWatchState{
		CheckedAt: checkedAt, CurrentVersion: "1.1.0", LatestVersion: "1.2.0", LatestBuildID: "build-2",
		PublishedAt: "2026-10-28T00:00:00Z", ReleaseNotes: "修复若干问题", HasUpdate: true,
	}

	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))

	require.Equal(t, true, payload["checkEnabled"])
	require.Equal(t, true, payload["hasUpdate"])
	require.Equal(t, "1.2.0", payload["latestVersion"])
	require.Equal(t, "1.1.0", payload["currentVersion"])
	require.Equal(t, "2026-10-29T02:00:00Z", payload["checkedAt"])
}

// A helper that is down must hide the badge instead of surfacing an error to
// every page load.
func TestUpgradeNoticeStaysQuietWhenHelperIsUnavailable(t *testing.T) {
	controller, fixture, _ := newNoticeFixture(t)
	fixture.statusErr = os.ErrDeadlineExceeded

	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))

	require.Equal(t, false, payload["checkEnabled"])
	require.Equal(t, false, payload["hasUpdate"])
}

func TestUpgradeNoticeHidesIgnoredRelease(t *testing.T) {
	controller, fixture, stateRoot := newNoticeFixture(t)
	fixture.onlineCheck = true
	fixture.watch = &upgrade.UpdateWatchState{CheckedAt: time.Now(), CurrentVersion: "1.1.0", LatestVersion: "1.2.0", HasUpdate: true}

	recorder := callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/ignore", []byte(`{"version":"1.2.0"}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.FileExists(t, filepath.Join(stateRoot, ignoredUpdateFilename))

	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))
	require.Equal(t, true, payload["ignored"])
	require.Equal(t, true, payload["hasUpdate"])

	recorder = callNotice(t, controller, http.MethodDelete, "/api/system/upgrade/notice/ignore", nil)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoFileExists(t, filepath.Join(stateRoot, ignoredUpdateFilename))

	payload = noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))
	require.Equal(t, false, payload["ignored"])
}

// Ignoring a version the helper is not offering would hide a later release,
// so the handler must reject it instead of storing a stale preference.
func TestIgnoreNoticeRejectsVersionNotOffered(t *testing.T) {
	controller, fixture, stateRoot := newNoticeFixture(t)
	fixture.onlineCheck = true
	fixture.watch = &upgrade.UpdateWatchState{CheckedAt: time.Now(), LatestVersion: "1.2.0", HasUpdate: true}

	recorder := callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/ignore", []byte(`{"version":"9.9.9"}`))

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.NoFileExists(t, filepath.Join(stateRoot, ignoredUpdateFilename))
}

func TestIgnoreNoticeRejectsUnsafeVersionText(t *testing.T) {
	controller, fixture, stateRoot := newNoticeFixture(t)
	fixture.onlineCheck = true
	fixture.watch = &upgrade.UpdateWatchState{CheckedAt: time.Now(), LatestVersion: "1.2.0", HasUpdate: true}

	recorder := callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/ignore", []byte(`{"version":"../../etc/passwd"}`))

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.NoFileExists(t, filepath.Join(stateRoot, ignoredUpdateFilename))
}

// Once the running version catches up the notice must stop advertising the
// release, and a stale "ignored" marker must not suppress the next one.
func TestUpgradeNoticeClearsIgnoredMarkerForNewerRelease(t *testing.T) {
	controller, fixture, _ := newNoticeFixture(t)
	fixture.onlineCheck = true
	fixture.watch = &upgrade.UpdateWatchState{CheckedAt: time.Now(), LatestVersion: "1.2.0", HasUpdate: true}
	require.Equal(t, http.StatusOK, callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/ignore", []byte(`{"version":"1.2.0"}`)).Code)

	fixture.watch = &upgrade.UpdateWatchState{CheckedAt: time.Now(), LatestVersion: "1.3.0", HasUpdate: true}
	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))

	require.Equal(t, true, payload["hasUpdate"])
	require.Equal(t, false, payload["ignored"])
}

// The schedule switch is off by default only when someone turned it off, so a
// fresh installation must report "enabled".
func TestUpgradeNoticeDefaultsAutoCheckToEnabled(t *testing.T) {
	controller, fixture, stateRoot := newNoticeFixture(t)
	fixture.onlineCheck = true

	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))

	require.Equal(t, true, payload["autoCheck"])
	require.NoFileExists(t, filepath.Join(stateRoot, upgrade.AutoCheckPreferenceFilename))
}

// Turning the schedule off must be durable: the helper reads this file before
// every probe, so the page must be able to write it and read it back.
func TestSetAutoCheckPersistsPreference(t *testing.T) {
	controller, fixture, stateRoot := newNoticeFixture(t)
	fixture.onlineCheck = true

	recorder := callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/auto-check", []byte(`{"enabled":false}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.FileExists(t, filepath.Join(stateRoot, upgrade.AutoCheckPreferenceFilename))
	require.False(t, upgrade.ReadAutoCheckPreference(filepath.Join(stateRoot, upgrade.AutoCheckPreferenceFilename)))

	payload := noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))
	require.Equal(t, false, payload["autoCheck"])

	recorder = callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/auto-check", []byte(`{"enabled":true}`))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	payload = noticePayload(t, callNotice(t, controller, http.MethodGet, "/api/system/upgrade/notice", nil))
	require.Equal(t, true, payload["autoCheck"])
}

// "enabled" is the whole request. A body without it must not silently fall back
// to false and stop the checks on a typo.
func TestSetAutoCheckRejectsMissingValue(t *testing.T) {
	controller, _, stateRoot := newNoticeFixture(t)

	recorder := callNotice(t, controller, http.MethodPost, "/api/system/upgrade/notice/auto-check", []byte(`{}`))

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.NoFileExists(t, filepath.Join(stateRoot, upgrade.AutoCheckPreferenceFilename))
}

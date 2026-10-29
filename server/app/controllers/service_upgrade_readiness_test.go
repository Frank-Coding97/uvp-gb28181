package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestUpgradeReadinessRejectsExternalPeerEvenWithForwardedLoopback(t *testing.T) {
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(t.TempDir(), "version.json")})
	result := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(result)
	ctx.Request = httptest.NewRequest(http.MethodGet, upgrade.RuntimeIdentityPath, nil)
	ctx.Request.RemoteAddr = "198.51.100.4:4412"
	ctx.Request.Header.Set("X-Forwarded-For", "127.0.0.1")
	controller.UpgradeReadiness(ctx)
	require.Equal(t, http.StatusForbidden, result.Code)
	require.Empty(t, result.Body.String())
}

func TestUpgradeReadinessReturnsLiveBuildAndDatabaseState(t *testing.T) {
	root := t.TempDir()
	setupUpgradePreflightDatabase(t, root)
	previousBuildID, previousVersion := app.BuildID, app.AppVersion
	app.BuildID = "embedded-live-build"
	app.AppVersion = app.VersionInfo{Version: "1.2.0", BuildID: app.BuildID}
	t.Cleanup(func() { app.BuildID, app.AppVersion = previousBuildID, previousVersion })
	t.Setenv("UVP_UPDATER_STATE", root)
	gate, err := upgrade.OpenGateStore(filepath.Join(root, "gate.json"))
	require.NoError(t, err)
	taskID := strings.Repeat("a", 32)
	require.NoError(t, gate.Save(upgrade.GateState{Mode: upgrade.GateCandidate, TaskID: taskID, BuildID: "expected-build"}))
	// File contents cannot impersonate the executable's identity.
	versionPath := filepath.Join(root, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"9.9.9","build_id":"forged-file-build"}`), 0600))
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath})
	result := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(result)
	ctx.Request = httptest.NewRequest(http.MethodGet, upgrade.RuntimeIdentityPath, nil)
	ctx.Request.RemoteAddr = "127.0.0.1:4412"
	controller.UpgradeReadiness(ctx)
	version, buildID := app.RuntimeVersionIdentity()
	require.NotEmpty(t, version)
	require.NotEmpty(t, buildID)
	require.Equal(t, http.StatusOK, result.Code)
	var identity upgrade.RuntimeIdentity
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &identity))
	require.Equal(t, version, identity.Version)
	require.Equal(t, buildID, identity.BuildID)
	require.NotEqual(t, "forged-file-build", identity.BuildID)
	require.Equal(t, taskID, identity.TaskID)
	require.True(t, identity.DatabaseReady)
	require.Equal(t, "sqlite", identity.DatabaseDialect)
	require.Len(t, identity.MigrationDigest, 64)
	require.NotContains(t, result.Body.String(), root)
}

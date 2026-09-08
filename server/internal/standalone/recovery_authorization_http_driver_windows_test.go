//go:build windows

package standalone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWindowsRecoveryAuthorizationHTTPDriver runs the launcher-side HTTP
// authorization recovery test against real Windows component images. The
// fixture is disposable and the child receives only its isolated root and
// non-secret case selector; credentials are generated and retained in the
// launcher test process.
func TestWindowsRecoveryAuthorizationHTTPDriver(t *testing.T) {
	launcherTest := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH"))
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)

	fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)
	require.NoError(t, fixture.lock.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, launcherTest,
		"-test.run=^TestWindowsRecoveryAuthorizationHTTP$",
		"-test.v", "-test.timeout=8m")
	cmd.Env = recoveryAuthorizationDriverEnvironment(fixture.paths.InstallDir)
	output, err := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		t.Fatalf("recovery authorization HTTP test timed out: %v", ctxErr)
	}
	// The child deliberately keeps tokens and passwords out of its output. Do
	// not print the complete subprocess stream from this outer driver either.
	if err != nil {
		t.Fatalf("recovery authorization HTTP test failed (output bytes=%d; %s): %v", len(output), recoveryAuthorizationFailureSummary(string(output)), err)
	}
	require.NotContains(t, string(output), "--- SKIP")

	old, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.OldVersion)
	require.NoError(t, err)
	candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
	require.NoError(t, err)
	require.NoError(t, backupComponentsStopped(old, candidate))
	require.NoError(t, CheckMaintenanceGate(fixture.paths.InstallDir))
	selected, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, fixture.journal.OldVersion, selected.Version)
}

func recoveryAuthorizationFailureSummary(output string) string {
	const testFile = "recovery_authorization_http_windows_test.go"
	seen := make(map[string]struct{})
	markers := make([]string, 0, 4)
	add := func(marker string) {
		if _, ok := seen[marker]; ok {
			return
		}
		seen[marker] = struct{}{}
		markers = append(markers, marker)
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Error Trace:") {
			add("Error Trace")
		}
		const httpStatusMarker = "HTTP_STATUS endpoint="
		if index := strings.Index(line, httpStatusMarker); index >= 0 {
			suffix := line[index+len(httpStatusMarker):]
			statusMarker := " status="
			markerEnd := strings.Index(suffix, statusMarker)
			if markerEnd > 0 && recoveryAuthorizationHTTPStatusEndpointAllowed(suffix[:markerEnd]) {
				status := suffix[markerEnd+len(statusMarker):]
				end := 0
				for end < len(status) && status[end] >= '0' && status[end] <= '9' {
					end++
				}
				if end > 0 {
					add("HTTP status=" + status[:end] + " endpoint=" + suffix[:markerEnd])
				}
			}
		}
		if strings.HasPrefix(line, "--- FAIL: TestWindowsRecoveryAuthorizationHTTP") {
			add("FAIL: TestWindowsRecoveryAuthorizationHTTP")
		}
		if index := strings.Index(line, testFile+":"); index >= 0 {
			suffix := line[index+len(testFile)+1:]
			end := 0
			for end < len(suffix) && suffix[end] >= '0' && suffix[end] <= '9' {
				end++
			}
			if end > 0 {
				add(testFile + ":" + suffix[:end])
			}
		}
		if line == "FAIL" || strings.HasPrefix(line, "FAIL ") || strings.HasPrefix(line, "FAIL\t") {
			add("FAIL")
		}
	}
	if len(markers) == 0 {
		return "no safe failure location"
	}
	return strings.Join(markers, ", ")
}

func recoveryAuthorizationHTTPStatusEndpointAllowed(endpoint string) bool {
	switch endpoint {
	case "/api/standalone/setup/status", "/api/standalone/setup/admin",
		"/api/gb28181/sip/setup/config", "/api/gb28181/sip/service-config/fixed-address-playback",
		"/api/users/session/heartbeat", "/api/refreshToken", "/api/gb28181/sip/qr/exchange",
		"/index/hook/on_play", "/api/login", "/api/gb28181/sip/qr/token",
		"/api/gb28181/play/:deviceId/:channelId/authorization":
		return true
	default:
		return false
	}
}

func recoveryAuthorizationDriverEnvironment(root string) []string {
	const (
		rootKey = "UVP_MAINTENANCE_TEST_ROOT"
		caseKey = "UVP_RECOVERY_AUTHORIZATION_CASE"
	)
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if upper == rootKey || upper == caseKey || strings.HasPrefix(upper, "UVP_") ||
			strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "CREDENTIAL") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, rootKey+"="+root, caseKey+"=complete")
}

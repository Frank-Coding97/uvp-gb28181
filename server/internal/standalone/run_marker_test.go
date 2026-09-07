package standalone

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const testRunMarkerName = ".uvp-running.json"

func runMarkerTestPaths(t *testing.T) Paths {
	t.Helper()
	root := filepath.Join(t.TempDir(), "UVP 中文 # spaces")
	dataDir := filepath.Join(root, "data 数据")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, protectConfigDir(dataDir, true))
	paths, err := ResolvePaths(PathOptions{
		InstallDir:    root,
		ConfigDir:     filepath.Join(root, "config"),
		ResourceDir:   filepath.Join(root, "resource"),
		WebDir:        filepath.Join(root, "web"),
		DataDir:       dataDir,
		RecordingsDir: filepath.Join(root, "recordings"),
	})
	require.NoError(t, err)
	return paths
}

func testRunMarkerPath(paths Paths) string {
	return filepath.Join(paths.DataDir, testRunMarkerName)
}

func readTestRunMarker(t *testing.T, paths Paths) map[string]any {
	t.Helper()
	raw, err := readSecureConfigFile(testRunMarkerPath(paths))
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	return payload
}

func TestBeginRunCreatesProtectedMarkerAndReportsCleanFirstRun(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	require.NotNil(t, marker)
	require.False(t, marker.PreviousUnclean)

	payload := readTestRunMarker(t, paths)
	require.Equal(t, float64(1), payload["schema"])
	require.NotEmpty(t, payload["nonce"])
	require.NotEmpty(t, payload["started_at"])
	require.NotContains(t, string(mustReadFile(t, testRunMarkerPath(paths))), "password")
	require.NotContains(t, string(mustReadFile(t, testRunMarkerPath(paths))), "pid")
}

func TestBeginRunReportsPreviousUncleanAndFinishUsesCurrentNonce(t *testing.T) {
	paths := runMarkerTestPaths(t)
	first, err := BeginRun(paths)
	require.NoError(t, err)
	second, err := BeginRun(paths)
	require.NoError(t, err)
	require.True(t, second.PreviousUnclean)

	require.NoError(t, first.Finish())
	_, err = os.Stat(testRunMarkerPath(paths))
	require.NoError(t, err, "an older run must not remove the newer marker")
	require.NoError(t, second.Finish())
	_, err = os.Stat(testRunMarkerPath(paths))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, second.Finish(), "repeated Finish must be safe")
}

func TestFinishRemovesMarkerAfterNormalExit(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	require.NoError(t, marker.Finish())
	require.NoError(t, marker.Finish())
	_, err = os.Stat(testRunMarkerPath(paths))
	require.ErrorIs(t, err, os.ErrNotExist)

	next, err := BeginRun(paths)
	require.NoError(t, err)
	require.False(t, next.PreviousUnclean)
	require.NoError(t, next.Finish())
}

func TestFinishDoesNotRemoveMarkerWithForgedNonce(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	payload := readTestRunMarker(t, paths)
	payload["nonce"] = strings.Repeat("f", 64)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(testRunMarkerPath(paths), raw, true, nil))

	require.NoError(t, marker.Finish())
	_, err = os.Stat(testRunMarkerPath(paths))
	require.NoError(t, err, "Finish must not remove another run marker")
	next, err := BeginRun(paths)
	require.NoError(t, err)
	require.True(t, next.PreviousUnclean)
	require.NoError(t, next.Finish())
}

func TestBeginRunRejectsMalformedMarkerAndPreservesIt(t *testing.T) {
	paths := runMarkerTestPaths(t)
	markerPath := testRunMarkerPath(paths)
	malformed := []byte(`{"schema":1,"nonce":`)
	require.NoError(t, os.WriteFile(markerPath, malformed, 0o600))

	marker, err := BeginRun(paths)
	require.Error(t, err)
	require.Nil(t, marker)
	require.Equal(t, malformed, mustReadFile(t, markerPath))
}

func TestBeginRunRejectsMarkerSymlink(t *testing.T) {
	paths := runMarkerTestPaths(t)
	outside := filepath.Join(t.TempDir(), "outside-marker.json")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	markerPath := testRunMarkerPath(paths)
	if err := os.Symlink(outside, markerPath); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	marker, err := BeginRun(paths)
	require.Error(t, err)
	require.Nil(t, marker)
	require.Equal(t, []byte("outside"), mustReadFile(t, outside))
}

func TestBeginRunRejectsWeakMarkerPermissionsOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows validates the marker ACL through securefile helpers")
	}
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	markerPath := testRunMarkerPath(paths)
	require.NoError(t, os.Chmod(markerPath, 0o644))

	next, err := BeginRun(paths)
	require.Error(t, err)
	require.Nil(t, next)
	require.NoError(t, os.Chmod(markerPath, 0o600))
	require.NoError(t, marker.Finish())
}

func TestRunMarkerConcurrentFinishIsSafe(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- marker.Finish()
		}()
	}
	wg.Wait()
	close(errs)
	for finishErr := range errs {
		require.NoError(t, finishErr)
	}
	_, err = os.Stat(testRunMarkerPath(paths))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestRunMarkerPayloadDoesNotContainRuntimeSecrets(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	raw := string(mustReadFile(t, testRunMarkerPath(paths)))
	for _, forbidden := range []string{"password", "pid", "process_id", "secret"} {
		require.False(t, strings.Contains(strings.ToLower(raw), forbidden), "payload contains %q", forbidden)
	}
	require.NoError(t, marker.Finish())
}

func TestRunMarkerMalformedPayloadIsNotAcceptedAsPreviousRun(t *testing.T) {
	paths := runMarkerTestPaths(t)
	markerPath := testRunMarkerPath(paths)
	for _, raw := range [][]byte{
		[]byte(`{"schema":2,"nonce":"n","started_at":"2026-09-08T00:00:00Z"}`),
		[]byte(`{"schema":1,"nonce":"n","started_at":"not-time"}`),
		[]byte(`{"schema":1,"nonce":"n","started_at":"2026-09-08T00:00:00Z","extra":true}`),
	} {
		require.NoError(t, os.WriteFile(markerPath, raw, 0o600))
		marker, err := BeginRun(paths)
		require.Error(t, err)
		require.Nil(t, marker)
		require.Equal(t, raw, mustReadFile(t, markerPath))
	}
}

func TestRunMarkerErrorsRemainInspectable(t *testing.T) {
	paths := runMarkerTestPaths(t)
	markerPath := testRunMarkerPath(paths)
	require.NoError(t, os.WriteFile(markerPath, []byte(`{"schema":1,"nonce":"n","started_at":"2026-09-08T00:00:00Z"}`), 0o600))
	_, err := BeginRun(paths)
	require.Error(t, err)
	require.False(t, errors.Is(err, os.ErrNotExist), "a malformed existing marker is not a first run")
}

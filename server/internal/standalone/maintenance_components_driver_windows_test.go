//go:build windows

package standalone

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestWindowsMaintenanceCandidateComponentsDriver runs the launcher-side
// candidate component test once per case. Each case gets a new install tree,
// new secrets and new local ports; no production config or data directory is
// used. The launcher test owns the instance lock after this driver releases it.
func TestWindowsMaintenanceCandidateComponentsDriver(t *testing.T) {
	launcherTest := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH"))
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)

	for _, mode := range []string{"complete", "media-failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
			require.NoError(t, fixture.lock.Close())

			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, launcherTest,
				"-test.run=^TestWindowsMaintenanceCandidateComponents$",
				"-test.v", "-test.timeout=5m")
			cmd.Env = maintenanceComponentDriverEnvironment(fixture.paths.InstallDir, mode)
			output, err := cmd.CombinedOutput()
			t.Logf("candidate component case %s launcher output:\n%s", mode, redactMaintenanceComponentOutput(t, fixture.paths, string(output)))
			if errors := ctx.Err(); errors != nil {
				t.Fatalf("candidate component case %s timed out: %v", mode, errors)
			}
			require.NoError(t, err)
			require.NotContains(t, string(output), "--- SKIP")

			candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
			require.NoError(t, err)
			// The launcher owns both real images through its Job. A successful
			// return must leave no candidate backend, Redis or media process.
			require.NoError(t, backupComponentsStopped(candidate))
			require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
			current, err := LoadRelease(fixture.paths.InstallDir)
			require.NoError(t, err)
			require.Equal(t, fixture.journal.OldVersion, current.Version)
		})
	}
}

func maintenanceComponentReleaseRoot(t *testing.T) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_COMPONENT_RELEASE_ROOT"))
	if value == "" {
		t.Skip("requires UVP_MAINTENANCE_COMPONENT_RELEASE_ROOT pointing to a real release directory")
	}
	root, err := filepath.Abs(value)
	require.NoError(t, err)
	root = filepath.Clean(root)
	manifestPath := filepath.Join(root, "manifest.json")
	if info, statErr := os.Stat(manifestPath); statErr == nil && !info.IsDir() {
		return root
	} else if statErr != nil && !os.IsNotExist(statErr) {
		require.NoError(t, statErr)
	}
	// Accepting the package install root as a convenience keeps the input
	// explicit while still resolving the immutable release through its pointer.
	release, err := LoadRelease(root)
	require.NoError(t, err)
	return release.ReleaseDir
}

func newMaintenanceComponentFixture(t *testing.T, backend, sourceRoot string) maintenanceBackendTestFixture {
	t.Helper()
	fixture := newMaintenanceBackendTestFixture(t, backend)
	sourceManifestRaw, err := os.ReadFile(filepath.Join(sourceRoot, "manifest.json"))
	require.NoError(t, err)
	sourceManifest, err := decodeReleaseManifest(sourceManifestRaw)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(sourceManifest.SourceCommit))

	currentManifest := copyMaintenanceComponentRelease(t, sourceRoot, fixture.current.releaseDir, fixture.current.manifest.Version, sourceManifest.SourceCommit)
	candidateManifest := copyMaintenanceComponentRelease(t, sourceRoot, fixture.candidate.releaseDir, fixture.candidate.manifest.Version, sourceManifest.SourceCommit)
	// The backend path is a separately supplied immutable build input. Keep the
	// Redis and ZLM closure from the real release tree, then bind this candidate
	// manifest to the backend actually selected by the driver.
	candidateBackend := filepath.Join(fixture.candidate.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, candidateBackend)
	candidateManifest = rewriteMaintenanceComponentManifest(t, fixture.candidate.releaseDir, fixture.candidate.manifest.Version, sourceManifest.SourceCommit)
	fixture.current.manifest = currentManifest
	fixture.candidate.manifest = candidateManifest

	// The launcher child fixture derives resource/web from the current release;
	// both releases therefore contain the real resource closure above.
	fixture.paths.ResourceDir = filepath.Join(fixture.current.releaseDir, "resource")
	fixture.paths.WebDir = filepath.Join(fixture.current.releaseDir, "web")
	configureMaintenanceComponentPorts(t, fixture.paths)
	return fixture
}

func copyMaintenanceComponentRelease(t *testing.T, sourceRoot, destinationRoot, version, sourceCommit string) testReleaseManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sourceRoot, "manifest.json"))
	require.NoError(t, err)
	source, err := decodeReleaseManifest(raw)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(destinationRoot))
	require.NoError(t, os.MkdirAll(destinationRoot, 0700))
	// Copy only the verified immutable inputs. A previously run source may also
	// contain media logs, which must never become hashed release payloads.
	for _, file := range source.Files {
		sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(file.Path))
		_, err := ensureReleaseChild(sourceRoot, sourcePath, false)
		require.NoError(t, err)
		digest, err := releaseFileSHA256(sourcePath)
		require.NoError(t, err)
		require.Equal(t, strings.ToLower(file.SHA256), digest)
		copyMaintenanceComponentFile(t, sourcePath, filepath.Join(destinationRoot, filepath.FromSlash(file.Path)))
	}
	return rewriteMaintenanceComponentManifest(t, destinationRoot, version, sourceCommit)
}

func rewriteMaintenanceComponentManifest(t *testing.T, releaseDir, version, sourceCommit string) testReleaseManifest {
	t.Helper()
	manifest := testReleaseManifest{
		FormatVersion: 1,
		Version:       version,
		SourceCommit:  sourceCommit,
		SchemaMin:     1,
		SchemaMax:     3,
	}
	err := filepath.WalkDir(releaseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(releaseDir, path)
		if err != nil {
			return err
		}
		if relative == "." || strings.EqualFold(filepath.ToSlash(relative), "manifest.json") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate release contains symbolic link %q", relative)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("candidate release contains non-regular file %q", relative)
		}
		sha, err := releaseFileSHA256(path)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, testReleaseFile{Path: filepath.ToSlash(relative), SHA256: sha})
		return nil
	})
	require.NoError(t, err)
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	require.NotEmpty(t, manifest.Files)
	writeTestReleaseManifest(t, testReleaseFixture{releaseDir: releaseDir, manifest: manifest})
	return manifest
}

func copyMaintenanceComponentFile(t *testing.T, source, destination string) {
	t.Helper()
	info, err := os.Stat(source)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	require.NoError(t, copyMaintenanceComponentFileWithMode(source, destination, info.Mode().Perm()))
}

func copyMaintenanceComponentFileWithMode(source, destination string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0600
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func configureMaintenanceComponentPorts(t *testing.T, paths Paths) {
	t.Helper()
	config, err := LoadConfig(paths)
	require.NoError(t, err)
	values := config.values
	used := make(map[int]struct{})
	backendPort := maintenanceComponentTCPPort(t, used)
	redisPort := maintenanceComponentTCPPort(t, used)
	httpPort := maintenanceComponentTCPPort(t, used)
	rtpPort := maintenanceComponentDualPort(t, used)
	rtcPort := maintenanceComponentDualPort(t, used)

	httpserver, ok := values["httpserver"].(map[string]any)
	require.True(t, ok)
	httpserver["port"] = fmt.Sprintf("127.0.0.1:%d", backendPort)
	redis, ok := values["redis"].(map[string]any)
	require.True(t, ok)
	redis["port"] = redisPort
	gb, ok := values["gb28181"].(map[string]any)
	require.True(t, ok)
	zlm, ok := gb["zlm"].(map[string]any)
	require.True(t, ok)
	zlm["listenip"] = "127.0.0.1"
	zlm["httpport"] = httpPort
	zlm["rtpport"] = rtpPort
	zlm["rtcport"] = rtcPort
	zlm["rtctcpport"] = rtcPort
	media, ok := gb["media"].(map[string]any)
	require.True(t, ok)
	media["hookport"] = backendPort

	raw, err := yaml.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(paths.ConfigFile, raw, true, nil))
	_, err = InitializeConfig(paths)
	require.NoError(t, err)
}

func maintenanceComponentTCPPort(t *testing.T, used map[int]struct{}) int {
	t.Helper()
	for attempt := 0; attempt < 32; attempt++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		require.NoError(t, err)
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		if _, exists := used[port]; exists {
			continue
		}
		used[port] = struct{}{}
		return port
	}
	t.Fatal("could not allocate an unused TCP port")
	return 0
}

func maintenanceComponentDualPort(t *testing.T, used map[int]struct{}) int {
	t.Helper()
	// TCP's ephemeral allocator need not avoid UDP exclusion ranges on Windows.
	// Inspect distinct candidate ports and hold both sockets until verified.
	for port := 20000; port <= 65535; port++ {
		if _, exists := used[port]; exists {
			continue
		}
		tcp, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		udp, udpErr := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", port))
		_ = tcp.Close()
		if udpErr != nil {
			continue
		}
		_ = udp.Close()
		used[port] = struct{}{}
		return port
	}
	t.Fatal("could not allocate an unused TCP/UDP port")
	return 0
}

func maintenanceComponentDriverEnvironment(root, mode string) []string {
	forbidden := map[string]struct{}{
		"UVP_MAINTENANCE_TEST_ROOT":              {},
		"UVP_MAINTENANCE_COMPONENT_CASE":         {},
		"UVP_MAINTENANCE_COMPONENT_TEST_ROOT":    {},
		"UVP_MAINTENANCE_COMPONENT_RELEASE_ROOT": {},
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, skip := forbidden[strings.ToUpper(key)]; skip {
				continue
			}
		}
		env = append(env, entry)
	}
	env = append(env, "UVP_MAINTENANCE_TEST_ROOT="+root, "UVP_MAINTENANCE_COMPONENT_CASE="+mode)
	return env
}

func redactMaintenanceComponentOutput(t *testing.T, paths Paths, output string) string {
	t.Helper()
	config, err := LoadConfig(paths)
	if err != nil {
		return output
	}
	for _, secret := range []string{config.JWTSecret(), config.RedisPassword(), config.ZLMSecret()} {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	return output
}

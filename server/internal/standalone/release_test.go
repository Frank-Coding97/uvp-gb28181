package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type testReleaseFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"-"`
}

type testReleaseManifest struct {
	FormatVersion int               `json:"format_version"`
	Version       string            `json:"version"`
	SourceCommit  string            `json:"source_commit"`
	Files         []testReleaseFile `json:"files"`
	SchemaMin     int               `json:"schema_min"`
	SchemaMax     int               `json:"schema_max"`
}

type testReleaseFixture struct {
	installDir string
	releaseDir string
	manifest   testReleaseManifest
}

func TestLoadReleaseAcceptsVerifiedManifest(t *testing.T) {
	fixture := newTestReleaseFixture(t, "1.2.3-win10")

	release, err := LoadRelease(fixture.installDir)

	require.NoError(t, err)
	require.Equal(t, fixture.manifest.Version, release.Version)
	require.Equal(t, fixture.manifest.SourceCommit, release.SourceCommit)
	require.Equal(t, fixture.releaseDir, release.ReleaseDir)
	require.Equal(t, filepath.Join(fixture.releaseDir, "backend", "uvp-server.exe"), release.BackendExe)
	require.Equal(t, filepath.Join(fixture.releaseDir, "redis", "redis-server.exe"), release.RedisExe)
	require.Equal(t, filepath.Join(fixture.releaseDir, "media", "MediaServer.exe"), release.MediaExe)
	require.Equal(t, filepath.Join(fixture.releaseDir, "web"), release.WebDir)
	require.Equal(t, filepath.Join(fixture.releaseDir, "resource"), release.ResourceDir)
}

func TestLoadReleaseRejectsInvalidCurrentJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed", raw: `{"version":`},
		{name: "trailing document", raw: `{"version":"1.2.3-win10"}{"version":"1.2.3-win10"}`},
		{name: "unknown field", raw: `{"version":"1.2.3-win10","extra":1}`},
		{name: "duplicate field", raw: `{"version":"1.2.3-win10","version":"1.2.3-win10"}`},
		{name: "missing version", raw: `{}`},
		{name: "array", raw: `[]`},
		{name: "dot", raw: `{"version":"."}`},
		{name: "dot dot", raw: `{"version":".."}`},
		{name: "relative escape", raw: `{"version":"../outside"}`},
		{name: "slash", raw: `{"version":"1/2"}`},
		{name: "backslash", raw: `{"version":"1\\2"}`},
		{name: "leading hyphen", raw: `{"version":"-1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newTestReleaseFixture(t, "1.2.3-win10")
			require.NoError(t, os.WriteFile(filepath.Join(fixture.installDir, "current.json"), []byte(tt.raw), 0o600))

			_, err := LoadRelease(fixture.installDir)

			require.Error(t, err)
		})
	}
}

func TestLoadReleaseRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testReleaseFixture)
	}{
		{
			name: "wrong format version",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.FormatVersion = 2
			},
		},
		{
			name: "wrong release version",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Version = "9.9.9"
			},
		},
		{
			name: "empty source commit",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.SourceCommit = ""
			},
		},
		{
			name: "schema minimum below supported",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.SchemaMin = 0
			},
		},
		{
			name: "schema maximum above supported",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.SchemaMax = 3
			},
		},
		{
			name: "schema range reversed",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.SchemaMin = 2
				fixture.manifest.SchemaMax = 1
			},
		},
		{
			name: "invalid checksum",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files[0].SHA256 = "not-a-sha256"
			},
		},
		{
			name: "missing required executable",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = fixture.manifest.Files[1:]
			},
		},
		{
			name: "manifest path escape",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = append(fixture.manifest.Files, testReleaseFile{Path: "../outside.txt", SHA256: testSHA256([]byte("outside"))})
			},
		},
		{
			name: "absolute manifest path",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = append(fixture.manifest.Files, testReleaseFile{Path: "/outside.txt", SHA256: testSHA256([]byte("outside"))})
			},
		},
		{
			name: "backslash manifest path",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = append(fixture.manifest.Files, testReleaseFile{Path: `backend\outside.txt`, SHA256: testSHA256([]byte("outside"))})
			},
		},
		{
			name: "duplicate path with different case",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = append(fixture.manifest.Files, testReleaseFile{Path: "BACKEND/UVP-SERVER.EXE", SHA256: fixture.manifest.Files[0].SHA256})
			},
		},
		{
			name: "dot manifest path",
			mutate: func(fixture *testReleaseFixture) {
				fixture.manifest.Files = append(fixture.manifest.Files, testReleaseFile{Path: "./readme.txt", SHA256: testSHA256([]byte("readme"))})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newTestReleaseFixture(t, "1.2.3-win10")
			tt.mutate(&fixture)
			writeTestReleaseManifest(t, fixture)

			_, err := LoadRelease(fixture.installDir)

			require.Error(t, err)
		})
	}
}

func TestLoadReleaseRejectsTamperedFileAndUnlistedRuntime(t *testing.T) {
	t.Run("tampered file", func(t *testing.T) {
		fixture := newTestReleaseFixture(t, "1.2.3-win10")
		tampered := filepath.Join(fixture.releaseDir, "backend", "uvp-server.exe")
		require.NoError(t, os.WriteFile(tampered, []byte("tampered"), 0o600))

		_, err := LoadRelease(fixture.installDir)

		require.Error(t, err)
	})

	t.Run("unlisted dll", func(t *testing.T) {
		fixture := newTestReleaseFixture(t, "1.2.3-win10")
		plugins := filepath.Join(fixture.releaseDir, "plugins")
		require.NoError(t, os.MkdirAll(plugins, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(plugins, "sidecar.DLL"), []byte("sidecar"), 0o600))

		_, err := LoadRelease(fixture.installDir)

		require.Error(t, err)
	})
}

func TestLoadReleaseRejectsMissingRequiredDirectories(t *testing.T) {
	tests := []string{"web", "resource"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newTestReleaseFixture(t, "1.2.3-win10")
			require.NoError(t, os.RemoveAll(filepath.Join(fixture.releaseDir, name)))

			_, err := LoadRelease(fixture.installDir)

			require.Error(t, err)
		})
	}
}

func TestLoadReleaseRejectsSymlinkedReleaseFile(t *testing.T) {
	fixture := newTestReleaseFixture(t, "1.2.3-win10")
	target := filepath.Join(fixture.releaseDir, "backend", "uvp-server.exe")
	outside := filepath.Join(t.TempDir(), "outside.exe")
	require.NoError(t, os.WriteFile(outside, []byte("backend"), 0o600))
	require.NoError(t, os.Remove(target))
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	_, err := LoadRelease(fixture.installDir)

	require.Error(t, err)
}

func TestLoadReleaseRejectsSymlinkedResourceDirectory(t *testing.T) {
	fixture := newTestReleaseFixture(t, "1.2.3-win10")
	resource := filepath.Join(fixture.releaseDir, "resource")
	outside := filepath.Join(t.TempDir(), "outside-resource")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "static.txt"), []byte("resource"), 0o600))
	require.NoError(t, os.RemoveAll(resource))
	if err := os.Symlink(outside, resource); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	_, err := LoadRelease(fixture.installDir)

	require.Error(t, err)
}

func newTestReleaseFixture(t *testing.T, version string) testReleaseFixture {
	t.Helper()
	installDir := filepath.Join(t.TempDir(), "UVP 发布包 中文 # spaces")
	releaseDir := filepath.Join(installDir, "releases", version)
	for _, dir := range []string{
		installDir,
		filepath.Join(installDir, "releases"),
		releaseDir,
		filepath.Join(releaseDir, "backend"),
		filepath.Join(releaseDir, "redis"),
		filepath.Join(releaseDir, "media"),
		filepath.Join(releaseDir, "web"),
		filepath.Join(releaseDir, "resource"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	fixture := testReleaseFixture{
		installDir: installDir,
		releaseDir: releaseDir,
		manifest: testReleaseManifest{
			FormatVersion: 1,
			Version:       version,
			SourceCommit:  "0123456789abcdef0123456789abcdef01234567",
			SchemaMin:     1,
			SchemaMax:     2,
			Files: []testReleaseFile{
				{Path: "backend/uvp-server.exe", Data: []byte("backend")},
				{Path: "redis/redis-server.exe", Data: []byte("redis")},
				{Path: "media/MediaServer.exe", Data: []byte("media")},
				{Path: "web/index.html", Data: []byte("web")},
				{Path: "resource/static.txt", Data: []byte("resource")},
			},
		},
	}
	for i := range fixture.manifest.Files {
		file := &fixture.manifest.Files[i]
		file.SHA256 = testSHA256(file.Data)
		path := filepath.Join(fixture.releaseDir, filepath.FromSlash(file.Path))
		require.NoError(t, os.WriteFile(path, file.Data, 0o600))
	}
	writeTestReleaseManifest(t, fixture)
	require.NoError(t, os.WriteFile(filepath.Join(installDir, "current.json"), []byte(`{"version":"`+version+`"}`+"\n"), 0o600))
	return fixture
}

func writeTestReleaseManifest(t *testing.T, fixture testReleaseFixture) {
	t.Helper()
	raw, err := json.MarshalIndent(fixture.manifest, "", "  ")
	require.NoError(t, err)
	raw = append(raw, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(fixture.releaseDir, "manifest.json"), raw, 0o600))
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

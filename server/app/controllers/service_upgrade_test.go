package controllers

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestPublicUpgradeTaskOmitsHelperFilesystemPaths(t *testing.T) {
	record := upgrade.TaskRecord{
		TaskID: "task", IdempotencyKey: "idem", Actor: "admin", TargetDigest: strings.Repeat("a", 64),
		Source: "offline", Version: "1.2.0", BuildID: "build-1", Phase: upgrade.PhaseFailed,
		FailurePhase: upgrade.PhaseValidating, Error: "candidate readiness failed",
		ReleaseNotes: "修复服务升级", PublishedAt: "2026-10-18T07:00:00Z",
		StagingPath: "/var/lib/uvp/staging/task.tar", BackupPath: "/var/lib/uvp/backups/task",
		PreviousTarget: "/var/lib/uvp/releases/old", Preflight: &upgrade.PreflightSnapshot{ConfigDigest: strings.Repeat("b", 64)},
	}
	public := publicUpgradeTask(&record)
	data, err := json.Marshal(public)
	require.NoError(t, err)
	body := string(data)
	require.Contains(t, body, "candidate readiness failed")
	require.Contains(t, body, "修复服务升级")
	require.Contains(t, body, "2026-10-18T07:00:00Z")
	require.NotContains(t, body, "/var/lib/uvp")
	require.NotContains(t, body, "config_digest")
}

func TestConfiguredUpgradeKeysAcceptsBase64AndHex(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	keys := configuredUpgradeKeys(map[string]interface{}{
		"base64": "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		"hex":    hex.EncodeToString(key),
		"bad":    "too-short",
	})
	require.Len(t, keys, 2)
	require.Equal(t, key, []byte(keys["hex"]))
}

func TestConfiguredCheckFollowsSignedChannelPointer(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	var channelEnvelope, releaseEnvelope []byte
	releaseRequests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/channels/stable/manifest.json":
			_, _ = writer.Write(channelEnvelope)
		case "/releases/release-2/manifest.json":
			releaseRequests++
			_, _ = writer.Write(releaseEnvelope)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	release := upgrade.Manifest{
		SchemaVersion:      1,
		Channel:            "stable",
		Sequence:           2,
		ReleaseID:          "release-2",
		Version:            "1.2.0",
		BuildID:            "build-2",
		MinUpdaterProtocol: 1,
		Artifacts: []upgrade.Artifact{{
			Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH,
			Filename: "uvp.tar.gz", URL: server.URL + "/releases/release-2/uvp.tar.gz", Size: 1,
			SHA256: strings.Repeat("a", 64),
		}},
	}
	releaseEnvelope = signUpgradeEnvelope(t, private, release)
	releaseDigest := sha256.Sum256(releaseEnvelope)
	now := time.Now().UTC()
	channelEnvelope = signUpgradeEnvelope(t, private, upgrade.ChannelManifest{
		SchemaVersion:  1,
		Channel:        "stable",
		Sequence:       2,
		ReleaseID:      "release-2",
		PublishedAt:    now.Add(-time.Minute).Format(time.RFC3339),
		ExpiresAt:      now.Add(time.Hour).Format(time.RFC3339),
		ManifestURL:    server.URL + "/releases/release-2/manifest.json",
		ManifestSHA256: hex.EncodeToString(releaseDigest[:]),
	})

	oldConfig := app.ConfigYml
	app.ConfigYml = &configControllerYML{values: map[string]interface{}{
		"upgrade.release_source": server.URL + "/channels/stable/manifest.json",
		"upgrade.trusted_keys":   map[string]interface{}{"release-key": base64.StdEncoding.EncodeToString(public)},
	}}
	t.Cleanup(func() {
		app.ConfigYml = oldConfig
	})
	stateRoot := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", filepath.Join(stateRoot, "updater-state"))
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(stateRoot, "version.json"), HTTPClient: server.Client()})
	checker := controller.configuredCheck()
	require.NotNil(t, checker)

	manifest, err := checker(context.Background())
	require.NoError(t, err)
	require.Equal(t, "release-2", manifest.ReleaseID)
	require.Equal(t, 1, releaseRequests)
}

func TestConfiguredCheckRejectsDirectReleaseEnvelope(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	releaseEnvelope := signUpgradeEnvelope(t, private, upgrade.Manifest{
		SchemaVersion: 1, Channel: "stable", Sequence: 2, ReleaseID: "release-2", Version: "1.2.0", BuildID: "build-2", MinUpdaterProtocol: 1,
		Artifacts: []upgrade.Artifact{{Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH, Filename: "uvp.tar.gz", URL: "https://updates.example/release-2/uvp.tar.gz", Size: 1, SHA256: strings.Repeat("a", 64)}},
	})
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/channels/stable/manifest.json", request.URL.Path)
		_, _ = writer.Write(releaseEnvelope)
	}))
	defer server.Close()

	oldConfig := app.ConfigYml
	app.ConfigYml = &configControllerYML{values: map[string]interface{}{
		"upgrade.release_source": server.URL + "/channels/stable/manifest.json",
		"upgrade.trusted_keys":   map[string]interface{}{"release-key": base64.StdEncoding.EncodeToString(public)},
	}}
	t.Cleanup(func() {
		app.ConfigYml = oldConfig
	})
	stateRoot := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", filepath.Join(stateRoot, "updater-state"))
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(stateRoot, "version.json"), HTTPClient: server.Client()})
	checker := controller.configuredCheck()
	require.NotNil(t, checker)

	_, err = checker(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel")
}

func signUpgradeEnvelope(t *testing.T, private ed25519.PrivateKey, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	envelope, err := json.Marshal(upgrade.SignedManifest{
		Payload: base64.StdEncoding.EncodeToString(payload),
		Signatures: []upgrade.ManifestSignature{{
			KeyID: "release-key", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, payload)),
		}},
	})
	require.NoError(t, err)
	return envelope
}

func TestServiceUpgradeCheckReturnsMatchedArtifactAndCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"name":"uvp-gb28181","version":"1.1.0"}`), 0600))
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		Check: func(context.Context) (upgrade.Manifest, error) {
			return upgrade.Manifest{
				SchemaVersion: 1, Channel: "stable", Sequence: 2, ReleaseID: "r2", Version: "1.2.0", BuildID: "b2", MinUpdaterProtocol: 1,
				Artifacts: []upgrade.Artifact{{Deployment: "standalone", OS: runtime.GOOS, Arch: runtime.GOARCH, Filename: "uvp.tar.gz", URL: "https://updates.example/r2/uvp.tar.gz", Size: 1, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
			}, nil
		},
	})
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/api/system/upgrade/check", nil)
	controller.Check(ctx)
	require.Equal(t, 200, recorder.Code)
	var envelope struct {
		Data struct {
			CurrentVersion string `json:"currentVersion"`
			HasUpdate      bool   `json:"hasUpdate"`
			Artifact       struct {
				Filename string `json:"filename"`
			} `json:"artifact"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "1.1.0", envelope.Data.CurrentVersion)
	require.True(t, envelope.Data.HasUpdate)
	require.Equal(t, "uvp.tar.gz", envelope.Data.Artifact.Filename)
}

func TestAcceptSequencePersistsAndRejectsReplay(t *testing.T) {
	dir := t.TempDir()
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(dir, "version.json")})
	require.NoError(t, controller.acceptSequence("stable", 3, []byte("payload-a")))
	require.NoError(t, controller.acceptSequence("stable", 3, []byte("payload-a")))
	require.Error(t, controller.acceptSequence("stable", 2, []byte("payload-old")))
	require.Error(t, controller.acceptSequence("stable", 3, []byte("payload-b")))
	_, err := os.Stat(controller.sequencePath)
	require.NoError(t, err)
}

func TestValidateTrustedTaskTargetRejectsArbitraryDigest(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0"}`), 0600))
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath})
	err := controller.validateTrustedTaskTarget(context.Background(), strings.Repeat("a", 64))
	require.Error(t, err)
	require.Contains(t, err.Error(), "trusted release source")
}

func TestValidateTrustedTaskTargetAcceptsVerifiedStagedArtifact(t *testing.T) {
	controller, digest, _ := newTrustedStagedArtifactFixture(t, "standalone", "1.1.0", "1.2.0", "")
	require.NoError(t, controller.validateTrustedTaskTarget(context.Background(), digest))
}

func TestValidateTrustedTaskTargetRejectsStagedVersionOutsideSignedPolicy(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		currentVersion string
		targetVersion  string
		minimumVersion string
	}{
		{name: "same version", currentVersion: "1.1.0", targetVersion: "1.1.0"},
		{name: "downgrade", currentVersion: "1.1.0", targetVersion: "1.0.9"},
		{name: "below signed minimum", currentVersion: "1.1.0", targetVersion: "1.2.0", minimumVersion: "1.1.1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			controller, digest, _ := newTrustedStagedArtifactFixture(t, "standalone", scenario.currentVersion, scenario.targetVersion, scenario.minimumVersion)
			err := controller.validateTrustedTaskTarget(context.Background(), digest)
			require.Error(t, err)
		})
	}
}

func TestPrepareTaskArtifactUsesSignedStagedMetadata(t *testing.T) {
	controller, digest, manifest := newTrustedStagedArtifactFixture(t, "compose", "1.1.0", "1.2.0", "")

	staged, err := controller.prepareTaskArtifact(context.Background(), digest)

	require.NoError(t, err)
	artifact, err := manifest.ArtifactFor(upgrade.Target{Deployment: "compose", OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion})
	require.NoError(t, err)
	require.Equal(t, manifest.ReleaseID, staged.ReleaseID)
	require.Equal(t, manifest.Version, staged.Version)
	require.Equal(t, manifest.BuildID, staged.BuildID)
	require.Equal(t, artifact.Filename, staged.Filename)
	require.Equal(t, artifact.Images, staged.Images)
	require.True(t, upgrade.CompatibilityEqual(manifest.Compatibility, staged.Compatibility))
}

func TestCurrentReleaseIdentityRequiresMatchingRuntimeIdentity(t *testing.T) {
	versionPath := filepath.Join(t.TempDir(), "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})

	version, buildID, err := controller.currentReleaseIdentity()

	require.NoError(t, err)
	require.Equal(t, "1.1.0", version)
	require.Equal(t, "build-a", buildID)
}

func TestCurrentReleaseIdentityUsesEmbeddedBuildIDWithoutLoadedVersionInfo(t *testing.T) {
	versionPath := filepath.Join(t.TempDir(), "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	oldVersion, oldBuildID := app.AppVersion, app.BuildID
	app.AppVersion = app.VersionInfo{}
	app.BuildID = "build-a"
	t.Cleanup(func() {
		app.AppVersion, app.BuildID = oldVersion, oldBuildID
	})
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath})

	version, buildID, err := controller.currentReleaseIdentity()

	require.NoError(t, err)
	require.Equal(t, "1.1.0", version)
	require.Equal(t, "build-a", buildID)
}

func TestCurrentReleaseIdentityRejectsForgedDiskIdentity(t *testing.T) {
	versionPath := filepath.Join(t.TempDir(), "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"9.9.9","build_id":"forged-build"}`), 0600))
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})

	_, _, err := controller.currentReleaseIdentity()

	require.Error(t, err)
	require.Contains(t, err.Error(), "does not match running binary")
}

func TestCurrentReleaseIdentityAllowsLegacyPackageWithoutBuildID(t *testing.T) {
	versionPath := filepath.Join(t.TempDir(), "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0"}`), 0600))
	runtimeCalls := 0
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			runtimeCalls++
			return "1.1.0", "build-a"
		},
	})

	version, buildID, err := controller.currentReleaseIdentity()

	require.NoError(t, err)
	require.Equal(t, "1.1.0", version)
	require.Empty(t, buildID)
	require.Zero(t, runtimeCalls)
}

func TestValidateManualCompatibilityRejectsLegacyBuildForAutomaticRelease(t *testing.T) {
	declaration := &upgrade.CompatibilityDeclaration{
		SchemaVersion:    1,
		AutomaticAllowed: true,
		SourceBuilds:     []upgrade.ReleaseIdentity{{Version: "1.1.0", BuildID: "build-a"}},
		Database: upgrade.DatabaseCompatibility{
			Dialect:                       "sqlite",
			AllowedSourceMigrationDigests: []string{strings.Repeat("a", 64)},
			TargetMigrationDigest:         strings.Repeat("b", 64),
		},
	}

	err := validateManualCompatibility(declaration, upgrade.PreflightSnapshot{
		CurrentVersion: "1.1.0", DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("a", 64),
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "current version and build")
}

func TestDownloadArtifactPersistsVerifiedEnvelopeAndCompatibility(t *testing.T) {
	body := []byte("verified release package")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/release.tar.gz", request.URL.Path)
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", filepath.Join(root, "updater-state"))
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(root, "version.json"), HTTPClient: server.Client()})
	compatibility := &upgrade.CompatibilityDeclaration{
		SchemaVersion:    1,
		AutomaticAllowed: true,
		SourceBuilds:     []upgrade.ReleaseIdentity{{Version: "1.1.0", BuildID: "source-build"}},
		Database: upgrade.DatabaseCompatibility{
			Dialect:                       "sqlite",
			AllowedSourceMigrationDigests: []string{strings.Repeat("a", 64)},
			TargetMigrationDigest:         strings.Repeat("b", 64),
		},
	}
	manifest := upgrade.Manifest{
		SchemaVersion: 1,
		ReleaseID:     "release-2",
		Version:       "1.2.0",
		BuildID:       "build-2",
		Envelope:      []byte("verified-signed-envelope"),
		Compatibility: compatibility,
	}
	artifact := upgrade.Artifact{
		Deployment: "standalone",
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Filename:   "release.tar.gz",
		URL:        server.URL + "/release.tar.gz",
		Size:       int64(len(body)),
		SHA256:     digest,
	}

	require.NoError(t, controller.downloadArtifact(context.Background(), manifest, artifact))
	staged, err := controller.findStagedArtifact(digest)
	require.NoError(t, err)
	require.Equal(t, manifest.Envelope, staged.ManifestEnvelope)
	require.True(t, upgrade.CompatibilityEqual(compatibility, staged.Compatibility))
	compatibility.Database.TargetMigrationDigest = strings.Repeat("c", 64)
	require.Equal(t, strings.Repeat("b", 64), staged.Compatibility.Database.TargetMigrationDigest)
}

func TestCollectUpgradePreflightUsesConfiguredRuntimePaths(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	setTestRuntimeIdentity(t, "1.1.0", "build-a")
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("server:\n  port: 8080\n"), 0600))
	setupUpgradePreflightDatabase(t, dir)
	t.Setenv("UVP_VERSION_PATH", versionPath)

	snapshot, err := CollectUpgradePreflight(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1.1.0", snapshot.CurrentVersion)
	require.Equal(t, "build-a", snapshot.CurrentBuildID)
	require.Equal(t, "sqlite", snapshot.DatabaseDialect)
	require.Len(t, snapshot.MigrationDigest, 64)
}

func TestSchedulerPreflightChangesWhenInstalledConfigurationChanges(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	configPath := filepath.Join(configDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0600))
	db := setupUpgradePreflightDatabase(t, dir)
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})
	first, err := controller.schedulerPreflight(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, first.ConfigDigest)
	require.Equal(t, "build-a", first.CurrentBuildID)
	require.Equal(t, "sqlite", first.DatabaseDialect)
	require.NoError(t, os.WriteFile(configPath, []byte("server:\n  port: 8081\n"), 0600))
	second, err := controller.schedulerPreflight(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, first.ConfigDigest, second.ConfigDigest)
	require.Equal(t, first.MigrationDigest, second.MigrationDigest)

	require.NoError(t, db.Exec("INSERT INTO gb_schema_migrations (version, applied_at) VALUES (?, ?)", "2026-10-16-extra.sql", "2026-10-16T00:00:00Z").Error)
	third, err := controller.schedulerPreflight(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, second.MigrationDigest, third.MigrationDigest)

	require.Equal(t, "build-a", third.CurrentBuildID)
}

func TestSchedulerPreflightReadsSymlinkedConfiguration(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	sharedConfigDir := filepath.Join(dir, "shared-config")
	require.NoError(t, os.MkdirAll(sharedConfigDir, 0700))
	configPath := filepath.Join(sharedConfigDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("server:\n  port: 8080\n"), 0600))
	require.NoError(t, os.Symlink(sharedConfigDir, filepath.Join(dir, "config")))
	setupUpgradePreflightDatabase(t, dir)

	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})
	first, err := controller.schedulerPreflight(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte("server:\n  port: 8081\n"), 0600))
	second, err := controller.schedulerPreflight(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, first.ConfigDigest, second.ConfigDigest)
}

func TestSchedulerPreflightFailsClosedWithoutConfiguration(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	setupUpgradePreflightDatabase(t, dir)

	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})
	_, err := controller.schedulerPreflight(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration")
}

func TestSchedulerPreflightFailsClosedWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("server:\n  port: 8080\n"), 0600))
	oldConfig, oldMySQL, oldSQLite, oldPostgreSQL, oldSQLServer := app.ConfigYml, app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver
	app.ConfigYml = &configControllerYML{values: map[string]interface{}{"gormv2.usedbtype": "sqlite"}}
	app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver = nil, nil, nil, nil
	t.Cleanup(func() {
		app.ConfigYml, app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver = oldConfig, oldMySQL, oldSQLite, oldPostgreSQL, oldSQLServer
	})

	controller := newServiceUpgradeController(serviceUpgradeOptions{
		VersionPath: versionPath,
		RuntimeIdentity: func() (string, string) {
			return "1.1.0", "build-a"
		},
	})
	_, err := controller.schedulerPreflight(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "database")
}

func TestSchedulerPreflightPropagatesContextToMigrationQuery(t *testing.T) {
	dir := t.TempDir()
	versionPath := filepath.Join(dir, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0","build_id":"build-a"}`), 0600))
	configDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), []byte("server:\n  port: 8080\n"), 0600))
	setupUpgradePreflightDatabase(t, dir)

	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := controller.migrationStateDigest(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func setupUpgradePreflightDatabase(t *testing.T, dir string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "upgrade-preflight.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE gb_schema_migrations (version TEXT NOT NULL, applied_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_schema_migrations (version, applied_at) VALUES (?, ?)", "2026-10-16-initial.sql", "2026-10-16T00:00:00Z").Error)
	oldConfig, oldMySQL, oldSQLite, oldPostgreSQL, oldSQLServer := app.ConfigYml, app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver
	app.ConfigYml = &configControllerYML{values: map[string]interface{}{"gormv2.usedbtype": "sqlite"}}
	app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver = nil, db, nil, nil
	t.Cleanup(func() {
		app.ConfigYml, app.GormDbMysql, app.GormDbSqlite, app.GormDbPostgreSql, app.GormDbSqlserver = oldConfig, oldMySQL, oldSQLite, oldPostgreSQL, oldSQLServer
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestPrepareTaskPersistsManualPreflightBeforeAwaitingConfirmation(t *testing.T) {
	want := manualUpgradePreflight("config-a")
	controller, store, record := newManualUpgradeTaskFixture(t, func(context.Context) (upgrade.PreflightSnapshot, error) {
		return want, nil
	})

	controller.prepareTask(context.Background(), record)

	prepared, err := store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseAwaitingConfirmation, prepared.Phase)
	require.NotNil(t, prepared.Preflight)
	require.Equal(t, want, *prepared.Preflight)
	require.NotEmpty(t, prepared.StagingPath)
}

func TestPrepareTaskFailsWithoutManualPreflightEvidence(t *testing.T) {
	controller, store, record := newManualUpgradeTaskFixture(t, func(context.Context) (upgrade.PreflightSnapshot, error) {
		return upgrade.PreflightSnapshot{}, errors.New("runtime preflight is unavailable")
	})

	controller.prepareTask(context.Background(), record)

	prepared, err := store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseFailed, prepared.Phase)
	require.Nil(t, prepared.Preflight)
	require.Contains(t, prepared.Error, "runtime preflight is unavailable")
}

func TestApplyTaskRejectsManualTaskWhenPreflightChanges(t *testing.T) {
	current := manualUpgradePreflight("config-a")
	controller, store, record := newManualUpgradeTaskFixture(t, func(context.Context) (upgrade.PreflightSnapshot, error) {
		return current, nil
	})
	controller.prepareTask(context.Background(), record)
	current.ConfigDigest = "config-b"
	t.Setenv("UVP_UPDATER_SOCKET", filepath.Join(t.TempDir(), "missing-updater.sock"))

	response := invokeServiceUpgradeApply(t, controller, record.TaskID)

	require.Equal(t, http.StatusConflict, response.Code)
	updated, err := store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseFailed, updated.Phase)
	require.Contains(t, updated.Error, "installation state changed")
}

func TestApplyTaskRejectsManualTaskWithoutPreflight(t *testing.T) {
	preflightCalls := 0
	controller, store, record := newManualUpgradeTaskFixture(t, func(context.Context) (upgrade.PreflightSnapshot, error) {
		preflightCalls++
		return manualUpgradePreflight("config-a"), nil
	})
	for _, phase := range []upgrade.TaskPhase{upgrade.PhaseVerifying, upgrade.PhasePreparing, upgrade.PhaseAwaitingConfirmation} {
		require.NoError(t, store.MarkPhase(record.TaskID, phase))
	}
	t.Setenv("UVP_UPDATER_SOCKET", filepath.Join(t.TempDir(), "missing-updater.sock"))

	response := invokeServiceUpgradeApply(t, controller, record.TaskID)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Zero(t, preflightCalls)
	updated, err := store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, upgrade.PhaseFailed, updated.Phase)
	require.Contains(t, updated.Error, "no installation preflight snapshot")
}

func manualUpgradePreflight(configDigest string) upgrade.PreflightSnapshot {
	return upgrade.PreflightSnapshot{
		CurrentVersion:  "1.1.0",
		CurrentBuildID:  "build-a",
		ConfigDigest:    configDigest,
		DatabaseDialect: "sqlite",
		MigrationDigest: strings.Repeat("a", 64),
	}
}

func setTestRuntimeIdentity(t *testing.T, version, buildID string) {
	t.Helper()
	oldVersion, oldBuildID := app.AppVersion, app.BuildID
	app.AppVersion = app.VersionInfo{Version: version, BuildID: buildID}
	app.BuildID = buildID
	t.Cleanup(func() {
		app.AppVersion, app.BuildID = oldVersion, oldBuildID
	})
}

func configureTrustedUpgradeKey(t *testing.T, deployment string, public ed25519.PublicKey) {
	t.Helper()
	oldConfig := app.ConfigYml
	app.ConfigYml = &configControllerYML{values: map[string]interface{}{
		"upgrade.deployment":   deployment,
		"upgrade.trusted_keys": map[string]interface{}{"release-key": base64.StdEncoding.EncodeToString(public)},
	}}
	t.Cleanup(func() { app.ConfigYml = oldConfig })
}

func newTrustedStagedArtifactFixture(t *testing.T, deployment, currentVersion, targetVersion, minimumVersion string) (*ServiceUpgradeController, string, upgrade.Manifest) {
	t.Helper()
	root := t.TempDir()
	versionPath := filepath.Join(root, "version.json")
	versionData, err := json.Marshal(map[string]string{"version": currentVersion})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(versionPath, versionData, 0600))
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	configureTrustedUpgradeKey(t, deployment, public)
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: versionPath})
	body := []byte("signed staged release package")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	images := []upgrade.ImageArtifact(nil)
	if deployment == "compose" {
		images = []upgrade.ImageArtifact{{
			Reference: "registry.example.test/uvp/backend:1.2.0",
			Archive:   "images/backend.tar",
			Digest:    "sha256:" + strings.Repeat("a", 64),
			ConfigID:  "sha256:" + strings.Repeat("b", 64),
		}}
	}
	compatibility := &upgrade.CompatibilityDeclaration{
		SchemaVersion: 1,
		SourceBuilds:  []upgrade.ReleaseIdentity{{Version: currentVersion, BuildID: "signed-source-build"}},
	}
	manifest := upgrade.Manifest{
		SchemaVersion:      1,
		Channel:            "stable",
		Sequence:           2,
		ReleaseID:          "release-2",
		Version:            targetVersion,
		BuildID:            "build-2",
		MinVersion:         minimumVersion,
		MinUpdaterProtocol: upgrade.UpdaterProtocolVersion,
		Compatibility:      compatibility,
		Artifacts: []upgrade.Artifact{{
			Deployment: deployment,
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			Filename:   "signed-release.tar",
			URL:        "https://updates.example.test/releases/release-2/signed-release.tar",
			Size:       int64(len(body)),
			SHA256:     digest,
			Images:     images,
		}},
	}
	manifest.Envelope = signUpgradeEnvelope(t, private, manifest)
	path := filepath.Join(controller.stagingRoot(), digest+"-stored.tar")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, body, 0600))
	require.NoError(t, upgrade.SaveStagedMetadata(path+".json", upgrade.StagedArtifact{
		Path:             path,
		Filename:         "sidecar.tar",
		Size:             int64(len(body)),
		SHA256:           digest,
		ReleaseID:        "sidecar-release",
		Version:          "9.9.9",
		BuildID:          "sidecar-build",
		Deployment:       deployment,
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		Images:           []upgrade.ImageArtifact{{Reference: "untrusted.example/uvp:latest", Archive: "images/untrusted.tar"}},
		Compatibility:    &upgrade.CompatibilityDeclaration{SchemaVersion: 1},
		ManifestEnvelope: manifest.Envelope,
	}))
	return controller, digest, manifest
}

func newManualUpgradeTaskFixture(t *testing.T, preflight func(context.Context) (upgrade.PreflightSnapshot, error)) (*ServiceUpgradeController, *upgrade.TaskStore, upgrade.TaskRecord) {
	t.Helper()
	root := t.TempDir()
	store, err := upgrade.OpenTaskStore(filepath.Join(root, "tasks"))
	require.NoError(t, err)
	versionPath := filepath.Join(root, "version.json")
	require.NoError(t, os.WriteFile(versionPath, []byte(`{"version":"1.1.0"}`), 0600))
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	configureTrustedUpgradeKey(t, "standalone", public)
	controller := newServiceUpgradeController(serviceUpgradeOptions{
		Store:       store,
		VersionPath: versionPath,
		Preflight:   preflight,
	})
	body := []byte("verified manual upgrade package")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	manifest := upgrade.Manifest{
		SchemaVersion:      1,
		Channel:            "stable",
		Sequence:           2,
		ReleaseID:          "manual-release",
		Version:            "1.2.0",
		BuildID:            "manual-build",
		MinUpdaterProtocol: upgrade.UpdaterProtocolVersion,
		Artifacts: []upgrade.Artifact{{
			Deployment: "standalone",
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			Filename:   "manual.tar",
			URL:        "https://updates.example.test/releases/manual-release/manual.tar",
			Size:       int64(len(body)),
			SHA256:     digest,
		}},
	}
	manifest.Envelope = signUpgradeEnvelope(t, private, manifest)
	path := filepath.Join(controller.stagingRoot(), digest+"-manual.tar")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, body, 0600))
	require.NoError(t, upgrade.SaveStagedMetadata(path+".json", upgrade.StagedArtifact{
		Path:             path,
		Filename:         "manual.tar",
		Size:             int64(len(body)),
		SHA256:           digest,
		ReleaseID:        "manual-release",
		Version:          "1.2.0",
		BuildID:          "manual-build",
		Deployment:       "standalone",
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		ManifestEnvelope: manifest.Envelope,
	}))
	record, err := store.Submit(upgrade.TaskRequest{IdempotencyKey: "manual-upgrade", Actor: "admin", TargetDigest: digest})
	require.NoError(t, err)
	return controller, store, record
}

func invokeServiceUpgradeApply(t *testing.T, controller *ServiceUpgradeController, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: taskID}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/tasks/"+taskID+"/apply", nil)
	func() {
		defer func() { _ = recover() }()
		controller.ApplyTask(ctx)
	}()
	return recorder
}

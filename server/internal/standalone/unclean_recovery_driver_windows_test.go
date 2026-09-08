//go:build windows

package standalone

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
	"uvplatform.cn/uvp-gb28181/internal/standalone/winprocess"
)

const (
	uncleanRecoveryDriverAdminID       int64 = 42
	uncleanRecoveryDriverAdminUsername       = "unclean-driver-admin"
	uncleanRecoveryDriverAdminPassword       = "Unclean!Driver42"
	uncleanRecoveryDriverSessionSID          = "unclean-driver-session"
)

type uncleanRecoveryRedisExpectation struct {
	values map[string]string
	expiry map[string]int64
}

// TestWindowsUncleanRecoveryRealComponents exercises schema-2 recovery with
// the supplied qualified backend and bundled Redis. Each case owns a fresh
// install tree; the test never uses the configured installation directories.
func TestWindowsUncleanRecoveryRealComponents(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)
	for _, initialized := range []bool{false, true} {
		name := "pristine-pending-admin"
		if initialized {
			name = "administrator-awaiting-confirmation"
		}
		t.Run(name, func(t *testing.T) {
			runWindowsUncleanRecoveryRealCase(t, backend, componentRoot, initialized)
		})
	}
}

func runWindowsUncleanRecoveryRealCase(t *testing.T, backend, componentRoot string, initialized bool) {
	t.Helper()
	fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	seedUpgradeDatabase(t, fixture)
	expectation := seedUncleanRecoveryRedis(t, fixture)
	if initialized {
		seedUncleanRecoveryAdministrator(t, fixture)
	}
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)

	before, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	currentPointer, err := os.ReadFile(filepath.Join(fixture.paths.InstallDir, "current.json"))
	require.NoError(t, err)
	lock := fixture.lock
	_, err = BeginRun(fixture.paths)
	require.NoError(t, err)
	marker, err := os.ReadFile(filepath.Join(fixture.paths.DataDir, runMarkerName))
	require.NoError(t, err)
	require.NoError(t, lock.Close())

	trust, err := releaseFileSHA256(backend)
	require.NoError(t, err)
	destination := filepath.Join(filepath.Dir(fixture.paths.InstallDir), "unclean-driver-snapshot")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	launcherPath := strings.TrimSpace(os.Getenv("UVP_UNCLEAN_LAUNCHER_PATH"))
	var result UncleanRecoveryResult
	if launcherPath == "" {
		result, err = recoverUncleanStoppedWithTrust(ctx, fixture.paths, destination, trust,
			uncleanRecoveryDriverMaintenanceRunner(t), recoverRedisStage)
	} else {
		result, err = runUncleanRecoveryLauncher(ctx, launcherPath, fixture.paths, destination)
	}
	require.NoError(t, err)
	require.Equal(t, initialized, result.AwaitingLocalConfirmation)

	after, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	require.NotEqual(t, uncleanRecoverySecretHash(before.JWTSecret()), uncleanRecoverySecretHash(after.JWTSecret()))
	require.NotEqual(t, uncleanRecoverySecretHash(before.InstanceGeneration()), uncleanRecoverySecretHash(after.InstanceGeneration()))
	require.Equal(t, uncleanRecoverySecretHash(before.RedisPassword()), uncleanRecoverySecretHash(after.RedisPassword()))
	actualPointer, err := os.ReadFile(filepath.Join(fixture.paths.InstallDir, "current.json"))
	require.NoError(t, err)
	require.Equal(t, currentPointer, actualPointer)
	if initialized {
		journal, err := ReadMaintenanceJournal(fixture.paths.InstallDir)
		require.NoError(t, err)
		require.Equal(t, MaintenanceAwaitingConfirmation, journal.Phase)
		require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
		confirmCtx, confirmCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err = confirmRecoveryWithTrust(confirmCtx, fixture.paths.InstallDir, result.OperationID, trust,
			func(info RecoveryConfirmationInfo) (RecoveryConfirmationInput, error) {
				require.Equal(t, result.OperationID, info.OperationID)
				require.Equal(t, maintenanceKindUnclean, info.Kind)
				return RecoveryConfirmationInput{
					Username:        uncleanRecoveryDriverAdminUsername,
					Password:        uncleanRecoveryDriverAdminPassword,
					Acknowledgement: "CONFIRM " + info.OperationID,
				}, nil
			})
		confirmCancel()
		require.NoError(t, err)
	} else {
		require.NoError(t, CheckMaintenanceGate(fixture.paths.InstallDir))
	}

	require.NoError(t, assertUncleanRecoveryRedis(t, fixture, expectation))
	if initialized {
		assertUncleanRecoverySessionRevoked(t, fixture.paths.DatabasePath)
	}
	archive := filepath.Join(fixture.paths.InstallDir, ".uvp-recovered-"+result.OperationID)
	require.Equal(t, marker, mustReadUncleanRecoveryFile(t, filepath.Join(archive, "failed", result.OperationID, "data", runMarkerName)))
	require.NoFileExists(t, filepath.Join(fixture.paths.DataDir, runMarkerName))
	if initialized {
		require.FileExists(t, filepath.Join(archive, "confirmation.json"))
	} else {
		require.FileExists(t, filepath.Join(archive, "pristine-confirmation.json"))
		require.NoFileExists(t, filepath.Join(archive, "confirmation.json"))
	}
	releases, _, err := installedMaintenanceReleaseSnapshot(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.NoError(t, backupComponentsStopped(releases...))
}

func runUncleanRecoveryLauncher(ctx context.Context, launcherPath string, paths Paths, destination string) (UncleanRecoveryResult, error) {
	var result UncleanRecoveryResult
	cmd := exec.CommandContext(ctx, launcherPath, "recover",
		"--install-dir", paths.InstallDir,
		"--recordings-dir", paths.RecordingsDir,
		"--snapshot", destination)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("unclean recovery launcher failed")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	manifest, err := VerifyBackup(ctx, destination)
	if err != nil {
		return result, err
	}
	journal, journalErr := ReadMaintenanceJournal(paths.InstallDir)
	gateErr := CheckMaintenanceGate(paths.InstallDir)
	switch {
	case journalErr == nil:
		if journal.OperationID != manifest.OperationID || journal.Phase != MaintenanceAwaitingConfirmation || !errors.Is(gateErr, ErrMaintenanceRequired) {
			return result, errors.New("unclean recovery launcher returned an invalid awaiting state")
		}
		return UncleanRecoveryResult{OperationID: manifest.OperationID, AwaitingLocalConfirmation: true}, nil
	case errors.Is(journalErr, os.ErrNotExist):
		if gateErr != nil {
			return result, errors.New("unclean recovery launcher returned an invalid archived state")
		}
		return UncleanRecoveryResult{OperationID: manifest.OperationID}, nil
	default:
		return result, errors.New("unclean recovery launcher journal is unavailable")
	}
}

func uncleanRecoveryDriverMaintenanceRunner(t *testing.T) MaintenanceRunner {
	t.Helper()
	return func(ctx context.Context, paths Paths, operation, purpose, version string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		frame, err := IssueMaintenancePermit(paths.InstallDir, operation, purpose, version)
		if err != nil {
			return err
		}
		defer clear(frame)
		args := []string(nil)
		if purpose == "db_check" {
			args = []string{"-db-check"}
		}
		release, err := LoadReleaseVersion(paths.InstallDir, version)
		if err != nil {
			return err
		}
		output := runMaintenanceBackend(t, release.BackendExe, paths, args, frame)
		assertUpgradeDriverMaintenanceSuccess(t, output, frame, purpose, version, paths.InstallDir)
		return nil
	}
}

func seedUncleanRecoveryAdministrator(t *testing.T, fixture maintenanceBackendTestFixture) {
	t.Helper()
	addRecoveryAuthorizationUser(t, fixture.paths.DatabasePath, uncleanRecoveryDriverAdminID, uncleanRecoveryDriverAdminUsername, uncleanRecoveryDriverAdminPassword)
	execRecoveryPristineSQL(t, fixture.paths.DatabasePath, `UPDATE standalone_installation SET phase='pending_sip', admin_user_id=? WHERE id=1`, uncleanRecoveryDriverAdminID)
	now := time.Now().UTC()
	execRecoveryAuthorizationSQL(t, fixture.paths.DatabasePath, `INSERT INTO sys_user_sessions (sid, user_id, refresh_token_hash, refresh_jti, login_at, last_active_at, session_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uncleanRecoveryDriverSessionSID, uncleanRecoveryDriverAdminID, strings.Repeat("a", 64), strings.Repeat("b", 36), now, now, now.Add(time.Hour))
}

func seedUncleanRecoveryRedis(t *testing.T, fixture maintenanceBackendTestFixture) uncleanRecoveryRedisExpectation {
	t.Helper()
	config, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	current, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.OldVersion)
	require.NoError(t, err)
	redisDir := filepath.Join(fixture.paths.DataDir, "redis")
	require.NoError(t, os.MkdirAll(redisDir, 0700))
	controlDir := t.TempDir()
	job, err := winprocess.NewJob()
	require.NoError(t, err)
	children := make([]*recoveryRedisProcess, 0, 1)
	configDirs := make([]string, 0, 1)
	cleaned := false
	defer func() {
		if !cleaned {
			_ = cleanupRecoveryRedisStage(job, children, configDirs)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	child, err := startRecoveryRedis(ctx, job, current.RedisExe, redisDir, controlDir, configInt(config.values, "redis", "indexdb"), true, &configDirs)
	if child != nil {
		children = append(children, child)
	}
	require.NoError(t, err)

	expected := uncleanRecoveryRedisExpectation{
		values: map[string]string{
			"account_locked:alice": "1",
			"login_fail_count:bob": "3",
		},
		expiry: make(map[string]int64, 2),
	}
	for key, value := range expected.values {
		require.NoError(t, child.client.Set(ctx, key, value, 20*time.Minute).Err())
		expiresAt, err := child.client.Do(ctx, "PEXPIRETIME", key).Int64()
		require.NoError(t, err)
		expected.expiry[key] = expiresAt
	}
	for key, value := range map[string]string{
		"uvp-gb28181:qr:token:fixture": "qr-secret-fixture",
		"refresh:fixture":              "refresh-secret-fixture",
		"unknown:secret":               "unknown-secret-fixture",
	} {
		require.NoError(t, child.client.Set(ctx, key, value, 20*time.Minute).Err())
	}
	require.NoError(t, child.client.Set(ctx, "account_locked:expired", "1", 2*time.Millisecond).Err())
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, stopRecoveryRedis(ctx, job, child))
	require.NoError(t, cleanupRecoveryRedisStage(job, children, configDirs))
	cleaned = true
	return expected
}

func assertUncleanRecoveryRedis(t *testing.T, fixture maintenanceBackendTestFixture, expected uncleanRecoveryRedisExpectation) error {
	t.Helper()
	config, err := LoadConfig(fixture.paths)
	if err != nil {
		return err
	}
	release, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.OldVersion)
	if err != nil {
		return err
	}
	controlDir := t.TempDir()
	job, err := winprocess.NewJob()
	if err != nil {
		return err
	}
	children := make([]*recoveryRedisProcess, 0, 1)
	configDirs := make([]string, 0, 1)
	cleaned := false
	defer func() {
		if !cleaned {
			_ = cleanupRecoveryRedisStage(job, children, configDirs)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	child, err := startRecoveryRedis(ctx, job, release.RedisExe, filepath.Join(fixture.paths.DataDir, "redis"), controlDir, configInt(config.values, "redis", "indexdb"), false, &configDirs)
	if child != nil {
		children = append(children, child)
	}
	if err != nil {
		return err
	}
	keys, err := scanRecoveryKeys(ctx, child.client)
	if err != nil {
		return err
	}
	wantKeys := make(map[string]struct{}, len(expected.values))
	for key := range expected.values {
		wantKeys[key] = struct{}{}
	}
	if len(keys) != len(wantKeys) {
		return errors.New("recovered Redis contains unexpected keys")
	}
	for key := range wantKeys {
		if _, ok := keys[key]; !ok {
			return errors.New("recovered Redis is missing a restriction key")
		}
		value, getErr := child.client.Get(ctx, key).Result()
		if getErr != nil || value != expected.values[key] {
			return errors.New("recovered Redis restriction value changed")
		}
		expiresAt, expiryErr := child.client.Do(ctx, "PEXPIRETIME", key).Int64()
		if expiryErr != nil || expiresAt != expected.expiry[key] {
			return errors.New("recovered Redis restriction expiry changed")
		}
	}
	if err := stopRecoveryRedis(ctx, job, child); err != nil {
		return err
	}
	if err := cleanupRecoveryRedisStage(job, children, configDirs); err != nil {
		return err
	}
	cleaned = true
	return nil
}

func assertUncleanRecoverySessionRevoked(t *testing.T, path string) {
	t.Helper()
	db, cleanup, err := openRecoveryAuthorizationDatabase(t.Context(), path)
	require.NoError(t, err)
	defer cleanup()
	var revokedAt, refreshHash, refreshJTI, reason sql.NullString
	err = db.QueryRowContext(t.Context(), `SELECT revoked_at, refresh_token_hash, refresh_jti, revoke_reason FROM sys_user_sessions WHERE sid=?`, uncleanRecoveryDriverSessionSID).Scan(&revokedAt, &refreshHash, &refreshJTI, &reason)
	require.NoError(t, err)
	require.True(t, revokedAt.Valid)
	require.False(t, refreshHash.Valid)
	require.False(t, refreshJTI.Valid)
	require.Equal(t, "backup_restore", reason.String)
}

func uncleanRecoverySecretHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func mustReadUncleanRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return raw
}

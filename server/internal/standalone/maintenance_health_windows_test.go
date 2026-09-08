//go:build windows

package standalone

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWindowsMaintenanceBackendRevokesSessionsOffline(t *testing.T) {
	fixture := newMaintenanceBackendTestFixture(t, maintenanceBackendTestPath(t))
	frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, "bootstrap_db", fixture.candidate.manifest.Version)
	require.NoError(t, err)
	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-bootstrap-db"}, frame)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	clear(frame)
	require.Zero(t, result.exitCode)
	db, err := sql.Open("sqlite", fixture.paths.DatabasePath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO sys_user_sessions(sid,user_id,refresh_token_hash,refresh_jti,login_at,last_active_at,session_expires_at)
VALUES ('maintenance-test-session',1,'test-hash','test-jti',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,datetime('now','+1 day'))`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Reusing this binary tests the internal restore entry only. It is not
	// evidence of compatibility or rollback between two real release builds.
	backend, err := os.ReadFile(fixture.candidateBackendPath())
	require.NoError(t, err)
	oldPath := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	require.NoError(t, os.WriteFile(oldPath, backend, 0700))
	fixture.current.manifest.Files[0].Data = backend
	fixture.current.manifest.Files[0].SHA256 = testSHA256(backend)
	writeTestReleaseManifest(t, fixture.current)
	require.NoError(t, advanceMaintenance(fixture.paths.InstallDir, fixture.journal.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.NoError(t, advanceMaintenance(fixture.paths.InstallDir, fixture.journal.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
	frame, err = IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, "revoke_sessions", fixture.current.manifest.Version)
	require.NoError(t, err)
	result = runMaintenanceBackend(t, oldPath, fixture.paths, nil, frame)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	clear(frame)
	require.Zero(t, result.exitCode)
	var completed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.stdout), &completed))
	require.Equal(t, "maintenance_complete", completed["status"])
	require.Equal(t, "revoke_sessions", completed["purpose"])
	db, err = sql.Open("sqlite", fixture.paths.DatabasePath)
	require.NoError(t, err)
	defer db.Close()
	var revoked int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM sys_user_sessions WHERE sid='maintenance-test-session' AND revoked_at IS NOT NULL AND refresh_token_hash IS NULL AND refresh_jti IS NULL AND revoke_reason='backup_restore'`).Scan(&revoked))
	require.Equal(t, 1, revoked)
	assertMaintenancePermitMissing(t, fixture.paths.InstallDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
}

func TestWindowsMaintenanceBackendBootstrapMigrateHealth(t *testing.T) {
	fixture := newMaintenanceBackendTestFixture(t, maintenanceBackendTestPath(t))
	server := newRecoveryRedisServer(t)
	client := server.client(0)
	defer client.Close()
	cfg, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	// This Redis belongs only to this test. Its password remains in protected
	// config and the Redis protocol, never a child argument or environment value.
	require.NoError(t, client.ConfigSet(context.Background(), "requirepass", cfg.RedisPassword()).Err())
	values := cfg.values
	values["redis"].(map[string]any)["port"] = server.port
	raw, err := yaml.Marshal(values)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(fixture.paths.ConfigFile, raw, true, nil))

	for _, action := range []struct {
		purpose string
		args    []string
	}{
		{"bootstrap_db", []string{"-bootstrap-db"}},
		{"migrate_up", []string{"-migrate-up"}},
		{"candidate_health", nil},
	} {
		t.Run(action.purpose, func(t *testing.T) {
			frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, action.purpose, fixture.candidate.manifest.Version)
			require.NoError(t, err)
			defer clear(frame)
			result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, action.args, frame)
			assertMaintenanceBackendNoPermitLeak(t, result, frame)
			require.Equal(t, 0, result.exitCode, "offline maintenance failed at %s", action.purpose)
			decoder := json.NewDecoder(strings.NewReader(result.stdout))
			var last map[string]any
			for {
				var item map[string]any
				err := decoder.Decode(&item)
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				last = item
			}
			require.Equal(t, "maintenance_complete", last["status"])
			require.Equal(t, action.purpose, last["purpose"])
			require.Equal(t, fixture.candidate.manifest.Version, last["version"])
			assertMaintenancePermitMissing(t, fixture.paths.InstallDir)
			require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
		})
		if t.Failed() {
			return
		}
	}
}

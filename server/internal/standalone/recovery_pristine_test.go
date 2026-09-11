package standalone

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/resource/database/sqlitemigrations"
)

func TestRecoveryPristinePendingAdmin(t *testing.T) {
	t.Run("fresh baseline is pristine", func(t *testing.T) {
		path := newRecoveryPristineSQLite(t)

		pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
		require.NoError(t, err)
		require.True(t, pristine)
	})

	t.Run("reads residual data from WAL without changing the source family", func(t *testing.T) {
		path := newRecoveryPristineSQLite(t)
		writer, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, writer.Close()) })
		_, err = writer.Exec(`PRAGMA journal_mode = WAL`)
		require.NoError(t, err)
		_, err = writer.Exec(`INSERT INTO sys_users (id, username, password, status, deleted_at) VALUES (99, 'wal-user', 'test-only-hash', 1, NULL)`)
		require.NoError(t, err)
		walInfo, err := os.Stat(path + "-wal")
		require.NoError(t, err)
		require.Greater(t, walInfo.Size(), int64(0))
		before := recoveryAuthorizationSQLiteArtifacts(t, path)

		pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
		require.False(t, pristine)
		require.ErrorIs(t, err, errRecoveryPristine)
		require.Equal(t, before, recoveryAuthorizationSQLiteArtifacts(t, path))
	})

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{
			name: "pending_sip with an administrator delegates confirmation",
			mutate: func(t *testing.T, path string) {
				addRecoveryAuthorizationUser(t, path, 1, "existing-admin", "Admin!Passw0rd#2026")
				execRecoveryPristineSQL(t, path, `UPDATE standalone_installation SET phase = 'pending_sip', admin_user_id = 1, completed_at = NULL WHERE id = 1`)
			},
		},
		{
			name: "complete delegates confirmation",
			mutate: func(t *testing.T, path string) {
				addRecoveryAuthorizationUser(t, path, 1, "existing-admin", "Admin!Passw0rd#2026")
				execRecoveryPristineSQL(t, path, `UPDATE standalone_installation SET phase = 'complete', admin_user_id = 1, completed_at = '2026-09-08 12:34:56' WHERE id = 1`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := newRecoveryPristineSQLite(t)
			tc.mutate(t, path)

			pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
			require.NoError(t, err)
			require.False(t, pristine)
		})
	}
}

func TestRecoveryPristinePendingAdminRejectsNonPristineState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{
			name: "deleted user counts as existing data",
			mutate: func(t *testing.T, path string) {
				addRecoveryAuthorizationUser(t, path, 1, "deleted-admin", "Admin!Passw0rd#2026")
				execRecoveryPristineSQL(t, path, `UPDATE sys_users SET deleted_at = '2026-09-08 12:34:56' WHERE id = 1`)
			},
		},
		{
			name: "residual user role binding",
			mutate: func(t *testing.T, path string) {
				execRecoveryPristineSQL(t, path, `INSERT INTO sys_user_role (user_id, role_id) VALUES (42, 1)`)
			},
		},
		{
			name: "residual user session",
			mutate: func(t *testing.T, path string) {
				execRecoveryPristineSQL(t, path, `INSERT INTO sys_user_sessions (sid, user_id, login_at, last_active_at, session_expires_at) VALUES ('pristine-session', 42, '2026-09-08 12:00:00', '2026-09-08 12:00:00', '2026-09-08 13:00:00')`)
			},
		},
		{
			name: "residual user casbin relation",
			mutate: func(t *testing.T, path string) {
				execRecoveryPristineSQL(t, path, `INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5) VALUES ('g', 'user_42', 'role_1', '*', '', '', '')`)
			},
		},
		{
			name: "missing installation table",
			mutate: func(t *testing.T, path string) {
				execRecoveryPristineSQL(t, path, `DROP TABLE standalone_installation`)
			},
		},
		{
			name: "missing installation row",
			mutate: func(t *testing.T, path string) {
				execRecoveryPristineSQL(t, path, `DELETE FROM standalone_installation WHERE id = 1`)
			},
		},
		{
			name: "multiple installation rows",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'pending_admin', NULL, NULL), (2, 'pending_admin', NULL, NULL)`)
			},
		},
		{
			name: "pending admin has administrator metadata",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'pending_admin', 1, NULL)`)
			},
		},
		{
			name: "pending sip has no administrator metadata",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'pending_sip', NULL, NULL)`)
			},
		},
		{
			name: "pending sip has completion metadata",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'pending_sip', 1, '2026-09-08 12:34:56')`)
			},
		},
		{
			name: "complete has no completion timestamp",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'complete', 1, NULL)`)
			},
		},
		{
			name: "unknown phase",
			mutate: func(t *testing.T, path string) {
				replaceRecoveryPristineInstallation(t, path, `(1, 'partial', NULL, NULL)`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := newRecoveryPristineSQLite(t)
			tc.mutate(t, path)

			pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
			require.False(t, pristine)
			require.ErrorIs(t, err, errRecoveryPristine)
		})
	}
}

func TestRecoveryPristinePendingAdminRejectsMissingRequiredTable(t *testing.T) {
	for _, table := range []string{"sys_users", "sys_user_role", "sys_user_sessions", "sys_casbin_rule"} {
		t.Run(table, func(t *testing.T) {
			path := newRecoveryPristineSQLite(t)
			execRecoveryPristineSQL(t, path, `DROP TABLE `+table)

			pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
			require.False(t, pristine)
			require.ErrorIs(t, err, errRecoveryPristine)
		})
	}
}

func TestRecoveryPristinePendingAdminRejectsCorruptDatabase(t *testing.T) {
	path := newRecoveryPristineSQLite(t)
	require.NoError(t, os.WriteFile(path, []byte("not a SQLite database"), 0o600))

	pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
	require.False(t, pristine)
	require.ErrorIs(t, err, errRecoveryPristine)
}

func TestRecoveryPristinePendingAdminDoesNotModifySourceFamily(t *testing.T) {
	path := newRecoveryPristineSQLite(t)
	before := recoveryAuthorizationSQLiteArtifacts(t, path)

	pristine, err := recoveryPristinePendingAdmin(t.Context(), path)
	require.NoError(t, err)
	require.True(t, pristine)
	require.Equal(t, before, recoveryAuthorizationSQLiteArtifacts(t, path))
}

func TestRecoveryPristinePendingAdminHonorsContext(t *testing.T) {
	path := newRecoveryPristineSQLite(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pristine, err := recoveryPristinePendingAdmin(ctx, path)
	require.False(t, pristine)
	require.ErrorIs(t, err, context.Canceled)

	pristine, err = recoveryPristinePendingAdmin(nil, path)
	require.False(t, pristine)
	require.ErrorIs(t, err, errRecoveryPristine)
}

func newRecoveryPristineSQLite(t *testing.T) string {
	t.Helper()
	path := newRecoveryAuthorizationSQLite(t)
	execRecoveryPristineSQL(t, path, sqlitemigrations.StandaloneInstallationSQL)
	return path
}

func execRecoveryPristineSQL(t *testing.T, path, statement string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	_, err = db.ExecContext(t.Context(), statement, args...)
	require.NoError(t, err)
}

func replaceRecoveryPristineInstallation(t *testing.T, path, values string) {
	t.Helper()
	execRecoveryPristineSQL(t, path, `DROP TABLE standalone_installation`)
	execRecoveryPristineSQL(t, path, `CREATE TABLE standalone_installation (id INTEGER, phase TEXT, admin_user_id INTEGER, completed_at DATETIME, created_at DATETIME, updated_at DATETIME)`)
	execRecoveryPristineSQL(t, path, `INSERT INTO standalone_installation (id, phase, admin_user_id, completed_at) VALUES `+values)
}

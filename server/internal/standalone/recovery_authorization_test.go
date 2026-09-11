package standalone

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"uvplatform.cn/uvp-gb28181/resource/database/sqlitebaseline"
)

func TestAuthenticateRecoveryAdministrator(t *testing.T) {
	const (
		username = "recovery-admin"
		password = "Admin!Passw0rd#2026"
	)

	t.Run("accepts only the enabled undeleted system administrator", func(t *testing.T) {
		path := newRecoveryAuthorizationSQLite(t)
		addRecoveryAuthorizationUser(t, path, 42, username, password)

		id, err := authenticateRecoveryAdministrator(t.Context(), path, username, password)
		require.NoError(t, err)
		require.Equal(t, int64(42), id)
	})

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, path string)
	}{
		{
			name:   "wrong password",
			mutate: func(t *testing.T, path string) {},
		},
		{
			name:   "unknown username",
			mutate: func(t *testing.T, path string) {},
		},
		{
			name: "disabled user",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_users SET status = 0 WHERE username = ?`, username)
			},
		},
		{
			name: "deleted user",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_users SET deleted_at = ? WHERE username = ?`, "2026-09-08 12:00:00", username)
			},
		},
		{
			name: "missing role binding",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `DELETE FROM sys_user_role WHERE user_id = 42 AND role_id = 1`)
			},
		},
		{
			name: "disabled role",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_role SET status = 0 WHERE id = 1`)
			},
		},
		{
			name: "deleted role",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_role SET deleted_at = ? WHERE id = 1`, "2026-09-08 12:00:00")
			},
		},
		{
			name: "wrong role name",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_role SET name = ? WHERE id = 1`, "历史管理员")
			},
		},
		{
			name: "wrong role data scope",
			mutate: func(t *testing.T, path string) {
				execRecoveryAuthorizationSQL(t, path, `UPDATE sys_role SET data_scope = 2 WHERE id = 1`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := newRecoveryAuthorizationSQLite(t)
			addRecoveryAuthorizationUser(t, path, 42, username, password)
			tc.mutate(t, path)

			attemptedPassword := password
			attemptedUsername := username
			if tc.name == "wrong password" {
				attemptedPassword = "wrong-password"
			}
			if tc.name == "unknown username" {
				attemptedUsername = "missing-admin"
			}
			_, err := authenticateRecoveryAdministrator(t.Context(), path, attemptedUsername, attemptedPassword)
			require.ErrorIs(t, err, errRecoveryAuthorization)
			require.NotErrorIs(t, err, errRecoveryAuthorizationUnsupported)
			require.NotContains(t, err.Error(), attemptedUsername)
			require.NotContains(t, err.Error(), attemptedPassword)
		})
	}

	t.Run("rejects duplicate usernames even if the unique index is absent", func(t *testing.T) {
		path := newRecoveryAuthorizationSQLite(t)
		execRecoveryAuthorizationSQL(t, path, `DROP INDEX "username"`)
		addRecoveryAuthorizationUser(t, path, 41, username, password)
		addRecoveryAuthorizationUser(t, path, 42, username, password)

		_, err := authenticateRecoveryAdministrator(t.Context(), path, username, password)
		require.ErrorIs(t, err, errRecoveryAuthorization)
	})

	t.Run("rejects a database with an unhandled access key table", func(t *testing.T) {
		path := newRecoveryAuthorizationSQLite(t)
		addRecoveryAuthorizationUser(t, path, 42, username, password)
		execRecoveryAuthorizationSQL(t, path, `CREATE TABLE access_keys (id INTEGER PRIMARY KEY, access_key TEXT, secret_key TEXT)`)

		_, err := authenticateRecoveryAdministrator(t.Context(), path, username, password)
		require.ErrorIs(t, err, errRecoveryAuthorizationUnsupported)
		require.NotContains(t, err.Error(), "secret_key")
	})
}

func TestRecoveryAuthorizationImpacts(t *testing.T) {
	const (
		username = "recovery-admin"
		password = "Admin!Passw0rd#2026"
	)

	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, restored, failed string)
		want   []string
	}{
		{
			name:   "no authorization change",
			mutate: func(t *testing.T, restored, failed string) {},
			want:   nil,
		},
		{
			name: "user enabled and reinstated",
			mutate: func(t *testing.T, restored, failed string) {
				execRecoveryAuthorizationSQL(t, failed, `UPDATE sys_users SET status = 0, deleted_at = ? WHERE username = ?`, "2026-09-08 12:00:00", username)
			},
			want: []string{"用户 \"recovery-admin\"：恢复后将重新启用账户"},
		},
		{
			name: "role fields changed",
			mutate: func(t *testing.T, restored, failed string) {
				execRecoveryAuthorizationSQL(t, failed, `UPDATE sys_role SET name = ?, status = 0, data_scope = 2, deleted_at = ? WHERE id = 1`, "历史管理员", "2026-09-08 12:00:00")
			},
			want: []string{"用户 \"recovery-admin\"：角色权限将恢复到备份值"},
		},
		{
			name: "role binding changed",
			mutate: func(t *testing.T, restored, failed string) {
				execRecoveryAuthorizationSQL(t, failed, `DELETE FROM sys_user_role WHERE user_id = 42 AND role_id = 1`)
				execRecoveryAuthorizationSQL(t, failed, `INSERT INTO sys_user_role (user_id, role_id) VALUES (42, 2)`)
			},
			want: []string{"用户 \"recovery-admin\"：角色绑定将恢复到备份值"},
		},
		{
			name: "role permission binding changed",
			mutate: func(t *testing.T, restored, failed string) {
				execRecoveryAuthorizationSQL(t, failed, `DELETE FROM sys_role_menu WHERE role_id = 1 AND menu_id = 140382`)
			},
			want: []string{"用户 \"recovery-admin\"：角色权限将恢复到备份值"},
		},
		{
			name: "password hash changed",
			mutate: func(t *testing.T, restored, failed string) {
				updateRecoveryAuthorizationPassword(t, restored, 42, "new-admin-password")
			},
			want: []string{"用户 \"recovery-admin\"：密码将恢复到备份值"},
		},
		{
			name: "multiple usernames are sorted and redacted",
			mutate: func(t *testing.T, restored, failed string) {
				hash := recoveryAuthorizationPasswordHash(t, password)
				addRecoveryAuthorizationUserWithHash(t, restored, 7, "zeta\n-admin", hash)
				addRecoveryAuthorizationUserWithHash(t, failed, 7, "zeta\n-admin", hash)
				execRecoveryAuthorizationSQL(t, failed, `UPDATE sys_users SET status = 0 WHERE username IN (?, ?)`, "zeta\n-admin", username)
			},
			want: []string{
				"用户 \"recovery-admin\"：恢复后将重新启用账户",
				"用户 \"zeta\\n-admin\"：恢复后将重新启用账户",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restored := newRecoveryAuthorizationSQLite(t)
			failed := newRecoveryAuthorizationSQLite(t)
			hash := recoveryAuthorizationPasswordHash(t, password)
			addRecoveryAuthorizationUserWithHash(t, restored, 42, username, hash)
			addRecoveryAuthorizationUserWithHash(t, failed, 42, username, hash)
			tc.mutate(t, restored, failed)

			got, err := recoveryAuthorizationImpacts(t.Context(), restored, failed)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.NotContains(t, strings.Join(got, "\n"), hash)
		})
	}
}

func TestRecoveryAuthorizationImpactsRejectsKnownAccessKeyTable(t *testing.T) {
	restored := newRecoveryAuthorizationSQLite(t)
	failed := newRecoveryAuthorizationSQLite(t)
	execRecoveryAuthorizationSQL(t, failed, `CREATE TABLE api_keys (id INTEGER PRIMARY KEY, secret TEXT)`)

	_, err := recoveryAuthorizationImpacts(t.Context(), restored, failed)
	require.ErrorIs(t, err, errRecoveryAuthorizationUnsupported)
	require.NotContains(t, err.Error(), "secret")
}

func TestRecoveryAuthorizationRejectsKnownOpenAPIClientTables(t *testing.T) {
	for _, table := range []string{"sys_openapi_client", "sys_openapi_client_scope"} {
		t.Run(table, func(t *testing.T) {
			path := newRecoveryAuthorizationSQLite(t)
			execRecoveryAuthorizationSQL(t, path, `CREATE TABLE `+table+` (id INTEGER PRIMARY KEY)`)

			_, err := authenticateRecoveryAdministrator(t.Context(), path, "missing-admin", "wrong-password")
			require.ErrorIs(t, err, errRecoveryAuthorizationUnsupported)
		})
	}
}

func TestRecoveryAuthorizationHelpersAreReadOnly(t *testing.T) {
	const (
		username = "recovery-admin"
		password = "Admin!Passw0rd#2026"
	)
	restored := newRecoveryAuthorizationSQLite(t)
	failed := newRecoveryAuthorizationSQLite(t)
	addRecoveryAuthorizationUser(t, restored, 42, username, password)
	addRecoveryAuthorizationUser(t, failed, 42, username, password)
	before := recoveryAuthorizationSQLiteArtifacts(t, restored, failed)

	_, err := recoveryAuthorizationImpacts(t.Context(), restored, failed)
	require.NoError(t, err)
	_, err = authenticateRecoveryAdministrator(t.Context(), restored, username, password)
	require.NoError(t, err)
	require.Equal(t, before, recoveryAuthorizationSQLiteArtifacts(t, restored, failed))
}

func TestRecoveryAuthorizationReadsWALWithoutMutatingSourceFiles(t *testing.T) {
	const (
		username = "recovery-admin"
		password = "Admin!Passw0rd#2026"
	)
	restored := newRecoveryAuthorizationSQLite(t)
	failed := newRecoveryAuthorizationSQLite(t)
	hash := recoveryAuthorizationPasswordHash(t, password)
	addRecoveryAuthorizationUserWithHash(t, restored, 42, username, hash)
	addRecoveryAuthorizationUserWithHash(t, failed, 42, username, hash)

	writer, err := sql.Open("sqlite", restored)
	require.NoError(t, err)
	defer func() { require.NoError(t, writer.Close()) }()
	_, err = writer.Exec(`PRAGMA journal_mode = WAL`)
	require.NoError(t, err)
	newHash := recoveryAuthorizationPasswordHash(t, "new-admin-password")
	_, err = writer.Exec(`UPDATE sys_users SET password = ? WHERE id = 42`, newHash)
	require.NoError(t, err)
	_, err = os.Stat(restored + "-wal")
	require.NoError(t, err)
	mainRaw, err := os.ReadFile(restored)
	require.NoError(t, err)
	walRaw, err := os.ReadFile(restored + "-wal")
	require.NoError(t, err)
	require.False(t, bytes.Contains(mainRaw, []byte(newHash)))
	require.True(t, bytes.Contains(walRaw, []byte(newHash)))

	before := recoveryAuthorizationSQLiteArtifacts(t, restored, failed)
	impacts, err := recoveryAuthorizationImpacts(t.Context(), restored, failed)
	require.NoError(t, err)
	require.Equal(t, []string{"用户 \"recovery-admin\"：密码将恢复到备份值"}, impacts)
	_, err = authenticateRecoveryAdministrator(t.Context(), restored, username, "new-admin-password")
	require.NoError(t, err)
	require.Equal(t, before, recoveryAuthorizationSQLiteArtifacts(t, restored, failed))
}

func newRecoveryAuthorizationSQLite(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uvp.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA foreign_keys = ON`)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA busy_timeout = 5000`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(sqlitebaseline.SQL)
	if err != nil {
		_ = tx.Rollback()
	}
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, db.Close())
	return path
}

func addRecoveryAuthorizationUser(t *testing.T, path string, id int64, username, password string) {
	t.Helper()
	addRecoveryAuthorizationUserWithHash(t, path, id, username, recoveryAuthorizationPasswordHash(t, password))
}

func recoveryAuthorizationPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

func addRecoveryAuthorizationUserWithHash(t *testing.T, path string, id int64, username, hash string) {
	t.Helper()
	execRecoveryAuthorizationSQL(t, path, `INSERT INTO sys_users (id, username, password, status, deleted_at) VALUES (?, ?, ?, 1, NULL)`, id, username, hash)
	execRecoveryAuthorizationSQL(t, path, `INSERT INTO sys_user_role (user_id, role_id) VALUES (?, 1)`, id)
}

func updateRecoveryAuthorizationPassword(t *testing.T, path string, id int64, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	execRecoveryAuthorizationSQL(t, path, `UPDATE sys_users SET password = ? WHERE id = ?`, string(hash), id)
}

func execRecoveryAuthorizationSQL(t *testing.T, path, statement string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	_, err = db.Exec(statement, args...)
	require.NoError(t, err)
}

func recoveryAuthorizationSQLiteArtifacts(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	artifacts := make(map[string]string)
	for _, path := range paths {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			artifact := path + suffix
			raw, err := os.ReadFile(artifact)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			require.NoError(t, err)
			digest := sha256.Sum256(raw)
			artifacts[artifact] = hex.EncodeToString(digest[:])
		}
	}
	keys := make([]string, 0, len(artifacts))
	for key := range artifacts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(keys))
	for _, key := range keys {
		ordered[key] = artifacts[key]
	}
	return ordered
}

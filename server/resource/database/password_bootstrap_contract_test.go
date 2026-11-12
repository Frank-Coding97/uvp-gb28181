package database_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/utils/passwordhelper"
)

func TestFreshInstallRequiresDefaultAdminPasswordChange(t *testing.T) {
	seedFile, err := os.Open(filepath.Join("baseline", "seeds", "sys_users.jsonl"))
	require.NoError(t, err)
	defer seedFile.Close()

	var admin struct {
		Username           string `json:"username"`
		Password           string `json:"password"`
		MustChangePassword bool   `json:"must_change_password"`
	}
	scanner := bufio.NewScanner(seedFile)
	require.True(t, scanner.Scan(), "管理员 seed 不得为空")
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &admin))
	require.Equal(t, "admin", admin.Username)
	require.True(t, admin.MustChangePassword, "全新部署的管理员必须被标记为待改密")
	require.NoError(t, passwordhelper.ComparePassword(admin.Password, "123456"),
		"管理员 seed 密码必须与部署默认密码配置一致")

	for _, file := range append(append([]string{}, releaseFiles...), "sqlitebaseline/baseline.sql") {
		text := strings.ToLower(string(mustRead(t, file)))
		section, found := tableSection(text, "sys_users")
		require.Truef(t, found, "%s 未找到 sys_users 建表段落", file)
		require.Containsf(t, section, "must_change_password", "%s 缺少 must_change_password 列", file)
	}
}

func TestExistingInstallPasswordMigrationDefaultsToComplete(t *testing.T) {
	root := filepath.Join("gb28181", "migrations")
	for _, tc := range []struct {
		name  string
		guard string
		zero  string
	}{
		{"2026-10-08-initial-admin-password-change.sql", "information_schema.columns", "default 0"},
		{"2026-10-08-initial-admin-password-change-postgresql.sql", "add column if not exists", "default false"},
		{"2026-10-08-initial-admin-password-change-sqlserver.sql", "col_length", "default (0)"},
		{"2026-10-08-initial-admin-password-change-sqlite.sql", "alter table", "default 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, tc.name))
			require.NoError(t, err)
			normalized := strings.ToLower(string(body))
			require.Contains(t, normalized, "must_change_password")
			require.Contains(t, normalized, tc.guard)
			require.Contains(t, normalized, tc.zero)
			require.NotContains(t, normalized, "default 1",
				"升级旧库不得推断管理员需要改密")
		})
	}
}

func TestSQLitePasswordMigrationAddsFalseForExistingRows(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("gb28181", "migrations", "2026-10-08-initial-admin-password-change-sqlite.sql"))
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	require.NoError(t, db.Exec("CREATE TABLE sys_users (id INTEGER PRIMARY KEY, username TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_users (id, username) VALUES (1, 'legacy')").Error)
	require.NoError(t, db.Exec(string(body)).Error)
	var required bool
	require.NoError(t, db.Raw("SELECT must_change_password FROM sys_users WHERE id = 1").Scan(&required).Error)
	require.False(t, required, "旧用户升级后不能误触发首次改密")
}

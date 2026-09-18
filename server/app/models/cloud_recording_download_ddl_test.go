package models

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var cloudRecordingDownloadAPIs = []string{
	"/api/gb28181/cloud-recordings/files/:id/downloads",
	"/api/gb28181/cloud-recordings/downloads/:taskid",
}

const cloudRecordingDownloadAPIGroup = "gb28181 云端录像下载"

func TestCloudRecordingDownloadPermissionMigrations(t *testing.T) {
	serverRoot := cloudRecordingDownloadServerRoot(t)
	for _, filename := range []string{
		"2026-08-12-cloud-recording-downloads.sql",
		"2026-08-12-cloud-recording-downloads-postgresql.sql",
		"2026-08-12-cloud-recording-downloads-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, path := range cloudRecordingDownloadAPIs {
				require.Contains(t, sql, path)
			}
			for _, token := range []string{"'post'", "'get'", "'delete'", "sys_api", "sys_menu_api", "sys_casbin_rule", "not exists"} {
				require.Contains(t, sql, token)
			}
			require.Contains(t, sql, cloudRecordingDownloadAPIGroup)
			require.Contains(t, sql, "deleted_at")
			require.NotContains(t, sql, "/downloads/:taskid/content")
			require.NotContains(t, sql, "create table")
		})
	}
}

func TestCloudRecordingDownloadPermissionDownMigrationsAreNonDestructive(t *testing.T) {
	serverRoot := cloudRecordingDownloadServerRoot(t)
	for _, filename := range []string{
		"2026-08-12-cloud-recording-downloads-down.sql",
		"2026-08-12-cloud-recording-downloads-postgresql-down.sql",
		"2026-08-12-cloud-recording-downloads-sqlserver-down.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			require.Contains(t, sql, "forward-only")
			require.Contains(t, sql, "source")
			require.Contains(t, sql, "select 1")
			for _, forbidden := range []string{"delete", "update", "insert", "alter", "drop", "truncate"} {
				require.NotContains(t, sql, forbidden)
			}
		})
	}
}

func TestCloudRecordingDownloadFreshInstallSeeds(t *testing.T) {
	serverRoot := cloudRecordingDownloadServerRoot(t)
	for _, filename := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, path := range cloudRecordingDownloadAPIs {
				require.Contains(t, sql, path)
			}
			for _, method := range []string{"'post'", "'get'", "'delete'"} {
				require.Contains(t, sql, method)
			}
			require.Contains(t, sql, "sys_api")
			require.Contains(t, sql, "sys_casbin_rule")
			require.Contains(t, sql, "'role_1'", "fresh installs must grant the download control APIs to admin")
			require.Contains(t, sql, cloudRecordingDownloadAPIGroup)
			require.NotContains(t, sql, "/downloads/:taskid/content")
		})
	}
}

func TestCloudRecordingDownloadFreshInstallIdentityWatermarks(t *testing.T) {
	serverRoot := cloudRecordingDownloadServerRoot(t)
	mysqlBody, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "uvp-gb28181.sql"))
	require.NoError(t, err)
	mysql := strings.ToLower(string(mysqlBody))
	requireMySQLIdentityWatermarkAfterSeed(t, mysql, "sys_api")
	requireMySQLIdentityWatermarkAfterSeed(t, mysql, "sys_casbin_rule")

	postgresBody, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "postgresql_converted.sql"))
	require.NoError(t, err)
	postgres := strings.ToLower(string(postgresBody))
	require.Contains(t, postgres, "setval('sys_api_id_seq',253,true)")
	require.Contains(t, postgres, "setval('sys_casbin_rule_id_seq',7597,true)")

	sqlServerBody, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "sqlserver_converted.sql"))
	require.NoError(t, err)
	sqlServer := strings.ToLower(string(sqlServerBody))
	require.Contains(t, sqlServer, "set identity_insert [sys_api] on")
	require.Contains(t, sqlServer, "set identity_insert [sys_casbin_rule] on")
}

func requireMySQLIdentityWatermarkAfterSeed(t *testing.T, sql, table string) {
	t.Helper()
	tablePattern := regexp.MustCompile(`(?s)create table ` + regexp.QuoteMeta("`"+table+"`") + `.*?auto_increment=(\d+).*?;`)
	match := tablePattern.FindStringSubmatch(sql)
	require.Len(t, match, 2, table)
	nextID, err := strconv.Atoi(match[1])
	require.NoError(t, err)

	seedStart := strings.Index(sql, "-- seed data for `"+table+"`")
	require.NotEqual(t, -1, seedStart, table)
	seed := sql[seedStart:]
	if nextSeed := strings.Index(seed[1:], "-- seed data for `"); nextSeed >= 0 {
		seed = seed[:nextSeed+1]
	}
	maxID := 0
	for _, row := range regexp.MustCompile(`(?m)^\((\d+),`).FindAllStringSubmatch(seed, -1) {
		id, parseErr := strconv.Atoi(row[1])
		require.NoError(t, parseErr)
		if id > maxID {
			maxID = id
		}
	}
	require.Greater(t, nextID, maxID, table)
}

func cloudRecordingDownloadServerRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

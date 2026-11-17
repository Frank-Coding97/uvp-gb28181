package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoShareMigrationsCoverEveryDialect(t *testing.T) {
	files := []struct {
		dialect Dialect
		up      string
		down    string
	}{
		{DialectMySQL, "2026-11-17-channel-video-share.sql", "2026-11-17-channel-video-share-down.sql"},
		{DialectPostgres, "2026-11-17-channel-video-share-postgresql.sql", "2026-11-17-channel-video-share-postgresql-down.sql"},
		{DialectSQLServer, "2026-11-17-channel-video-share-sqlserver.sql", "2026-11-17-channel-video-share-sqlserver-down.sql"},
		{DialectSQLite, "2026-11-17-channel-video-share-sqlite.sql", "2026-11-17-channel-video-share-sqlite-down.sql"},
		{DialectDM, "2026-11-17-channel-video-share-dm.sql", "2026-11-17-channel-video-share-dm-down.sql"},
	}
	for _, file := range files {
		t.Run(string(file.dialect), func(t *testing.T) {
			up, err := (&embedSource{dialect: file.dialect}).ReadSQL(file.up)
			require.NoError(t, err)
			for _, contract := range []string{"gb_channel_video_share", "gb_device_video_share_policy", "gb_video_share_authority", "channel_pk", "public_id", "generation", "device_epoch", "policy_revision", "expires_at"} {
				require.Contains(t, up, contract)
			}
			_, err = (&embedSource{dialect: file.dialect}).ReadSQL(file.down)
			require.NoError(t, err)
			require.Equal(t, []string{"gb_channel_video_share", "gb_device_video_share_policy", "gb_video_share_authority"}, baselineTableMigrations[file.up].tables)
		})
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-11-17-channel-video-share-sqlite.sql")
	require.NoError(t, err)
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(up))
	require.NoError(t, executor.ExecSQL(up), "migration can safely rerun")
	var gateCount int64
	require.NoError(t, db.Table("gb_video_share_authority").Where("id = ?", 1).Count(&gateCount).Error)
	require.EqualValues(t, 1, gateCount)
	require.NoError(t, db.Exec(`INSERT INTO gb_channel_video_share (channel_pk, public_id, generation, source, device_pk, device_id, key_id, created_at, updated_at) VALUES (1, 'public-1', 'generation-1', 'auto', 1, '34020000002000000001', 'key-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.Error(t, db.Exec(`INSERT INTO gb_channel_video_share (channel_pk, public_id, generation, source, device_pk, device_id, key_id, created_at, updated_at) VALUES (1, 'public-2', 'generation-2', 'manual', 1, '34020000002000000001', 'key-1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
}

func TestVideoSharePermissionMigrationIsDialectPortableAndIdempotent(t *testing.T) {
	files := []string{
		"2026-11-17-channel-video-share-permissions.sql",
		"2026-11-17-channel-video-share-permissions-postgresql.sql",
		"2026-11-17-channel-video-share-permissions-sqlserver.sql",
		"2026-11-17-channel-video-share-permissions-sqlite.sql",
		"2026-11-17-channel-video-share-permissions-dm.sql",
	}
	for _, file := range files {
		_, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(file)
		require.NoError(t, err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		"CREATE TABLE sys_menu (id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,type INTEGER,permission TEXT,created_at DATETIME,updated_at DATETIME,created_by INTEGER)",
		"CREATE TABLE sys_api (id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at DATETIME,updated_at DATETIME,created_by INTEGER)",
		"CREATE TABLE sys_menu_api (menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id))",
		"CREATE TABLE sys_role_menu (role_id INTEGER,menu_id INTEGER,PRIMARY KEY(role_id,menu_id))",
		"CREATE TABLE sys_casbin_rule (id INTEGER PRIMARY KEY AUTOINCREMENT,ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
		"INSERT INTO sys_menu (id,path,permission) VALUES (140358,'/gb28181/device-mgmt/index','')",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	sql, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(files[3])
	require.NoError(t, err)
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(sql))
	require.NoError(t, executor.ExecSQL(sql), "permission migration can safely rerun")
	var apiCount, linkCount, roleCount, policyCount int64
	require.NoError(t, db.Table("sys_api").Count(&apiCount).Error)
	require.NoError(t, db.Table("sys_menu_api").Count(&linkCount).Error)
	require.NoError(t, db.Table("sys_role_menu").Count(&roleCount).Error)
	require.NoError(t, db.Table("sys_casbin_rule").Count(&policyCount).Error)
	require.EqualValues(t, 4, apiCount)
	require.EqualValues(t, 4, linkCount)
	require.EqualValues(t, 1, roleCount)
	require.EqualValues(t, 4, policyCount)
}

package sqlitebootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReleaseInitializePreservesUserDeviceAndRoleChanges(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.True(t, result.Created)
	var users int64
	require.NoError(t, db.Table("sys_users").Count(&users).Error)
	require.Zero(t, users)
	createdAt := time.Date(2026, 9, 7, 12, 34, 56, 123000000, time.FixedZone("CST", 8*3600))
	require.NoError(t, db.Exec("INSERT INTO sys_users(username,password,created_at) VALUES('fixture-user','fixture-hash',?)", createdAt).Error)
	raw, err := db.DB()
	require.NoError(t, err)
	var loaded time.Time
	require.NoError(t, raw.QueryRow("SELECT created_at FROM sys_users WHERE username='fixture-user'").Scan(&loaded))
	require.True(t, createdAt.Equal(loaded))
	require.NoError(t, db.Exec("INSERT INTO gb_device(device_id,name) VALUES('34020000001320000001','fixture-device')").Error)
	require.NoError(t, db.Exec("UPDATE sys_role SET description='user-modified' WHERE id=1").Error)
	again, err := Initialize(ctx, db)
	require.NoError(t, err)
	require.False(t, again.Created)
	require.Equal(t, result.Version, again.Version)
	require.Equal(t, result.Checksum, again.Checksum)
	var value string
	require.NoError(t, db.Raw("SELECT password FROM sys_users WHERE username='fixture-user'").Scan(&value).Error)
	require.Equal(t, "fixture-hash", value)
	require.NoError(t, db.Raw("SELECT name FROM gb_device WHERE device_id='34020000001320000001'").Scan(&value).Error)
	require.Equal(t, "fixture-device", value)
	require.NoError(t, db.Raw("SELECT description FROM sys_role WHERE id=1").Scan(&value).Error)
	require.Equal(t, "user-modified", value)
	var codes int64
	require.NoError(t, db.Table("sys_civil_code").Count(&codes).Error)
	require.EqualValues(t, 3348, codes, "locked civil-code dataset must be complete without extra baseline rows")
	require.NoError(t, db.Exec("UPDATE gb_schema_migrations SET checksum='tampered' WHERE version=?", result.Version).Error)
	_, err = Initialize(ctx, db)
	require.ErrorContains(t, err, "checksum")
}

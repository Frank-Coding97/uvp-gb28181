package setup

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeYAMLSource map[string]any

func (f fakeYAMLSource) GetString(key string) string {
	if v, ok := f[key].(string); ok {
		return v
	}
	return ""
}

func (f fakeYAMLSource) GetInt(key string) int {
	if v, ok := f[key].(int); ok {
		return v
	}
	return 0
}

// legacyDevice 模拟 gb_device 表(只要能被 Count 就行,字段随意).
type legacyDevice struct {
	ID uint `gorm:"primaryKey"`
}

func (legacyDevice) TableName() string { return "gb_device" }

func newMigrationTestDB(t *testing.T, withLegacyDevice bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	if withLegacyDevice {
		require.NoError(t, db.AutoMigrate(&legacyDevice{}))
		require.NoError(t, db.Create(&legacyDevice{ID: 1}).Error)
	}
	return db
}

func completeYAMLSource() fakeYAMLSource {
	return fakeYAMLSource{
		"gb28181.sip.deploymentmode": "lan",
		"gb28181.sip.ip":             "192.168.1.20",
		"gb28181.sip.advertiseip":    "192.168.1.20",
		"gb28181.sip.port":           5061,
		"gb28181.sip.domain":         "3402000000",
		"gb28181.sip.serverid":       "34020000002000000001",
		"gb28181.sip.password":       "Sec12345Aa!!",
	}
}

func TestMigrateYAMLToDB_SeedsWhenLegacyDeviceExists(t *testing.T) {
	db := newMigrationTestDB(t, true)
	migrated, err := MigrateYAMLToDB(context.Background(), db, completeYAMLSource())
	require.NoError(t, err)
	require.True(t, migrated)

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "192.168.1.20", row.ListenIP)
	require.Equal(t, "Sec12345Aa!!", row.Password)
}

func TestMigrateYAMLToDB_SkipsWhenNoLegacyDevice(t *testing.T) {
	db := newMigrationTestDB(t, false)
	migrated, err := MigrateYAMLToDB(context.Background(), db, completeYAMLSource())
	require.NoError(t, err)
	require.False(t, migrated, "全新装机(gb_device 表不存在)不应触发迁移")

	row, err := NewSIPConfigRepository(db).Get(context.Background())
	require.NoError(t, err)
	require.Nil(t, row)
}

func TestMigrateYAMLToDB_SkipsWhenLegacyDeviceEmpty(t *testing.T) {
	db := newMigrationTestDB(t, true)
	require.NoError(t, db.Exec("DELETE FROM gb_device").Error)
	migrated, err := MigrateYAMLToDB(context.Background(), db, completeYAMLSource())
	require.NoError(t, err)
	require.False(t, migrated, "gb_device 表空(新装但先建了表)不应触发迁移")
}

func TestMigrateYAMLToDB_IdempotentAfterDBSeeded(t *testing.T) {
	db := newMigrationTestDB(t, true)
	_, err := MigrateYAMLToDB(context.Background(), db, completeYAMLSource())
	require.NoError(t, err)

	migrated, err := MigrateYAMLToDB(context.Background(), db, completeYAMLSource())
	require.NoError(t, err)
	require.False(t, migrated, "已迁移过应幂等 skip")
}

func TestMigrateYAMLToDB_SkipsWhenYAMLIncomplete(t *testing.T) {
	db := newMigrationTestDB(t, true)
	incomplete := completeYAMLSource()
	delete(incomplete, "gb28181.sip.password")
	migrated, err := MigrateYAMLToDB(context.Background(), db, incomplete)
	require.NoError(t, err)
	require.False(t, migrated, "YAML 缺 password 应 skip 让用户走引导页")
}

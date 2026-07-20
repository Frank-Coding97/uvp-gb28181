package setup

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SIPConfig{}))
	return db
}

func TestSetupModels_AutoMigrate(t *testing.T) {
	db := newModelTestDB(t)
	require.True(t, db.Migrator().HasTable("gb_sip_config"))
	require.True(t, db.Migrator().HasColumn(&SIPConfig{}, "advertise_ip"))
	require.True(t, db.Migrator().HasColumn(&SIPConfig{}, "deployment_mode"))
}

func TestDeploymentMode_Valid(t *testing.T) {
	require.True(t, DeploymentLAN.Valid())
	require.True(t, DeploymentPublic.Valid())
	require.False(t, DeploymentMode("unknown").Valid())
	require.False(t, DeploymentMode("").Valid())
}

func TestSIPConfig_PasswordIsNotSerialized(t *testing.T) {
	b, err := json.Marshal(SIPConfig{ID: SingletonID, ServerID: "34020000002000000001", Password: "Secret123"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "Secret123")
	require.NotContains(t, string(b), "password")
}

package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIMediaSecurityRestoresIndependentOfFeatureSwitch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	calls := 0
	install := func() { calls++ }
	require.Error(t, RestoreMediaSecurity(context.Background(), db, install), "missing table never means unlocked")
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.Error(t, RestoreMediaSecurity(context.Background(), db, install), "missing row never means unlocked")
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	require.NoError(t, RestoreMediaSecurity(context.Background(), db, install))
	require.Zero(t, calls)
	store := openapiconfig.NewMustAuthStore(db, time.Now)
	_, err = store.Latch(context.Background())
	require.NoError(t, err)
	// No feature settings parameter exists: ordinary disable cannot bypass load.
	for i := 0; i < 2; i++ {
		require.NoError(t, RestoreMediaSecurity(context.Background(), db, install))
	}
	require.Equal(t, 2, calls)
	require.Error(t, RestoreMediaSecurity(context.Background(), db, nil))
	require.NoError(t, db.Where("id = ?", 1).Delete(&models.SecurityState{}).Error)
	require.Error(t, RestoreMediaSecurity(context.Background(), db, install))
	require.Equal(t, 2, calls)
}

func TestOpenAPIMediaActivationUnavailableUntilFullChain(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		gate, admin, err := InitializeRuntime(context.Background(), nil, nil, runtimeTestSettings{enabled: enabled, playEnabled: true}, nil)
		require.Error(t, err)
		require.Nil(t, gate)
		require.Nil(t, admin)
	}
}

func TestOpenAPIMediaActivationLatchesBeforePublishingIngress(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(
		&models.SecurityState{}, &models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &models.PlayGrant{}, &models.Viewer{},
		&basemodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)

	activated := 0
	require.NoError(t, ActivateMediaSecurity(context.Background(), db, func() { activated++ }))
	require.Equal(t, 1, activated)
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Load(context.Background())
	require.NoError(t, err)
	require.True(t, state.MustAuthLocked)

	t.Setenv("UVP_OPENAPI_MASTER_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	settings := runtimeTestSettings{enabled: true, playEnabled: true,
		values:   map[string]string{"openapi.audience": "test-audience", "openapi.master_key_id": "test"},
		integers: map[string]int{"httpserver.write_timeout": 30}}
	root := openapimedia.NewRuntimeRoot()
	gate, admin, err := InitializeRuntime(context.Background(), db, runtimePermissions{}, settings, root)
	require.NoError(t, err)
	require.NotNil(t, gate)
	require.NotNil(t, admin)
	require.False(t, root.Ready(), "HTTP may be installed before GB assembly, but media admission remains closed")
}

func TestOpenAPIMediaActivationFailureDoesNotEnableRuntimeProjection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_security_latch BEFORE UPDATE ON sys_openapi_security_state BEGIN SELECT RAISE(ABORT, 'denied'); END").Error)
	activated := 0
	require.Error(t, ActivateMediaSecurity(context.Background(), db, func() { activated++ }))
	require.Zero(t, activated)
}

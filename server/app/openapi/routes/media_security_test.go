package routes

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
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
		gate, admin, err := InitializeRuntime(context.Background(), nil, nil, runtimeTestSettings{enabled: enabled, playEnabled: true})
		require.Error(t, err)
		require.Nil(t, gate)
		require.Nil(t, admin)
	}
}

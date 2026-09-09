package routes

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func openAPISecurityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}
func seedOpenAPISecurity(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&models.SecurityState{}))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
}
func TestOpenAPIRootMissingSecurityStopsEvenWhenDisabled(t *testing.T) {
	oldConfig, oldDB := app.ConfigYml, app.GormDbMysql
	app.ConfigYml = openAPIRootConfig{staticDir: t.TempDir()}
	app.GormDbMysql = openAPISecurityDB(t)
	t.Cleanup(func() { app.ConfigYml, app.GormDbMysql = oldConfig, oldDB })
	require.PanicsWithValue(t, "OpenAPI media security state unavailable; HTTP and GB startup remain closed", func() { InitRoutes(gin.New()) })
}

func TestOpenAPIRootDoesNotLatchMediaAuthWhenGatewayPreflightFails(t *testing.T) {
	db := openAPISecurityDB(t)
	require.NoError(t, db.AutoMigrate(
		&models.SecurityState{}, &models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &models.PlayGrant{}, &models.Viewer{},
		&basemodels.SysDepartment{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	))
	require.NoError(t, db.Create(&models.SecurityState{ID: 1}).Error)
	t.Setenv("UVP_OPENAPI_MASTER_KEY", "invalid")
	oldConfig, oldDB, oldCasbin := app.ConfigYml, app.GormDbMysql, app.CasbinV2
	app.ConfigYml = openAPIEnabledConfig{openAPIRootConfig{staticDir: t.TempDir(), enabled: true, playEnabled: true}}
	app.GormDbMysql, app.CasbinV2 = db, openAPIRootCasbin{}
	t.Cleanup(func() { app.ConfigYml, app.GormDbMysql, app.CasbinV2 = oldConfig, oldDB, oldCasbin })
	require.PanicsWithValue(t, "OpenAPI initialization failed; ingress remains closed", func() { InitRoutes(gin.New()) })
	state, err := openapiconfig.NewMustAuthStore(db, nil).Load(context.Background())
	require.NoError(t, err)
	require.False(t, state.MustAuthLocked, "failed preflight must not commit the one-way latch")
}

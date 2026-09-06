package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.cn/uvp-gb28181/app/global/app"
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

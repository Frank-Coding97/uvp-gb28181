package controllers

import (
	"testing"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type deviceSQLiteConfig struct{ app.YmlConfigInterf }

func (deviceSQLiteConfig) GetString(string) string { return "sqlite" }

func TestDeviceControllersUseConfiguredSQLite(t *testing.T) {
	oldConfig, oldSQLite, oldMySQL := app.ConfigYml, app.GormDbSQLite, app.GormDbMysql
	t.Cleanup(func() {
		app.ConfigYml, app.GormDbSQLite, app.GormDbMysql = oldConfig, oldSQLite, oldMySQL
	})
	db := &gorm.DB{}
	app.ConfigYml, app.GormDbSQLite, app.GormDbMysql = deviceSQLiteConfig{}, db, nil
	for name, provider := range map[string]func() *gorm.DB{
		"devices": NewDeviceMgmtController().db,
		"catalog": NewCatalogTreeController().db,
		"map": NewMapController().db,
	} {
		if provider() != db {
			t.Errorf("%s did not select configured SQLite", name)
		}
	}
}

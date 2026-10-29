package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
)

type sysJobsModelTestConfig struct {
	app.YmlConfigInterf
}

func (sysJobsModelTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "sqlite"
	}
	return ""
}

func TestSysJobsGetByIDFindsNonNumericTaskID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sysjobs-get-by-id?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&models.SysJobs{}))

	oldDB, oldConfig := app.GormDbSqlite, app.ConfigYml
	app.GormDbSqlite = db
	app.ConfigYml = sysJobsModelTestConfig{}
	t.Cleanup(func() {
		app.GormDbSqlite = oldDB
		app.ConfigYml = oldConfig
	})

	now := time.Now()
	want := models.SysJobs{
		Id: "system-log-cleanup-sip", Group: "system", Name: "SIP 日志清理",
		ExecutorName: "log-cleanup-executor", CronExpression: "0 0 3 * * *",
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&want).Error)

	var got models.SysJobs
	require.NoError(t, got.GetByID(context.Background(), want.Id))
	require.Equal(t, want.Id, got.Id)
}

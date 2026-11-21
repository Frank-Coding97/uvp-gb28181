package executors

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/service"
	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
)

// 保留期由配置决定（2026-10-19 起出厂默认 7 天），这里显式配成 180 天，
// 只为验证**截止时间的严格边界**：恰好等于 cutoff 的保留、早一秒的删掉。
// ⛔ 别把 180 写死在代码里当默认值 —— 那条已经被移除，写死会让用例和产品脱节。
func TestLoginLogCleanupExecutorUsesStrictCutoffBoundary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	now := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	cutoff := now.Add(-180 * 24 * time.Hour)
	rows := []models.SysLoginLog{
		{Username: "old", Result: service.LoginResultFailure, IP: "10.0.0.1", Location: "内网", UserAgent: "ua", Browser: "x", OS: "x", CreatedAt: cutoff.Add(-time.Second)},
		{Username: "boundary", Result: service.LoginResultFailure, IP: "10.0.0.1", Location: "内网", UserAgent: "ua", Browser: "x", OS: "x", CreatedAt: cutoff},
	}
	require.NoError(t, db.Create(&rows).Error)
	previous := app.ConfigYml
	app.ConfigYml = &cleanupExecutorConfig{values: map[string]interface{}{
		logcleanup.ConfiguredConfigKey: true, logcleanup.LoginRetentionDaysConfigKey: 180,
	}}
	t.Cleanup(func() { app.ConfigYml = previous })
	executor := &LoginLogCleanupExecutor{Service: service.NewLoginLogService(db), Now: func() time.Time { return now }}
	require.NoError(t, executor.Execute(context.Background(), nil))
	var remaining []models.SysLoginLog
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "boundary", remaining[0].Username)
}

func TestLegacyLoginCleanupUsesConfiguredRetention(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("log_cleanup:\n  login_retention_days: 3\n"), 0600))
	config, err := ymlconfig.LoadYamlFactory(dir)
	require.NoError(t, err)
	previous := app.ConfigYml
	app.ConfigYml = config
	t.Cleanup(func() { app.ConfigYml = previous })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	now := time.Now().UTC()
	require.NoError(t, db.Create(&models.SysLoginLog{Username: "old", CreatedAt: now.Add(-4 * 24 * time.Hour)}).Error)
	executor := &LoginLogCleanupExecutor{Service: service.NewLoginLogService(db), Now: func() time.Time { return now }}
	require.NoError(t, executor.Execute(context.Background(), nil))
	var count int64
	require.NoError(t, db.Unscoped().Model(&models.SysLoginLog{}).Count(&count).Error)
	require.Zero(t, count)

	release, err := logcleanup.Acquire(context.Background(), logcleanup.Login)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, executor.Execute(ctx, nil), context.Canceled)
}

package executors

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/schedulerhelper"
)

type cleanupExecutorConfig struct{ values map[string]interface{} }

func (c *cleanupExecutorConfig) ConfigFileChangeListen(...func()) {}
func (c *cleanupExecutorConfig) Get(key string) interface{}       { return c.values[key] }
func (c *cleanupExecutorConfig) GetString(string) string          { return "" }
func (c *cleanupExecutorConfig) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *cleanupExecutorConfig) GetInt(key string) int             { value, _ := c.values[key].(int); return value }
func (c *cleanupExecutorConfig) GetInt32(key string) int32         { return int32(c.GetInt(key)) }
func (c *cleanupExecutorConfig) GetInt64(key string) int64         { return int64(c.GetInt(key)) }
func (c *cleanupExecutorConfig) GetFloat64(string) float64         { return 0 }
func (c *cleanupExecutorConfig) GetDuration(string) time.Duration  { return 0 }
func (c *cleanupExecutorConfig) GetStringSlice(string) []string    { return nil }
func (c *cleanupExecutorConfig) GetUintSlice(string) []uint        { return nil }
func (c *cleanupExecutorConfig) Set(key string, value interface{}) { c.values[key] = value }
func (c *cleanupExecutorConfig) SaveConfig() error                 { return nil }

func TestLogCleanupExecutorUsesConfiguredRetentionAndStrictCutoff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}))
	now := time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	cutoff := now.Add(-3 * 24 * time.Hour)
	require.NoError(t, db.Create(&[]models.SysOperationLog{
		{Operation: "old", BaseModel: models.BaseModel{CreatedAt: cutoff.Add(-time.Second)}},
		{Operation: "boundary", BaseModel: models.BaseModel{CreatedAt: cutoff}},
	}).Error)
	previous := app.ConfigYml
	app.ConfigYml = &cleanupExecutorConfig{values: map[string]interface{}{
		logcleanup.ConfiguredConfigKey: true, logcleanup.OperationRetentionDaysConfigKey: 3,
	}}
	t.Cleanup(func() { app.ConfigYml = previous })

	executor := &LogCleanupExecutor{DB: db, Now: func() time.Time { return now }}
	job := &schedulerhelper.Job{Parameters: map[string]interface{}{"kind": string(logcleanup.Operation)}}
	require.NoError(t, executor.Execute(context.Background(), job))
	var remaining []models.SysOperationLog
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "boundary", remaining[0].Operation)
}

func TestLogCleanupExecutorSkipsNewTypesUntilFirstConfigurationSave(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysOperationLog{}))
	require.NoError(t, db.Create(&models.SysOperationLog{Operation: "old", BaseModel: models.BaseModel{CreatedAt: time.Now().Add(-365 * 24 * time.Hour)}}).Error)
	previous := app.ConfigYml
	app.ConfigYml = &cleanupExecutorConfig{values: map[string]interface{}{logcleanup.OperationRetentionDaysConfigKey: 180}}
	t.Cleanup(func() { app.ConfigYml = previous })

	executor := &LogCleanupExecutor{DB: db}
	job := &schedulerhelper.Job{Parameters: map[string]interface{}{"kind": string(logcleanup.Operation)}}
	require.NoError(t, executor.Execute(context.Background(), job))
	var count int64
	require.NoError(t, db.Model(&models.SysOperationLog{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestLogCleanupExecutorValidatesKindAndHonorsCanceledContext(t *testing.T) {
	executor := &LogCleanupExecutor{}
	require.Error(t, executor.Execute(context.Background(), &schedulerhelper.Job{Parameters: map[string]interface{}{"kind": "unknown"}}))
	release, err := logcleanup.Acquire(context.Background(), logcleanup.Operation)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = executor.Execute(ctx, &schedulerhelper.Job{Parameters: map[string]interface{}{"kind": string(logcleanup.Operation)}})
	require.ErrorIs(t, err, context.Canceled)
}

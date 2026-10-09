package scheduler

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/scheduler/executors"
	"uvplatform.com/uvp-gb28181/app/utils/schedulerhelper"
)

func TestLoginLogCleanupSchedulerRegistrationIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysJobs{}))
	oldScheduler, oldLog := app.JobScheduler, app.ZapLog
	t.Cleanup(func() { app.JobScheduler, app.ZapLog = oldScheduler, oldLog })
	app.JobScheduler = schedulerhelper.NewJobScheduler()
	app.ZapLog = zap.NewNop()
	RegisterExecutors()
	require.NoError(t, RegisterSystemJobs(db))
	require.NoError(t, db.Model(&models.SysJobs{}).
		Where("id = ?", "system-login-log-cleanup").
		Update("status", int(schedulerhelper.StatusDisabled)).Error)
	require.NoError(t, RegisterSystemJobs(db))
	jobs := app.JobScheduler.ListJobs()
	count := 0
	for _, job := range jobs {
		if job.ID == "system-login-log-cleanup" {
			count++
			require.Equal(t, "0 0 3 * * *", job.CronExpression)
			require.Equal(t, executors.LogCleanupExecutorName, job.ExecutorName)
			require.Equal(t, "login", job.Parameters["kind"])
			require.Equal(t, schedulerhelper.BlockDiscard, job.BlockingPolicy)
			require.Equal(t, schedulerhelper.StatusDisabled, job.Status)
		}
	}
	require.Equal(t, 1, count)
}

func TestLogCleanupJobsUseDailyScheduleAndRemainEnabledByDefault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysJobs{}))
	oldScheduler, oldLog := app.JobScheduler, app.ZapLog
	t.Cleanup(func() { app.JobScheduler, app.ZapLog = oldScheduler, oldLog })
	app.JobScheduler = schedulerhelper.NewJobScheduler()
	app.ZapLog = zap.NewNop()
	RegisterExecutors()
	require.NoError(t, RegisterSystemJobs(db))

	wantKinds := map[string]bool{"sip": true, "operation": true, "login": true, "job": true, "playback": true, "scheduler": true}
	for _, job := range app.JobScheduler.ListJobs() {
		if job.ExecutorName != executors.LogCleanupExecutorName {
			continue
		}
		kind, ok := job.Parameters["kind"].(string)
		require.True(t, ok)
		require.True(t, wantKinds[kind], "unexpected or duplicate cleanup kind %q", kind)
		delete(wantKinds, kind)
		require.Equal(t, schedulerhelper.StatusEnabled, job.Status)
		require.Equal(t, "0 0 3 * * *", job.CronExpression)
		require.Equal(t, schedulerhelper.BlockDiscard, job.BlockingPolicy)
	}
	require.Empty(t, wantKinds)
}

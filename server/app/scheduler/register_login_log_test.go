package scheduler

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
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
	require.NoError(t, RegisterSystemJobs(db))
	jobs := app.JobScheduler.ListJobs()
	count := 0
	for _, job := range jobs {
		if job.ID == "system-login-log-cleanup" {
			count++
			require.Equal(t, "0 0 3 * * *", job.CronExpression)
			require.Equal(t, executors.LoginLogCleanupExecutorName, job.ExecutorName)
			require.Equal(t, schedulerhelper.BlockDiscard, job.BlockingPolicy)
		}
	}
	require.Equal(t, 1, count)
}

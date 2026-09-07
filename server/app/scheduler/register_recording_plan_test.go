package scheduler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestRecordingPlanSystemJobsAreFixedAndIdempotent(t *testing.T) {
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
	want := map[string]struct{ executor, cron string }{
		"system-recording-plan-dispatch": {executors.RecordingPlanDispatchExecutorName, "*/5 * * * * *"},
		"system-recording-plan-heal":     {executors.RecordingPlanHealExecutorName, "0 * * * * *"},
	}
	counts := map[string]int{}
	for _, job := range app.JobScheduler.ListJobs() {
		expected, ok := want[job.ID]
		if !ok {
			continue
		}
		counts[job.ID]++
		require.Equal(t, expected.executor, job.ExecutorName)
		require.Equal(t, expected.cron, job.CronExpression)
		require.Equal(t, schedulerhelper.BlockDiscard, job.BlockingPolicy)
	}
	require.Equal(t, 1, counts["system-recording-plan-dispatch"])
	require.Equal(t, 1, counts["system-recording-plan-heal"])
}

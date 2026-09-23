package scheduler

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestRegisterSystemJobsPersistsParentsBeforeScheduling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysJobs{}))

	oldScheduler, oldLog := app.JobScheduler, app.ZapLog
	t.Cleanup(func() { app.JobScheduler, app.ZapLog = oldScheduler, oldLog })
	app.JobScheduler = schedulerhelper.NewJobScheduler()
	app.ZapLog = zap.NewNop()

	RegisterExecutors()
	require.NoError(t, RegisterSystemJobs(db))

	var persisted []models.SysJobs
	require.NoError(t, db.Order("id").Find(&persisted).Error)
	require.Len(t, persisted, 4)

	for _, job := range app.JobScheduler.ListJobs() {
		var count int64
		require.NoError(t, db.Model(&models.SysJobs{}).Where("id = ?", job.ID).Count(&count).Error)
		require.Equalf(t, int64(1), count, "任务 %s 必须先存在于 sys_jobs", job.ID)
	}
}

func TestRegisterSystemJobsIsIdempotentAndPreservesDisabledStatus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysJobs{}))

	oldScheduler, oldLog := app.JobScheduler, app.ZapLog
	t.Cleanup(func() { app.JobScheduler, app.ZapLog = oldScheduler, oldLog })
	app.JobScheduler = schedulerhelper.NewJobScheduler()
	app.ZapLog = zap.NewNop()

	require.NoError(t, RegisterSystemJobs(db))
	require.NoError(t, db.Model(&models.SysJobs{}).
		Where("id = ?", "system-recording-plan-dispatch").
		Update("status", int(schedulerhelper.StatusDisabled)).Error)
	require.NoError(t, RegisterSystemJobs(db))

	var count int64
	require.NoError(t, db.Model(&models.SysJobs{}).Count(&count).Error)
	require.Equal(t, int64(4), count)

	var registered *schedulerhelper.Job
	for _, job := range app.JobScheduler.ListJobs() {
		if job.ID == "system-recording-plan-dispatch" {
			registered = job
			break
		}
	}
	require.NotNil(t, registered)
	require.Equal(t, schedulerhelper.StatusDisabled, registered.Status)
}

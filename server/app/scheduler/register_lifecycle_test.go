package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

type loggingRegisterTestConfig struct{}

func (loggingRegisterTestConfig) ConfigFileChangeListen(...func()) {}
func (loggingRegisterTestConfig) Get(string) interface{}           { return nil }
func (loggingRegisterTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (loggingRegisterTestConfig) GetBool(string) bool              { return false }
func (loggingRegisterTestConfig) GetInt(string) int                { return 0 }
func (loggingRegisterTestConfig) GetInt32(string) int32            { return 0 }
func (loggingRegisterTestConfig) GetInt64(string) int64            { return 0 }
func (loggingRegisterTestConfig) GetFloat64(string) float64        { return 0 }
func (loggingRegisterTestConfig) GetDuration(string) time.Duration { return 0 }
func (loggingRegisterTestConfig) GetStringSlice(string) []string   { return nil }
func (loggingRegisterTestConfig) GetUintSlice(string) []uint       { return nil }
func (loggingRegisterTestConfig) Set(string, interface{})          {}
func (loggingRegisterTestConfig) SaveConfig() error                { return nil }

type loggingRegisterExecutor struct{}

func (loggingRegisterExecutor) Name() string { return "logging-register-test" }
func (loggingRegisterExecutor) Execute(context.Context, *schedulerhelper.Job) error {
	return nil
}

func TestLoggingRegisterStartsResultHandlerBeforeLoadFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	// Keep sys_jobs absent so LoadJobsFromDB takes its real query-error path.
	require.NoError(t, db.AutoMigrate(&models.SysJobResults{}))

	scheduler := schedulerhelper.NewJobScheduler(
		schedulerhelper.WithLogger(schedulerhelper.NewZapJobLogger(zap.NewNop())),
		schedulerhelper.WithJobResultsBufferSize(1),
	)
	scheduler.RegisterExecutor(loggingRegisterExecutor{})

	oldDB, oldConfig, oldScheduler, oldLog := app.GormDbMysql, app.ConfigYml, app.JobScheduler, app.ZapLog
	app.GormDbMysql = db
	app.ConfigYml = loggingRegisterTestConfig{}
	app.JobScheduler = scheduler
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() {
		_ = scheduler.StopContext(context.Background())
		_ = StopResultHandlerContext(context.Background())
		app.GormDbMysql, app.ConfigYml, app.JobScheduler, app.ZapLog = oldDB, oldConfig, oldScheduler, oldLog
	})

	LoadJobsFromDB()

	const jobID = "logging-register-after-load-failure"
	_, err = scheduler.AddOrUpdateJob(&schedulerhelper.Job{
		ID:              jobID,
		Group:           "logging",
		Name:            "result consumer check",
		ExecutorName:    loggingRegisterExecutor{}.Name(),
		ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status:          schedulerhelper.StatusDisabled,
		CronExpression:  "0 0 1 * * *",
		Timeout:         time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, scheduler.ExecuteNow(jobID))
	require.NoError(t, scheduler.StopContext(context.Background()))
	require.NoError(t, StopResultHandlerContext(context.Background()))

	var saved int64
	require.NoError(t, db.Model(&models.SysJobResults{}).Where("job_id = ?", jobID).Count(&saved).Error)
	require.Equal(t, int64(1), saved)
}

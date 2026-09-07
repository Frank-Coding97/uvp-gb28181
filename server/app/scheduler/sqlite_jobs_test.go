package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type sqliteSchedulerTestConfig struct {
	strings map[string]string
	uints   map[string][]uint
}

func (c sqliteSchedulerTestConfig) ConfigFileChangeListen(...func()) {}
func (c sqliteSchedulerTestConfig) Get(key string) interface{} {
	return c.strings[key]
}
func (c sqliteSchedulerTestConfig) GetString(key string) string { return c.strings[key] }
func (sqliteSchedulerTestConfig) GetBool(string) bool           { return false }
func (sqliteSchedulerTestConfig) GetInt(string) int             { return 0 }
func (sqliteSchedulerTestConfig) GetInt32(string) int32         { return 0 }
func (sqliteSchedulerTestConfig) GetInt64(string) int64         { return 0 }
func (sqliteSchedulerTestConfig) GetFloat64(string) float64     { return 0 }
func (sqliteSchedulerTestConfig) GetDuration(string) time.Duration {
	return 0
}
func (sqliteSchedulerTestConfig) GetStringSlice(string) []string { return nil }
func (c sqliteSchedulerTestConfig) GetUintSlice(key string) []uint {
	return c.uints[key]
}
func (sqliteSchedulerTestConfig) Set(string, interface{}) {}
func (sqliteSchedulerTestConfig) SaveConfig() error       { return nil }

func newSQLiteSchedulerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldConfig, oldLog := app.GormDbSQLite, app.ConfigYml, app.ZapLog
	app.ConfigYml = sqliteSchedulerTestConfig{
		strings: map[string]string{
			"gormv2.usedbtype":    "sqlite",
			"casbin.tableprefix":  "",
			"casbin.tablename":    "sys_casbin_rule",
			"server.notcheckuser": "",
		},
	}
	app.ZapLog = zap.NewNop()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "scheduler.db"))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	require.NoError(t, sqlitebootstrap.Migrate(ctx, db))
	app.GormDbSQLite = db
	t.Cleanup(func() {
		raw, dbErr := db.DB()
		if dbErr == nil {
			_ = raw.Close()
		}
		app.GormDbSQLite, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog
	})
	return db
}

type sqliteJobTestExecutor struct {
	called chan *schedulerhelper.Job
}

func (e *sqliteJobTestExecutor) Name() string { return "sqlite-t12-test" }

func (e *sqliteJobTestExecutor) Execute(ctx context.Context, job *schedulerhelper.Job) error {
	select {
	case e.called <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sqliteTestGinContext() *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	return c
}

func TestSQLiteSystemJobsServiceCRUDExecutionAndResults(t *testing.T) {
	db := newSQLiteSchedulerTestDB(t)
	oldScheduler := app.JobScheduler
	logger, err := schedulerhelper.NewFileJobLogger(filepath.Join(t.TempDir(), "scheduler-logs"), schedulerhelper.LevelError)
	require.NoError(t, err)
	jobScheduler := schedulerhelper.NewJobScheduler(schedulerhelper.WithLogger(logger))
	app.JobScheduler = jobScheduler
	t.Cleanup(func() {
		jobScheduler.Stop()
		app.JobScheduler = oldScheduler
	})

	executor := &sqliteJobTestExecutor{called: make(chan *schedulerhelper.Job, 1)}
	jobScheduler.RegisterExecutor(executor)
	ctx := sqliteTestGinContext()
	jobsService := service.NewSysJobsService()
	created, err := jobsService.Create(ctx, models.SysJobsCreateRequest{
		Group:           "sqlite-t12",
		Name:            "SQLite job before update",
		Description:     "full baseline scheduler probe",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: int(schedulerhelper.PolicyOnce),
		Status:          int(schedulerhelper.StatusEnabled),
		CronExpression:  "0 0 0 1 1 *",
		Parameters:      `{"probe":"sqlite","attempt":1}`,
		BlockingPolicy:  int(schedulerhelper.BlockDiscard),
		Timeout:         int64(time.Second),
	})
	require.NoError(t, err)
	require.NotEmpty(t, created.Id)
	require.True(t, jobScheduler.JobExists(created.Id))

	stored := models.NewSysJobs()
	require.NoError(t, stored.GetByID(ctx, created.Id))
	require.Equal(t, "SQLite job before update", stored.Name)
	require.Equal(t, `{"probe":"sqlite","attempt":1}`, stored.Parameters)

	group := "sqlite-t12"
	jobs, total, err := jobsService.List(ctx, models.SysJobsListRequest{
		Group:      &group,
		BasePaging: models.BasePaging{PageNum: 1, PageSize: 10},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, *jobs, 1)
	require.Equal(t, created.Id, (*jobs)[0].Id)

	require.NoError(t, jobsService.Update(ctx, models.SysJobsUpdateRequest{
		Id:              created.Id,
		Group:           group,
		Name:            "SQLite job after update",
		Description:     "updated",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: int(schedulerhelper.PolicyOnce),
		Status:          int(schedulerhelper.StatusEnabled),
		CronExpression:  "0 0 0 1 1 *",
		Parameters:      `{"probe":"sqlite-updated"}`,
		BlockingPolicy:  int(schedulerhelper.BlockDiscard),
		Timeout:         int64(time.Second),
	}))
	updated, err := jobsService.GetByID(ctx, created.Id)
	require.NoError(t, err)
	require.Equal(t, "SQLite job after update", updated.Name)
	require.Equal(t, `{"probe":"sqlite-updated"}`, updated.Parameters)

	require.NoError(t, jobsService.SetStatus(ctx, created.Id, int(schedulerhelper.StatusDisabled)))
	require.Equal(t, schedulerhelper.StatusDisabled, jobScheduler.ListJobs()[0].Status)
	require.NoError(t, jobsService.SetStatus(ctx, created.Id, int(schedulerhelper.StatusEnabled)))
	require.Equal(t, schedulerhelper.StatusEnabled, jobScheduler.ListJobs()[0].Status)

	require.NoError(t, jobsService.ExecuteNow(ctx, created.Id))
	select {
	case executed := <-executor.called:
		require.Equal(t, created.Id, executed.ID)
		require.Equal(t, "sqlite-updated", executed.Parameters["probe"])
	case <-time.After(5 * time.Second):
		t.Fatal("test executor was not called")
	}

	var result *schedulerhelper.JobResult
	select {
	case result = <-jobScheduler.GetResults():
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler result was not published")
	}
	require.Equal(t, created.Id, result.JobID)
	require.Equal(t, "SUCCESS", result.Status)
	require.NoError(t, result.Error)
	require.Equal(t, schedulerhelper.PolicyOnce, result.ExecutionPolicy)
	require.Eventually(t, func() bool {
		jobs := jobScheduler.ListJobs()
		return len(jobs) == 1 && jobs[0].Status == schedulerhelper.StatusDisabled
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, saveJobResult(result))
	var persistedJob models.SysJobs
	require.NoError(t, db.Unscoped().First(&persistedJob, "id = ?", created.Id).Error)
	require.Equal(t, int(schedulerhelper.StatusDisabled), persistedJob.Status)

	jobID := created.Id
	success := "SUCCESS"
	resultsService := service.NewSysJobResultsService()
	results, total, err := resultsService.List(ctx, models.SysJobResultsListRequest{
		JobId:      &jobID,
		Status:     &success,
		BasePaging: models.BasePaging{PageNum: 1, PageSize: 10},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, *results, 1)
	require.Equal(t, created.Id, (*results)[0].JobId)
	require.GreaterOrEqual(t, (*results)[0].Duration, int64(0))

	loadedResult, err := resultsService.GetByID(ctx, (*results)[0].Id)
	require.NoError(t, err)
	require.Equal(t, (*results)[0].Id, loadedResult.Id)
	require.NoError(t, resultsService.Delete(ctx, loadedResult.Id))
	deletedResult, err := resultsService.GetByID(ctx, loadedResult.Id)
	require.NoError(t, err)
	require.True(t, deletedResult.IsEmpty())

	require.NoError(t, jobsService.Delete(ctx, created.Id))
	deletedJob, err := jobsService.GetByID(ctx, created.Id)
	require.NoError(t, err)
	require.True(t, deletedJob.IsEmpty())
	require.False(t, jobScheduler.JobExists(created.Id))
}

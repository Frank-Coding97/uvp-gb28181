//go:build logging_acceptance

package loggingacceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

const (
	acceptanceRoutePrefix    = "/api/__logging_acceptance"
	acceptanceCronExpression = "0 0 0 1 1 *"
	maxAcceptanceBlockMS     = 5000
)

// Register is called from the tagged main package after bootstrap has created
// the logger, scheduler, and authentication services. Keeping registration out
// of this package's init avoids touching app.ZapLog before bootstrap.
func Register() {
	if app.JobScheduler != nil {
		app.JobScheduler.RegisterExecutor(&acceptanceExecutor{})
	}
	ginhelper.RegisterPluginRoutes(registerRoutes)
}

func registerRoutes(engine *gin.Engine) {
	acceptance := engine.Group(acceptanceRoutePrefix)
	// routes.InitRoutes owns the production OperationLogMiddleware and runs
	// before InitPluginRoutes. The acceptance harness enables server.syslog, so
	// adding it here would record every request twice.
	acceptance.Use(middleware.JWTAuthMiddleware())
	acceptance.Use(middleware.DemoAccountMiddleware())
	acceptance.Use(middleware.CasbinMiddleware())

	acceptance.GET("/ok", handleOK)
	acceptance.POST("/business-failure", handleBusinessFailure)
	acceptance.GET("/slow-sql", handleSlowSQL)
	acceptance.GET("/panic", handlePanic)
	acceptance.GET("/block", handleBlock)
	acceptance.GET("/forbidden", handleForbidden)
	acceptance.POST("/scheduler", handleScheduler)
}

func handleOK(c *gin.Context) {
	acceptanceLog(c).Info("acceptance HTTP request completed", zap.String("event", "acceptance.http.completed"))
	response.Success(c, gin.H{"ok": true})
}

func handleBusinessFailure(c *gin.Context) {
	acceptanceLog(c).Info("acceptance HTTP business failure returned",
		zap.String("event", "acceptance.http.completed"), zap.Int("business_code", 42), zap.Bool("business_success", false))
	response.ReturnJson(c, http.StatusOK, 42, "business failure", gin.H{"ok": false})
}

func handleSlowSQL(c *gin.Context) {
	if acceptanceDB() == nil {
		acceptanceLog(c).Warn("acceptance slow SQL database unavailable",
			zap.String("event", "acceptance.http.slow_sql_failed"))
		response.Fail(c, "slow SQL database unavailable", http.StatusServiceUnavailable, 1, nil)
		return
	}

	err := app.DBContext(c.Request.Context()).Exec("SELECT SLEEP(?)", 1.1).Error
	if err != nil {
		acceptanceLog(c).Warn("acceptance slow SQL failed",
			zap.String("event", "acceptance.http.slow_sql_failed"), logging.Error(err))
		response.Fail(c, "slow SQL failed", http.StatusInternalServerError, 1, nil)
		return
	}
	acceptanceLog(c).Info("acceptance HTTP request completed", zap.String("event", "acceptance.http.completed"))
	response.Success(c, gin.H{"ok": true})
}

func handlePanic(c *gin.Context) {
	acceptanceLog(c).Error("acceptance panic fixture invoked", zap.String("event", "acceptance.http.panic"))
	panic("logging-acceptance-secret-panic")
}

func handleBlock(c *gin.Context) {
	ms, err := boundedMilliseconds(c.Query("ms"), maxAcceptanceBlockMS)
	if err != nil {
		response.Fail(c, "ms must be between 1 and 5000", http.StatusBadRequest, 1, nil)
		return
	}

	ctx := c.Request.Context()
	acceptanceLog(c).Info("acceptance HTTP request started",
		zap.String("event", "acceptance.http.started"), zap.Int("duration_ms", ms))
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		acceptanceLog(c).Info("acceptance HTTP request completed",
			zap.String("event", "acceptance.http.completed"), zap.Int("duration_ms", ms))
		response.Success(c, gin.H{"ms": ms})
	case <-ctx.Done():
		acceptanceLog(c).Warn("acceptance HTTP request canceled",
			zap.String("event", "acceptance.http.canceled"), logging.Error(ctx.Err()))
	}
}

func handleForbidden(c *gin.Context) {
	acceptanceLog(c).Info("acceptance forbidden fixture reached", zap.String("event", "acceptance.http.completed"))
	response.Success(c, gin.H{"ok": true})
}

type schedulerRequest struct {
	JobID      string `json:"job_id"`
	DurationMS int    `json:"duration_ms"`
}

func handleScheduler(c *gin.Context) {
	var request schedulerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Fail(c, "invalid scheduler request", http.StatusBadRequest, 1, nil)
		return
	}
	if !validAcceptanceJobID(request.JobID) {
		response.Fail(c, "job_id contains invalid characters", http.StatusBadRequest, 1, nil)
		return
	}
	if request.DurationMS < 1 || request.DurationMS > maxAcceptanceJobMS {
		response.Fail(c, "duration_ms must be between 1 and 10000", http.StatusBadRequest, 1, nil)
		return
	}
	if app.JobScheduler == nil {
		response.Fail(c, "scheduler unavailable", http.StatusServiceUnavailable, 1, nil)
		return
	}
	db := acceptanceDB()
	if db == nil {
		response.Fail(c, "scheduler database unavailable", http.StatusServiceUnavailable, 1, nil)
		return
	}

	job := &schedulerhelper.Job{
		ID:              request.JobID,
		Group:           "acceptance",
		Name:            "logging acceptance",
		Description:     "logging acceptance fixture",
		ExecutorName:    acceptanceExecutorName,
		ExecutionPolicy: schedulerhelper.PolicyOnce,
		Status:          schedulerhelper.StatusEnabled,
		CronExpression:  acceptanceCronExpression,
		Parameters:      map[string]interface{}{"duration_ms": request.DurationMS},
		BlockingPolicy:  schedulerhelper.BlockDiscard,
		Timeout:         time.Duration(request.DurationMS+5000) * time.Millisecond,
		MaxRetry:        0,
		RetryInterval:   0,
		ParallelNum:     0,
	}
	if err := persistAcceptanceJob(c.Request.Context(), db, job); err != nil {
		acceptanceLog(c).Error("acceptance scheduler job persistence failed",
			zap.String("event", "acceptance.scheduler.persist_failed"), logging.Error(err))
		response.Fail(c, "scheduler job persistence failed", http.StatusInternalServerError, 1, nil)
		return
	}
	if _, err := app.JobScheduler.AddOrUpdateJob(job); err != nil {
		acceptanceLog(c).Error("acceptance scheduler job registration failed",
			zap.String("event", "acceptance.scheduler.register_failed"), logging.Error(err))
		response.Fail(c, "scheduler job registration failed", http.StatusInternalServerError, 1, nil)
		return
	}
	if err := app.JobScheduler.ExecuteNow(request.JobID); err != nil {
		acceptanceLog(c).Error("acceptance scheduler job execution failed",
			zap.String("event", "acceptance.scheduler.execute_failed"), logging.Error(err))
		response.Fail(c, "scheduler job execution failed", http.StatusInternalServerError, 1, nil)
		return
	}
	acceptanceLog(c).Info("acceptance scheduler job accepted",
		zap.String("event", "acceptance.scheduler.accepted"), zap.Int("duration_ms", request.DurationMS))
	response.Success(c, gin.H{"job_id": request.JobID})
}

func persistAcceptanceJob(ctx context.Context, db *gorm.DB, job *schedulerhelper.Job) error {
	parameters, err := json.Marshal(job.Parameters)
	if err != nil {
		return fmt.Errorf("marshal acceptance job parameters: %w", err)
	}
	now := time.Now()
	record := &models.SysJobs{
		Id:              job.ID,
		Group:           job.Group,
		Name:            job.Name,
		Description:     job.Description,
		ExecutorName:    job.ExecutorName,
		ExecutionPolicy: int(job.ExecutionPolicy),
		Status:          int(job.Status),
		CronExpression:  job.CronExpression,
		Parameters:      string(parameters),
		BlockingPolicy:  int(job.BlockingPolicy),
		Timeout:         int64(job.Timeout),
		MaxRetry:        job.MaxRetry,
		RetryInterval:   int64(job.RetryInterval),
		ParallelNum:     job.ParallelNum,
		CreatedAt:       &now,
		UpdatedAt:       &now,
	}
	definitionColumns := []string{
		"group", "name", "description", "executor_name", "execution_policy",
		"status", "cron_expression", "parameters", "blocking_policy", "timeout",
		"max_retry", "retry_interval", "parallel_num", "updated_at", "deleted_at",
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns(definitionColumns),
	}).Create(record).Error
}

// acceptanceExecutorName, acceptanceDuration and validAcceptanceJobID live in
// acceptance_executor.go without a build tag, so `go test ./...` covers them.

func boundedMilliseconds(raw string, max int) (int, error) {
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 1 || ms > max {
		return 0, fmt.Errorf("milliseconds out of range")
	}
	return ms, nil
}

func acceptanceDB() *gorm.DB {
	if app.ConfigYml == nil {
		return nil
	}
	switch app.ConfigYml.GetString("gormv2.usedbtype") {
	case consts.DbTypeSqlServer:
		return app.GormDbSqlserver
	case consts.DbTypePostgreSql:
		return app.GormDbPostgreSql
	case consts.DbTypeMySql, "":
		return app.GormDbMysql
	default:
		return app.GormDbMysql
	}
}

func acceptanceLog(c *gin.Context) *zap.Logger {
	if c == nil || c.Request == nil {
		return app.Log(context.Background()).Named("acceptance")
	}
	return app.Log(c.Request.Context()).Named("acceptance")
}

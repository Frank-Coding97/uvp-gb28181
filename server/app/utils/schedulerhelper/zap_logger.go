package schedulerhelper

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

// ZapJobLogger adapts scheduler events to the application's shared Zap
// logger. It does not own the supplied logger or its sinks.
type ZapJobLogger struct {
	logger *zap.Logger
}

var _ JobLogger = (*ZapJobLogger)(nil)

// NewZapJobLogger creates a scheduler logger backed by a shared Zap logger.
func NewZapJobLogger(logger *zap.Logger) *ZapJobLogger {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ZapJobLogger{logger: logger}
}

func (l *ZapJobLogger) emit(level zapcore.Level, event, message, jobID, executionID string, fields ...zap.Field) {
	if l == nil || l.logger == nil {
		return
	}
	logger := l.logger.Named("scheduler")
	if executionID != "" {
		logger = logging.WithIdentity(logger, zap.String("execution_id", executionID))
	}
	fields = append([]zap.Field{zap.String("event", event)}, fields...)
	if jobID != "" {
		fields = append(fields, zap.String("job_id", jobID))
	}
	switch level {
	case zap.DebugLevel:
		logger.Debug(message, fields...)
	case zap.InfoLevel:
		logger.Info(message, fields...)
	case zap.WarnLevel:
		logger.Warn(message, fields...)
	default:
		logger.Error(message, fields...)
	}
}

func (l *ZapJobLogger) executionScope(jobID, executionID string, attempt int, executorName string) *zap.Logger {
	if l == nil || l.logger == nil {
		return zap.NewNop()
	}
	scope := logging.WithIdentity(l.logger, zap.String("execution_id", executionID))
	return scope.With(
		zap.String("job_id", jobID),
		zap.Int("attempt", attempt),
		zap.String("executor", executorName),
	)
}

func safeErrorField(args []interface{}) (zap.Field, bool) {
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			return logging.Error(err), true
		}
	}
	return zap.Field{}, false
}

// The format and arguments are intentionally not rendered. Callers that need
// structured details use the methods below; this keeps legacy free-form error
// text out of the shared logger.
func (l *ZapJobLogger) Debug(jobID, _ string, _ ...interface{}) {
	l.emit(zap.DebugLevel, "scheduler.debug", "scheduler debug event", jobID, "")
}

func (l *ZapJobLogger) Info(jobID, _ string, _ ...interface{}) {
	l.emit(zap.InfoLevel, "scheduler.info", "scheduler info event", jobID, "")
}

func (l *ZapJobLogger) Warn(jobID, _ string, args ...interface{}) {
	if field, ok := safeErrorField(args); ok {
		l.emit(zap.WarnLevel, "scheduler.warn", "scheduler warning", jobID, "", field)
		return
	}
	l.emit(zap.WarnLevel, "scheduler.warn", "scheduler warning", jobID, "")
}

func (l *ZapJobLogger) Error(jobID, _ string, args ...interface{}) {
	if field, ok := safeErrorField(args); ok {
		l.emit(zap.ErrorLevel, "scheduler.error", "scheduler error", jobID, "", field)
		return
	}
	l.emit(zap.ErrorLevel, "scheduler.error", "scheduler error", jobID, "", zap.String("error_class", "unknown"))
}

func (l *ZapJobLogger) Fatal(jobID, _ string, args ...interface{}) {
	if field, ok := safeErrorField(args); ok {
		l.emit(zap.ErrorLevel, "scheduler.fatal", "scheduler fatal event", jobID, "", field)
		return
	}
	l.emit(zap.ErrorLevel, "scheduler.fatal", "scheduler fatal event", jobID, "")
}

// LogJobExecution records only the terminal scheduler result. A successful
// generic execution is DEBUG; recording-plan business actions are logged by
// their engine at INFO.
func (l *ZapJobLogger) LogJobExecution(result *JobResult) {
	if result == nil {
		l.emit(zap.ErrorLevel, "scheduler.execution.invalid", "scheduler execution result missing", "", "")
		return
	}
	level := zap.DebugLevel
	message := "scheduler execution completed"
	if result.Status != "SUCCESS" {
		level = zap.ErrorLevel
		message = "scheduler execution failed"
	}
	fields := []zap.Field{
		zap.String("status", result.Status),
		zap.Int("attempt", result.Attempt),
		zap.Int("retry_count", result.RetryCount),
		zap.Int64("duration_ms", result.Duration.Milliseconds()),
		zap.Int("execution_policy", int(result.ExecutionPolicy)),
	}
	if result.Error != nil {
		fields = append(fields, logging.Error(result.Error))
	}
	l.emit(level, "scheduler.execution.completed", message, result.JobID, result.ExecutionID, fields...)
}

func (l *ZapJobLogger) LogJobLifecycle(job *Job, action string) {
	if job == nil {
		l.emit(zap.ErrorLevel, "scheduler.job.lifecycle.invalid", "scheduler job lifecycle missing", "", "")
		return
	}
	level := zap.DebugLevel
	switch action {
	case "启用", "禁用", "删除", "手动触发":
		level = zap.InfoLevel
	}
	fields := []zap.Field{
		zap.String("action", action),
		zap.String("job_name", job.Name),
		zap.String("group", job.Group),
		zap.String("executor", job.ExecutorName),
		zap.String("status", getJobStatusName(job.Status)),
		zap.String("execution_policy", getExecutionPolicyName(job.ExecutionPolicy)),
		zap.String("blocking_policy", getBlockingPolicyName(job.BlockingPolicy)),
		zap.String("cron", job.CronExpression),
	}
	l.emit(level, "scheduler.job.lifecycle", "scheduler job lifecycle", job.ID, "", fields...)
}

func (l *ZapJobLogger) logExecutionStart(jobID, executionID string, attempt int) {
	l.emit(zap.DebugLevel, "scheduler.execution.started", "scheduler execution started", jobID, executionID, zap.Int("attempt", attempt))
}

func (l *ZapJobLogger) logExecutionRetry(jobID, executionID string, attempt int, interval time.Duration) {
	l.emit(zap.InfoLevel, "scheduler.execution.retry", "scheduler execution retry scheduled", jobID, executionID,
		zap.Int("attempt", attempt), zap.Int64("wait_ms", interval.Milliseconds()))
}

func (l *ZapJobLogger) logExecutionFailure(jobID, executionID string, attempt int, err error) {
	l.emit(zap.WarnLevel, "scheduler.execution.attempt_failed", "scheduler execution attempt failed", jobID, executionID,
		zap.Int("attempt", attempt), logging.Error(err))
}

func (l *ZapJobLogger) logExecutionResult(result *JobResult) {
	l.LogJobExecution(result)
}

func (l *ZapJobLogger) Close() error { return nil }

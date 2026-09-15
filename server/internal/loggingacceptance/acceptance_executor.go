package loggingacceptance

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

// This file deliberately carries no build tag.
//
// The acceptance package has two halves. The half that starts a real backend
// process needs an isolated MySQL and stays behind `UVP_LOGGING_MYSQLD` plus the
// `logging_acceptance` build tag (see acceptance_test.go and
// routes_logging_acceptance.go). The half below is a pure component: a
// scheduler executor, its parameter validation, and its two static events.
//
// It used to live behind `//go:build logging_acceptance` as well, which meant
// `go test ./...` - the only thing CI runs - never executed it. A test nobody
// runs is not an acceptance test. See C09.5 in
// docs/logging-governance/content-plan.md.

const (
	acceptanceExecutorName = "logging-acceptance"
	// maxAcceptanceJobMS bounds the scheduled fixture job.
	maxAcceptanceJobMS = 10000
)

// acceptanceExecutor is the scheduler job body the acceptance routes submit.
// It is only ever registered by Register(), which is build-tagged, so this type
// compiles into release binaries without ever being reachable there.
type acceptanceExecutor struct{}

func (e *acceptanceExecutor) Name() string { return acceptanceExecutorName }

func (e *acceptanceExecutor) Execute(ctx context.Context, job *schedulerhelper.Job) error {
	if job == nil {
		return fmt.Errorf("acceptance job is nil")
	}
	durationMS, err := acceptanceDuration(job.Parameters)
	if err != nil {
		return err
	}
	logger := app.Log(ctx).Named("scheduler.acceptance")
	logger.Info("acceptance scheduler job started",
		zap.String("event", "scheduler.acceptance.started"), zap.Int("duration_ms", durationMS))
	timer := time.NewTimer(time.Duration(durationMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		logger.Info("acceptance scheduler job completed",
			zap.String("event", "scheduler.acceptance.completed"), zap.Int("duration_ms", durationMS))
		return nil
	case <-ctx.Done():
		logger.Warn("acceptance scheduler job canceled",
			zap.String("event", "scheduler.acceptance.canceled"), logging.Error(ctx.Err()))
		return ctx.Err()
	}
}

func acceptanceDuration(parameters map[string]interface{}) (int, error) {
	value, ok := parameters["duration_ms"]
	if !ok {
		return 0, fmt.Errorf("duration_ms is required")
	}
	var durationMS int
	switch value := value.(type) {
	case int:
		durationMS = value
	case int8:
		durationMS = int(value)
	case int16:
		durationMS = int(value)
	case int32:
		durationMS = int(value)
	case int64:
		durationMS = int(value)
	case float64:
		if value != float64(int(value)) {
			return 0, fmt.Errorf("duration_ms must be an integer")
		}
		durationMS = int(value)
	default:
		return 0, fmt.Errorf("duration_ms must be an integer")
	}
	if durationMS < 1 || durationMS > maxAcceptanceJobMS {
		return 0, fmt.Errorf("duration_ms must be between 1 and 10000")
	}
	return durationMS, nil
}

func validAcceptanceJobID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range []byte(value) {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

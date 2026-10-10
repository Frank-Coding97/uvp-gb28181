package schedulerhelper

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type loggingPolicyCall struct {
	jobID string
	args  []interface{}
}

type loggingPolicyLogger struct {
	mu        sync.Mutex
	closeErr  error
	errorCall []loggingPolicyCall
}

func (l *loggingPolicyLogger) Debug(string, string, ...interface{}) {}
func (l *loggingPolicyLogger) Info(string, string, ...interface{})  {}
func (l *loggingPolicyLogger) Warn(string, string, ...interface{})  {}
func (l *loggingPolicyLogger) Error(jobID, _ string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorCall = append(l.errorCall, loggingPolicyCall{jobID: jobID, args: append([]interface{}(nil), args...)})
}
func (l *loggingPolicyLogger) Fatal(string, string, ...interface{}) {}
func (l *loggingPolicyLogger) LogJobExecution(*JobResult)           {}
func (l *loggingPolicyLogger) LogJobLifecycle(*Job, string)         {}
func (l *loggingPolicyLogger) Close() error                         { return l.closeErr }

func TestLoggingSchedulerDefaultUsesNoopZapLogger(t *testing.T) {
	scheduler := NewJobScheduler()
	t.Cleanup(scheduler.Stop)

	logger, ok := scheduler.logger.(*ZapJobLogger)
	require.True(t, ok, "default scheduler logger must be ZapJobLogger")
	require.NotNil(t, logger)
	require.NotNil(t, logger.logger)
	require.Nil(t, logger.logger.Check(zap.InfoLevel, "default scheduler logger must be silent"))
}

func TestLoggingSchedulerExplicitWithLoggerKeepsSharedRootUsable(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	root := zap.New(core)
	scheduler := NewJobScheduler(WithLogger(NewZapJobLogger(root)))

	require.NoError(t, scheduler.StopContext(context.Background()))
	before := observed.Len()
	root.Info("other component", zap.String("event", "other.event"))

	require.Equal(t, before+1, observed.Len())
	require.Equal(t, "other.event", observed.All()[before].ContextMap()["event"])
}

func TestLoggingSchedulerWithLoggerConfigRemainsExplicitFileOptIn(t *testing.T) {
	logDir := t.TempDir()
	scheduler := NewJobScheduler(WithLoggerConfig(logDir, LevelInfo))

	logger, ok := scheduler.logger.(*FileJobLogger)
	require.True(t, ok, "WithLoggerConfig must retain the standalone file logger")
	require.Equal(t, logDir, logger.logDir)
	require.NoError(t, scheduler.StopContext(context.Background()))
}

func TestLoggingSchedulerStopRoutesCloseErrorThroughLogger(t *testing.T) {
	wantErr := errors.New("scheduler close failure")
	logger := &loggingPolicyLogger{closeErr: wantErr}
	scheduler := NewJobScheduler(WithLogger(logger))

	scheduler.Stop()

	logger.mu.Lock()
	defer logger.mu.Unlock()
	require.Len(t, logger.errorCall, 1)
	require.Equal(t, "system", logger.errorCall[0].jobID)
	require.Len(t, logger.errorCall[0].args, 1)
	gotErr, ok := logger.errorCall[0].args[0].(error)
	require.True(t, ok)
	require.ErrorIs(t, gotErr, wantErr)
}

var _ JobLogger = (*loggingPolicyLogger)(nil)

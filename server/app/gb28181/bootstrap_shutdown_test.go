package gb28181

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type bootstrapQuiescingServer struct {
	fakeSIPRuntimeServer
	quiesceStarted chan struct{}
	quiesceRelease chan struct{}
	quiesceOnce    sync.Once
	quiesceCount   int
	shutdownCount  int
	shutdownErr    error
}

func (s *bootstrapQuiescingServer) QuiesceRequests(context.Context) error {
	s.quiesceOnce.Do(func() { close(s.quiesceStarted) })
	s.quiesceCount++
	<-s.quiesceRelease
	if s.events != nil {
		*s.events = append(*s.events, "sip.quiesce")
	}
	return nil
}

func (s *bootstrapQuiescingServer) Shutdown(ctx context.Context) error {
	s.shutdownCount++
	if err := s.fakeSIPRuntimeServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.shutdownErr
}

func resetBootstrapShutdownState() {
	activeShutdownRun = nil
	lastShutdownGeneration = nil
	stopping = false
	clearSIPShutdownGlobals()
	clearControlPlaneGlobals()
}

func TestLoggingBootstrapShutdownQuiescesBeforeDependencies(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	events := []string{}
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	close(server.quiesceRelease)
	sipServer = server

	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, []string{"sip.quiesce", "record.sink.clear", "sip.shutdown"}, events)
	require.Equal(t, 1, server.quiesceCount)
	require.Equal(t, 1, server.shutdownCount)

	// The completed generation is joined rather than executed a second time.
	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, 1, server.quiesceCount)
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapQuiesceGatesNewSIPGeneration(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	close(server.quiesceRelease)
	sipServer = server

	require.NoError(t, QuiesceRequests(context.Background()))
	require.True(t, stopping)
	require.Equal(t, 1, server.quiesceCount)
	require.EqualError(t, func() error {
		_, err := reloadSIPGeneration(context.Background())
		return err
	}(), "gb28181 正在停止,拒绝 SIP 热重载")
	require.Equal(t, 0, server.shutdownCount)

	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, 1, server.quiesceCount)
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapShutdownTimeoutRetainsGeneration(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	events := []string{}
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	sipServer = server

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	err := StopContext(ctx)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotNil(t, activeShutdownRun)
	require.Equal(t, 0, server.shutdownCount)
	require.Empty(t, events)

	close(server.quiesceRelease)
	if err := StopContext(context.Background()); err != nil {
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	require.Equal(t, []string{"sip.quiesce", "record.sink.clear", "sip.shutdown"}, events)
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapQuiesceTimeoutDoesNotReleaseDependencies(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	events := []string{}
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	sipServer = server

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	err := QuiesceRequests(ctx)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, events)

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	err = QuiesceRequests(secondCtx)
	secondCancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, events)

	close(server.quiesceRelease)
	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, []string{"sip.quiesce", "record.sink.clear", "sip.shutdown"}, events)
	require.Equal(t, 1, server.quiesceCount)
}

func TestLoggingBootstrapShutdownJoinsSIPFailure(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	sipErr := errors.New("sip transport stopped incompletely")
	events := []string{}
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
		shutdownErr:          sipErr,
	}
	close(server.quiesceRelease)
	sipServer = server

	err := StopContext(context.Background())
	require.ErrorIs(t, err, sipErr)
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapShutdownUpgradedReloadStopsControlPlaneOnce(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	cancelled := make(chan struct{})
	heartbeatDone := make(chan struct{})
	close(heartbeatDone)
	run := &bootstrapShutdownRun{
		control: controlPlaneShutdownSnapshot{
			heartbeatCancel: func() { close(cancelled) },
			heartbeatDone:   heartbeatDone,
		},
	}
	run.generation = newShutdownGeneration(context.Background(), nil, nil)
	run.process.Store(true)
	run.bindControlGeneration(context.Background())
	require.NoError(t, run.generation.Wait(context.Background()))
	require.NoError(t, run.controlGeneration.Wait(context.Background()))

	require.NoError(t, finalizeProcessShutdown(context.Background(), run, nil))
	require.NoError(t, finalizeProcessShutdown(context.Background(), run, nil))
	requireClosed(t, cancelled)
	require.True(t, shutdownDone(run.controlGeneration))
}

func TestLoggingBootstrapStopJoinsOneBlockingControlGeneration(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	release := make(chan struct{})
	started := make(chan struct{})
	var closeCount atomic.Int32
	run := &bootstrapShutdownRun{}
	run.generation = newShutdownGeneration(context.Background(), nil, nil)
	run.process.Store(true)
	run.controlGeneration = newShutdownGeneration(context.Background(), nil, []shutdownStep{{
		name: "control-plane",
		stop: func(context.Context) error {
			close(started)
			<-release
			closeCount.Add(1)
			return nil
		},
	}})
	sipLifecycleMu.Lock()
	activeShutdownRun = run
	sipLifecycleMu.Unlock()

	longResult := make(chan error, 1)
	go func() { longResult <- StopContext(context.Background()) }()
	<-started
	shortCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	shortErr := StopContext(shortCtx)
	cancel()
	require.ErrorIs(t, shortErr, context.DeadlineExceeded)
	require.Same(t, run, activeShutdownRun)

	close(release)
	require.NoError(t, <-longResult)
	require.Equal(t, int32(1), closeCount.Load())
	// A completed Stop joins the same result and does not execute control a
	// second time.
	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, int32(1), closeCount.Load())
}

func TestLoggingBootstrapControlWaitsForSIPBeforeMetricsPersistenceStop(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	metricsStopped := make(chan struct{})
	metricsPersistCancel = func() { close(metricsStopped) }
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	sipServer = server

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	require.ErrorIs(t, StopContext(ctx), context.DeadlineExceeded)
	cancel()
	select {
	case <-metricsStopped:
		t.Fatal("metrics persistence stopped before SIP generation completed")
	default:
	}

	close(server.quiesceRelease)
	if err := StopContext(context.Background()); err != nil {
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	requireClosed(t, metricsStopped)
}

func TestLoggingBootstrapReloadGenerationIsSharedUntilSuccessfulCompletion(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	sipServer = server

	run, err := reloadSIPGeneration(context.Background())
	require.NoError(t, err)
	require.NotNil(t, run)
	second, err := reloadSIPGeneration(context.Background())
	require.NoError(t, err)
	require.Same(t, run, second)
	require.Equal(t, 0, server.shutdownCount)

	close(server.quiesceRelease)
	require.NoError(t, run.generation.Wait(context.Background()))
	finishReloadGeneration(run, nil)
	require.Nil(t, activeShutdownRun)
	require.Nil(t, sipServer)
}

func TestLoggingBootstrapRunningReloadPromotesToProcessStop(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	sipServer = server

	run, err := reloadSIPGeneration(context.Background())
	require.NoError(t, err)
	stopResult := make(chan error, 1)
	go func() { stopResult <- StopContext(context.Background()) }()
	<-server.quiesceStarted
	require.True(t, run.process.Load())
	sipLifecycleMu.Lock()
	controlGeneration := run.controlGeneration
	sipLifecycleMu.Unlock()
	require.NotNil(t, controlGeneration)
	close(server.quiesceRelease)
	require.NoError(t, <-stopResult)
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapCompletedReloadStopDoesNotRecloseSIP(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
	}
	close(server.quiesceRelease)
	sipServer = server

	run, err := reloadSIPGeneration(context.Background())
	require.NoError(t, err)
	require.NoError(t, run.generation.Wait(context.Background()))
	finishReloadGeneration(run, nil)
	require.Nil(t, activeShutdownRun)
	require.Nil(t, sipServer)
	require.Equal(t, 1, server.shutdownCount)

	require.NoError(t, StopContext(context.Background()))
	require.Equal(t, 1, server.shutdownCount)
}

func TestLoggingBootstrapFailedReloadRemainsFailClosed(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	sipErr := errors.New("reload SIP stop failed")
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
		shutdownErr:          sipErr,
	}
	close(server.quiesceRelease)
	sipServer = server

	sipLifecycleMu.Lock()
	run := newBootstrapShutdownRun(context.Background(), captureSIPShutdownSnapshot(), captureControlPlaneShutdownSnapshot(), false)
	activeShutdownRun = run
	sipLifecycleMu.Unlock()
	require.ErrorIs(t, run.generation.Wait(context.Background()), sipErr)

	finishReloadGeneration(run, nil)
	require.Same(t, run, activeShutdownRun)
	require.Same(t, server, sipServer)
	_, err := reloadSIPGeneration(context.Background())
	require.ErrorIs(t, err, sipErr)
}

func TestLoggingBootstrapFailedReloadPromotesSameGenerationOnStop(t *testing.T) {
	resetBootstrapShutdownState()
	defer resetBootstrapShutdownState()
	var controlCloseCount atomic.Int32
	metricsPersistCancel = func() { controlCloseCount.Add(1) }
	sipErr := errors.New("reload SIP stop failed")
	server := &bootstrapQuiescingServer{
		fakeSIPRuntimeServer: fakeSIPRuntimeServer{},
		quiesceStarted:       make(chan struct{}),
		quiesceRelease:       make(chan struct{}),
		shutdownErr:          sipErr,
	}
	close(server.quiesceRelease)
	sipServer = server

	run, err := reloadSIPGeneration(context.Background())
	require.NoError(t, err)
	require.ErrorIs(t, run.generation.Wait(context.Background()), sipErr)
	finishReloadGeneration(run, nil)
	require.Same(t, run, activeShutdownRun)

	require.ErrorIs(t, StopContext(context.Background()), sipErr)
	require.Equal(t, 1, server.shutdownCount)
	require.Equal(t, int32(1), controlCloseCount.Load())
}

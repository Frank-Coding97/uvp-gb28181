package gb28181

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

type shutdownRootConfig struct{ app.YmlConfigInterf }

func (shutdownRootConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	if key == gbconfig.PlayAuthActiveKeyConfigKey {
		return strings.Repeat("fixture-key-", 4)
	}
	return ""
}
func (shutdownRootConfig) Get(string) interface{}         { return nil }
func (shutdownRootConfig) GetInt(string) int              { return 0 }
func (shutdownRootConfig) GetBool(key string) bool        { return key == "gb28181.enabled" }
func (shutdownRootConfig) GetStringSlice(string) []string { return nil }

func isolateSIPShutdownRoot(t *testing.T) {
	t.Helper()
	resetBootstrapShutdownState()
	previousServer, previousSecurity, previousStatus := sipServer, securityRuntime, sipRuntimeStatus
	previousLog := app.ZapLog
	previousConfig := app.ConfigYml
	previousMySQL, previousPG, previousMSSQL := app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver
	sipServer, securityRuntime, sipRuntimeStatus = nil, nil, gbsetup.NewRuntimeStatus()
	app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver = nil, nil, nil
	app.ZapLog = zap.NewNop()
	app.ConfigYml = shutdownRootConfig{}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "root.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	app.GormDbMysql = db // Only this isolated empty fixture; never default configuration.
	t.Cleanup(func() {
		sipServer, securityRuntime, sipRuntimeStatus = previousServer, previousSecurity, previousStatus
		app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver = previousMySQL, previousPG, previousMSSQL
		app.ZapLog = previousLog
		app.ConfigYml = previousConfig
		_ = raw.Close()
	})
}

func TestStopSIPDependenciesRetainsFailedServerAndSecurity(t *testing.T) {
	isolateSIPShutdownRoot(t)
	want := errors.New("fixture SIP drain unavailable")
	server := &fakeSIPRuntimeServer{shutdownErr: want}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
	sipServer, securityRuntime = server, runtime
	require.ErrorIs(t, stopSIPDependencies(context.Background()), want)
	require.Same(t, server, sipServer)
	require.Same(t, runtime, securityRuntime)
	called := 0
	err := startSIPDependenciesWithFactory(gbconfig.Config{}, nil, func(gbconfig.Config) (sipRuntimeServer, error) {
		called++
		return &fakeSIPRuntimeServer{}, nil
	})
	require.Error(t, err)
	require.Zero(t, called, "failed stop must not permit a replacement instance")
	server.shutdownErr = nil
	require.NoError(t, stopSIPDependencies(context.Background()))
	require.Nil(t, sipServer)
	require.Nil(t, securityRuntime)
}

func TestStartSIPDependenciesRetainsFailedRollback(t *testing.T) {
	for _, stage := range []string{"factory", "start", "assembly"} {
		t.Run(stage, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			isolateSIPShutdownRoot(t)
			authority := authoritytest.Register(t, app.DB(), "")
			failure, shutdownFailure := errors.New("fixture start failed"), errors.New("fixture rollback failed")
			server := &fakeSIPRuntimeServer{shutdownErr: shutdownFailure}
			if stage == "start" {
				server.startErr = failure
			}
			if stage == "assembly" {
				ua, err := sipgo.NewUA()
				require.NoError(t, err)
				defer ua.Close()
				server.uac, err = uac.New(ua, "34020000002000000001", "3402000000", "127.0.0.1", 5061, false)
				require.NoError(t, err)
			}
			err := startSIPDependenciesWithFactory(gbconfig.Config{}, authority, func(gbconfig.Config) (sipRuntimeServer, error) {
				if stage == "factory" {
					return server, failure
				}
				return server, nil
			})
			require.ErrorIs(t, err, shutdownFailure)
			if stage != "assembly" {
				require.ErrorIs(t, err, failure)
			}
			require.Same(t, server, sipServer)
			require.NotNil(t, securityRuntime)
			require.Equal(t, gbsetup.RuntimeFailed, sipRuntimeStatus.Snapshot().State)
			server.shutdownErr = nil
			require.NoError(t, stopSIPDependencies(context.Background()))
			require.Nil(t, sipServer)
			require.Nil(t, securityRuntime)
		})
	}
}

func TestStopFailurePreservesControlPlaneDependencies(t *testing.T) {
	isolateSIPShutdownRoot(t)
	want := errors.New("fixture persistent drain failed")
	sipServer = &fakeSIPRuntimeServer{shutdownErr: want}
	previousHeartbeat := heartbeatCancel
	defer func() { heartbeatCancel = previousHeartbeat }()
	called := false
	heartbeatCancel = func() { called = true }
	require.ErrorIs(t, Stop(), want)
	require.False(t, called, "control plane/DB dependencies must not be torn down after SIP failure")
}

func TestReloadSIPDoesNotReplaceServerAfterStopFailure(t *testing.T) {
	isolateSIPShutdownRoot(t)
	db := app.DB()
	require.NoError(t, db.AutoMigrate(&gbsetup.SIPConfig{}))
	require.NoError(t, db.Create(&gbsetup.SIPConfig{ID: 1, DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP: "127.0.0.1", AdvertiseIP: "127.0.0.1", Port: 5061, Domain: "3402000000", ServerID: "34020000002000000001"}).Error)
	want := errors.New("fixture reload drain failed")
	server := &fakeSIPRuntimeServer{shutdownErr: want}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
	sipServer, securityRuntime = server, runtime
	require.ErrorIs(t, ReloadSIP(nil), want)
	require.Same(t, server, sipServer)
	require.Same(t, runtime, securityRuntime)
	require.Equal(t, gbsetup.RuntimeFailed, sipRuntimeStatus.Snapshot().State)
}

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
	controlGeneration := waitForBootstrapControlGeneration(t, run, server.quiesceRelease, stopResult)
	require.NotNil(t, controlGeneration)
	close(server.quiesceRelease)
	require.NoError(t, <-stopResult)
	require.Equal(t, 1, server.shutdownCount)
}

func waitForBootstrapControlGeneration(t *testing.T, run *bootstrapShutdownRun, quiesceRelease chan struct{}, stopResult <-chan error) *shutdownGeneration {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		sipLifecycleMu.Lock()
		controlGeneration := run.controlGeneration
		process := run.process.Load()
		sipLifecycleMu.Unlock()
		if controlGeneration != nil {
			require.True(t, process)
			return controlGeneration
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			close(quiesceRelease)
			select {
			case <-stopResult:
			case <-time.After(time.Second):
				t.Fatal("StopContext did not finish after releasing quiesce")
			}
			t.Fatal("StopContext did not bind control generation")
		}
	}
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
	require.Zero(t, controlCloseCount.Load(), "control-plane owners remain available after a failed SIP generation")
}

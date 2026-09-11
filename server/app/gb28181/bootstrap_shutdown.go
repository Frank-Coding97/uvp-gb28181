package gb28181

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbhandler "uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play/reconciler"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	gbptz "uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	gbtalk "uvplatform.cn/uvp-gb28181/app/gb28181/talk"
	"uvplatform.cn/uvp-gb28181/app/gb28181/upgrade"
	gbzlmsched "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/asyncgroup"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

// sipQuiescer is optional so older test and embedding servers that only expose
// the original sipRuntimeServer contract remain source compatible.
type sipQuiescer interface {
	QuiesceRequests(context.Context) error
}

type sipQuiesceState struct {
	done    chan struct{}
	mu      sync.Mutex
	started bool
	err     error
}

// sipShutdownSnapshot owns one SIP generation's references. Shutdown steps
// close this captured generation; they do not rediscover globals after a
// timeout or a subsequent reload has started.
type sipShutdownSnapshot struct {
	server   sipRuntimeServer
	security *gbsecurity.Runtime
	cascade  cascadeRuntimeLifecycle

	playback *gbplayback.Service

	recordQuery  *recordquery.Service
	ptzService   *gbptz.Service
	ptzScheduler ptzSchedulerLifecycle
	firmware     *upgrade.Service

	subscriptionScheduler *subscribe.Scheduler
	offlineScanner        *device.OfflineScanner
	playReconciler        *reconciler.Reconciler

	positionPruneCancel context.CancelFunc
	positionPruneDone   <-chan struct{}

	recordingReconciler       *gbrecording.Reconciler
	recordingCatalogScheduler *gbrecording.CatalogReconcileScheduler
	recordingCatalogService   *gbrecording.CatalogService
	talkService               *gbtalk.Service
	talkCleanupWorker         *gbtalk.CleanupWorker
	background                *asyncgroup.Group
}

// Control-plane loops do not belong to a reloadable SIP generation. Their
// cancellation and completion channels are captured once for process stop.
type controlPlaneShutdownSnapshot struct {
	heartbeatCancel       context.CancelFunc
	heartbeatDone         <-chan struct{}
	threadLoadDone        <-chan struct{}
	configConvergenceDone <-chan struct{}
	startupProbeDone      <-chan struct{}
	nodeService           *gbzlmsvc.NodeService
	restartCoordinator    *gbzlmsvc.RestartCoordinator

	trafficCancel     context.CancelFunc
	trafficDone       <-chan struct{}
	trafficPrunerDone <-chan struct{}

	schedulerLogCancel context.CancelFunc
	schedulerLogDone   <-chan struct{}
	schedulerLog       *gbzlmsched.LogService

	metricsCleanupStop   context.CancelFunc
	metricsCleanupDone   <-chan struct{}
	metricsPersistCancel context.CancelFunc
	metricsPersistDone   <-chan struct{}
	dashboardCancel      context.CancelFunc
	dashboardDone        <-chan struct{}

	managementCore *zlmManagementCoreRuntime
}

// Lifecycle state is separate from sipLifecycleMu's resource pointers. Stop
// and Reload only hold sipLifecycleMu while capturing references; waits happen
// after unlocking it.
type bootstrapShutdownRun struct {
	generation        *shutdownGeneration
	controlGeneration *shutdownGeneration
	sip               sipShutdownSnapshot
	control           controlPlaneShutdownSnapshot
	quiesce           *sipQuiesceState
	process           atomic.Bool
	finalized         atomic.Bool
	controlSignalOnce sync.Once
}

var (
	reloadMu                sync.Mutex
	stopping                bool
	activeShutdownRun       *bootstrapShutdownRun
	lastShutdownGeneration  *shutdownGeneration
	sipQuiesce              *sipQuiesceState
	sipGenerationBackground *asyncgroup.Group

	zlmNodeService           *gbzlmsvc.NodeService
	zlmRestartCoordinator    *gbzlmsvc.RestartCoordinator
	startupProbeDone         <-chan struct{}
	configConvergenceDone    <-chan struct{}
	heartbeatDone            <-chan struct{}
	threadLoadDone           <-chan struct{}
	trafficDone              <-chan struct{}
	trafficPrunerDone        <-chan struct{}
	schedulerLogDone         <-chan struct{}
	metricsCleanupDone       <-chan struct{}
	metricsPersistDone       <-chan struct{}
	dashboardRetentionDone   <-chan struct{}
	positionHistoryPruneDone <-chan struct{}
)

func captureSIPShutdownSnapshot() sipShutdownSnapshot {
	return sipShutdownSnapshot{
		server: sipServer, security: securityRuntime, cascade: cascadeRuntimeManager,
		playback:    playbackService,
		recordQuery: recordQueryService,
		ptzService:  ptzService, ptzScheduler: ptzScheduler, firmware: firmwareUpgradeService,
		subscriptionScheduler: subscriptionScheduler,
		offlineScanner:        offlineScanner, playReconciler: playReconciler,
		positionPruneCancel: positionHistoryPruneCancel, positionPruneDone: positionHistoryPruneDone,
		recordingReconciler: recordingReconciler, recordingCatalogScheduler: recordingCatalogScheduler,
		recordingCatalogService: recordingCatalogService, talkService: talkSvc,
		talkCleanupWorker: talkCleanupWorker, background: sipGenerationBackground,
	}
}

func captureControlPlaneShutdownSnapshot() controlPlaneShutdownSnapshot {
	var metricsCleanupCancel context.CancelFunc
	if metricsCleanupStop != nil {
		stop := metricsCleanupStop
		var stopOnce sync.Once
		metricsCleanupCancel = func() { stopOnce.Do(func() { close(stop) }) }
	}
	return controlPlaneShutdownSnapshot{
		heartbeatCancel: heartbeatCancel, heartbeatDone: heartbeatDone,
		threadLoadDone: threadLoadDone, configConvergenceDone: configConvergenceDone,
		startupProbeDone: startupProbeDone, nodeService: zlmNodeService,
		restartCoordinator: zlmRestartCoordinator, trafficCancel: trafficCancel,
		trafficDone: trafficDone, trafficPrunerDone: trafficPrunerDone,
		schedulerLogCancel: schedulerLogCancel, schedulerLogDone: schedulerLogDone,
		schedulerLog: zlmSchedulerLog, metricsCleanupStop: metricsCleanupCancel,
		metricsCleanupDone: metricsCleanupDone, metricsPersistCancel: metricsPersistCancel,
		metricsPersistDone: metricsPersistDone, dashboardCancel: dashboardRetentionCancel,
		dashboardDone: dashboardRetentionDone, managementCore: zlmManagementCore,
	}
}

func (r *bootstrapShutdownRun) signal() {
	// The signal callback must stay non-blocking. It only closes admission
	// contexts; every completion wait is represented by a later step.
	if r == nil {
		return
	}
	if r.sip.positionPruneCancel != nil {
		r.sip.positionPruneCancel()
	}
	if r.process.Load() {
		r.signalControl()
	}
}

func (r *bootstrapShutdownRun) signalControl() {
	if r == nil {
		return
	}
	r.controlSignalOnce.Do(func() { r.control.signal() })
}

// bindControlGeneration attaches exactly one process-wide control shutdown to
// this SIP generation. Callers hold sipLifecycleMu while binding; the
// generation itself only signals and starts goroutines, never waits under the
// lifecycle lock. The control worker waits for the captured SIP generation to
// finish before stopping the control plane so metrics persistence remains
// alive for the SIP generation's final facts.
func (r *bootstrapShutdownRun) bindControlGeneration(ctx context.Context) {
	if r == nil || r.controlGeneration != nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sipGeneration := r.generation
	r.controlGeneration = newShutdownGeneration(ctx, r.signalControl, []shutdownStep{
		{name: "control-plane", stop: func(ctx context.Context) error {
			if sipGeneration != nil {
				<-sipGeneration.Done()
			}
			return stopControlPlaneSnapshot(ctx, r.control)
		}},
	})
}

func (r *bootstrapShutdownRun) requestProcessStop(ctx context.Context) {
	if r == nil {
		return
	}
	if !r.process.Swap(true) {
		r.bindControlGeneration(ctx)
		return
	}
	// A manually constructed or partially started run can be promoted more
	// than once; binding remains idempotent and never replaces the generation.
	r.bindControlGeneration(ctx)
}

func (r *bootstrapShutdownRun) waitControlGeneration(ctx context.Context) error {
	if r == nil || !r.process.Load() {
		return nil
	}
	if r.controlGeneration == nil {
		return errors.New("control-plane shutdown generation missing")
	}
	return r.controlGeneration.Wait(ctx)
}

func (c controlPlaneShutdownSnapshot) signal() {
	if c.heartbeatCancel != nil {
		c.heartbeatCancel()
	}
	if c.trafficCancel != nil {
		c.trafficCancel()
	}
	if c.schedulerLogCancel != nil {
		c.schedulerLogCancel()
	}
	if c.metricsCleanupStop != nil {
		c.metricsCleanupStop()
	}
	if c.dashboardCancel != nil {
		c.dashboardCancel()
	}
	// Metrics persistence remains active until SIP dependencies have emitted
	// their final facts; stopControlPlaneSnapshot cancels it at the tail.
}

func (r *bootstrapShutdownRun) steps() []shutdownStep {
	steps := []shutdownStep{
		{name: "sip.quiesce", stop: func(ctx context.Context) error {
			return quiesceSIPServerWithState(ctx, r.sip.server, r.quiesce)
		}},
		{name: "sip.dependencies", stop: func(ctx context.Context) error {
			// shutdownGeneration may start later steps concurrently after its
			// stop context expires. Keep this dependency boundary explicit so
			// accepted SIP handlers cannot lose their sinks before quiescing.
			waitSIPQuiesce(r.quiesce)
			return stopSIPDependenciesSnapshot(ctx, r.sip)
		}},
	}
	return steps
}

// QuiesceRequests closes only the SIP business admission gate and waits for
// already accepted request handlers. It deliberately leaves UAC and transport
// alive so accepted application background work can finish before StopContext.
func QuiesceRequests(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sipLifecycleMu.Lock()
	// Quiescing is the process shutdown admission boundary. Once the caller
	// crosses it, Start/Reload must not create a new SIP generation while the
	// remaining background work drains.
	stopping = true
	server := sipServer
	if server == nil && activeShutdownRun != nil {
		server = activeShutdownRun.sip.server
	}
	state := sipQuiesce
	if server != nil && state == nil {
		state = &sipQuiesceState{done: make(chan struct{})}
		sipQuiesce = state
	}
	sipLifecycleMu.Unlock()
	return quiesceSIPServerWithState(ctx, server, state)
}

func quiesceSIPServer(ctx context.Context, server sipRuntimeServer) error {
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sipLifecycleMu.Lock()
	state := sipQuiesce
	if state == nil {
		state = &sipQuiesceState{done: make(chan struct{})}
		sipQuiesce = state
	}
	sipLifecycleMu.Unlock()
	return quiesceSIPServerWithState(ctx, server, state)
}

func quiesceSIPServerWithState(ctx context.Context, server sipRuntimeServer, state *sipQuiesceState) error {
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if state == nil {
		sipLifecycleMu.Lock()
		state = sipQuiesce
		if state == nil {
			state = &sipQuiesceState{done: make(chan struct{})}
			sipQuiesce = state
		}
		sipLifecycleMu.Unlock()
	}
	state.mu.Lock()
	if state.started {
		state.mu.Unlock()
		return waitForSIPQuiesceResult(ctx, state)
	}
	state.started = true
	state.mu.Unlock()
	// The operation belongs to the captured SIP generation, so a caller's
	// deadline may stop waiting without declaring accepted handlers drained.
	// Run the actual admission drain independently and let each caller wait on
	// its own context below. This keeps a timed-out caller from allowing the
	// dependency step to clear sinks early.
	go func() {
		var err error
		if quiescer, ok := server.(sipQuiescer); ok {
			err = quiescer.QuiesceRequests(context.Background())
		}
		state.mu.Lock()
		state.err = err
		state.mu.Unlock()
		close(state.done)
	}()
	return waitForSIPQuiesceResult(ctx, state)
}

func waitForSIPQuiesceResult(ctx context.Context, state *sipQuiesceState) error {
	select {
	case <-state.done:
		return state.result()
	case <-ctx.Done():
		select {
		case <-state.done:
			return state.result()
		default:
			return ctx.Err()
		}
	}
}

func (s *sipQuiesceState) result() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func newBootstrapShutdownRun(ctx context.Context, sip sipShutdownSnapshot, control controlPlaneShutdownSnapshot, process bool) *bootstrapShutdownRun {
	// Callers hold sipLifecycleMu while creating a run. Create the shared
	// state before the generation starts so a deadline-expired coordinator
	// cannot start dependency cleanup before the quiesce step has even
	// established its completion state.
	if sip.server != nil && sipQuiesce == nil {
		sipQuiesce = &sipQuiesceState{done: make(chan struct{})}
	}
	quiesce := sipQuiesce
	if sip.server == nil {
		quiesce = nil
	}
	run := &bootstrapShutdownRun{sip: sip, control: control, quiesce: quiesce}
	run.process.Store(process)
	run.generation = newShutdownGeneration(ctx, run.signal, run.steps())
	if process {
		run.bindControlGeneration(ctx)
	}
	return run
}

func waitSIPQuiesce(state *sipQuiesceState) {
	if state != nil {
		<-state.done
	}
}

func shutdownDone(g *shutdownGeneration) bool {
	if g == nil {
		return true
	}
	select {
	case <-g.Done():
		return true
	default:
		return false
	}
}

func waitShutdownDone(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

func stopSIPDependenciesSnapshot(ctx context.Context, r sipShutdownSnapshot) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var stopErr error
	clearZLMManagementController()
	if r.playback != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("playback", r.playback.Close(ctx)))
	}
	gbroutes.SetDeviceMgmtPlaybackRuntime(nil, nil)
	gbroutes.SetPlaybackMediaSink(nil)
	if r.server != nil {
		r.server.SetPlaybackEndSink(nil)
		if u := r.server.UAC(); u != nil {
			u.SetPlaybackEndHook(nil)
		}
	}
	if r.recordQuery != nil {
		r.recordQuery.Close()
	}
	gbroutes.SetDeviceMgmtRecordQueryRuntime(nil, gbconfig.RecordQueryConfig{}, nil)
	if r.server != nil {
		r.server.SetRecordInfoSink(nil)
	}
	if r.firmware != nil && r.server != nil {
		if setter, ok := r.server.(interface {
			SetUpgradeProcessor(gbhandler.UpgradeMessageProcessor)
		}); ok {
			setter.SetUpgradeProcessor(nil)
		}
	}
	gbroutes.SetDeviceMgmtFirmwareUpgradeService(nil)
	if r.firmware != nil {
		r.firmware.Retire()
	}
	if r.ptzScheduler != nil {
		r.ptzScheduler.Stop()
	}
	if r.ptzService != nil {
		r.ptzService.Retire()
	}
	gbroutes.SetDeviceMgmtPTZRuntime(nil, nil)
	if r.background != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("sip.background", r.background.StopContext(ctx)))
	}
	if r.talkCleanupWorker != nil {
		r.talkCleanupWorker.Stop()
	}
	if r.server != nil {
		if broadcast, ok := r.server.(broadcastRuntimeServer); ok {
			broadcast.SetBroadcastMessageProcessor(nil)
			broadcast.SetBroadcastInviteProcessor(nil)
		}
		if u := r.server.UAC(); u != nil {
			u.SetTalkByeHandler(nil)
		}
	}
	if r.talkService != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("talk", r.talkService.Shutdown(ctx)))
	}
	gbroutes.SetTalkService(nil, nil)
	stopErr = errors.Join(stopErr, stopRecordingSnapshot(ctx, r))
	if r.positionPruneCancel != nil {
		r.positionPruneCancel()
		stopErr = errors.Join(stopErr, shutdownComponentError("position_history", waitShutdownDone(ctx, r.positionPruneDone)))
	}
	if r.subscriptionScheduler != nil {
		r.subscriptionScheduler.Stop()
	}
	if r.offlineScanner != nil {
		r.offlineScanner.Stop()
	}
	if r.playReconciler != nil {
		r.playReconciler.Stop()
	}
	gbroutes.SetPlayService(nil)
	gbroutes.SetPlayAuthorizer(nil)
	gbroutes.SetDeviceMgmtCatalogTrigger(nil)
	gbroutes.SetDeviceMgmtSubscriptionManager(nil)
	gbroutes.SetDeviceMgmtCaptureRuntime(nil)
	if r.server != nil {
		r.server.SetSnapshotSink(nil)
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("cascade", stopCascadeSnapshot(ctx, r.cascade)))
	if r.server != nil {
		if err := r.server.Shutdown(ctx); err != nil {
			stopErr = errors.Join(stopErr, shutdownComponentError("sip", err))
			app.Log(ctx).Named("sip").Error("SIP shutdown incomplete", zap.String("event", "sip.shutdown_incomplete"), logging.Error(err))
		}
	}
	if r.security != nil {
		if err := r.security.Close(ctx); err != nil {
			stopErr = errors.Join(stopErr, shutdownComponentError("security", err))
			app.Log(ctx).Named("security").Error("Security persistence shutdown incomplete", zap.String("event", "security.shutdown_incomplete"), logging.Error(err))
		}
	}
	return stopErr
}

func stopRecordingSnapshot(ctx context.Context, r sipShutdownSnapshot) error {
	var stopErr error
	device.SetStatusObserver(nil)
	gbroutes.SetRecordingPlanStreamObserver(nil)
	executors.SetRecordingPlanRuntime(nil)
	gbroutes.SetRecordingPlanSourceLeaseChecker(nil)
	if r.recordingCatalogService != nil {
		r.recordingCatalogService.CloseDownloads()
	}
	if r.recordingCatalogScheduler != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("recording.catalog", r.recordingCatalogScheduler.Stop()))
	}
	if r.recordingReconciler != nil {
		r.recordingReconciler.Stop()
	}
	gbroutes.SetCloudRecordingCatalogService(nil)
	gbroutes.SetRecordingService(nil, nil, nil)
	_ = ctx
	return stopErr
}

func stopCascadeSnapshot(ctx context.Context, manager cascadeRuntimeLifecycle) error {
	if manager == nil {
		return nil
	}
	return manager.Shutdown(ctx)
}

func stopControlPlaneSnapshot(ctx context.Context, c controlPlaneShutdownSnapshot) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var stopErr error
	for _, component := range []struct {
		name string
		done <-chan struct{}
	}{
		{name: "heartbeat", done: c.heartbeatDone},
		{name: "thread_load", done: c.threadLoadDone},
		{name: "config_convergence", done: c.configConvergenceDone},
		{name: "startup_probe", done: c.startupProbeDone},
	} {
		stopErr = errors.Join(stopErr, shutdownComponentError(component.name, waitShutdownDone(ctx, component.done)))
	}
	if c.nodeService != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("zlm.node", c.nodeService.StopContext(ctx)))
	}
	if c.restartCoordinator != nil {
		stopErr = errors.Join(stopErr, shutdownComponentError("zlm.restart", c.restartCoordinator.StopContext(ctx)))
	}
	if c.managementCore != nil && c.managementCore.overview != nil {
		c.managementCore.overview.Close()
	}
	if c.trafficCancel != nil {
		c.trafficCancel()
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("traffic", waitShutdownDone(ctx, c.trafficDone)))
	stopErr = errors.Join(stopErr, shutdownComponentError("traffic.pruner", waitShutdownDone(ctx, c.trafficPrunerDone)))
	if c.schedulerLogCancel != nil {
		c.schedulerLogCancel()
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("scheduler.log", waitShutdownDone(ctx, c.schedulerLogDone)))
	if c.schedulerLog != nil {
		c.schedulerLog.Stop()
	}
	if c.metricsCleanupStop != nil {
		c.metricsCleanupStop()
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("metrics.cleanup", waitShutdownDone(ctx, c.metricsCleanupDone)))
	if c.metricsPersistCancel != nil {
		c.metricsPersistCancel()
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("metrics.persistence", waitShutdownDone(ctx, c.metricsPersistDone)))
	if c.dashboardCancel != nil {
		c.dashboardCancel()
	}
	stopErr = errors.Join(stopErr, shutdownComponentError("dashboard.retention", waitShutdownDone(ctx, c.dashboardDone)))
	return stopErr
}

func clearSIPShutdownGlobals() {
	sipServer = nil
	sipQuiesce = nil
	securityRuntime = nil
	cascadeRuntimeManager = nil
	playbackService, playbackRegistry, playbackMetrics = nil, nil, nil
	deviceCaptureRegistry = nil
	recordQueryService, recordQueryMetrics = nil, nil
	ptzService, ptzScheduler, firmwareUpgradeService = nil, nil, nil
	subscriptionService, subscriptionScheduler = nil, nil
	offlineScanner, playReconciler = nil, nil
	playSvc, playAuthMetrics = nil, nil
	positionHistoryPruneCancel, positionHistoryPruneDone = nil, nil
	recordingSvc, recordingRepo = nil, nil
	recordingReconciler, recordingCatalogScheduler, recordingCatalogService = nil, nil, nil
	recordingPlanEngine, recordingPlanLeases = nil, nil
	talkSvc, talkRepo, talkCleanupWorker = nil, nil, nil
	sipGenerationBackground = nil
	zlmClient, zlmLocationMap = nil, nil
	gbroutes.SetSecurityRuntime(nil)
}

func clearControlPlaneGlobals() {
	heartbeatCancel, heartbeatDone, threadLoadDone, configConvergenceDone = nil, nil, nil, nil
	startupProbeDone = nil
	zlmNodeService, zlmRestartCoordinator = nil, nil
	trafficCancel, trafficDone, trafficPrunerDone = nil, nil, nil
	schedulerLogCancel, schedulerLogDone, zlmSchedulerLog = nil, nil, nil
	metricsCleanupStop, metricsCleanupDone = nil, nil
	metricsPersistCancel, metricsPersistDone = nil, nil
	dashboardRetentionCancel, dashboardRetentionDone = nil, nil
	if zlmManagementCore != nil {
		zlmManagementCore = nil
	}
}

func finalizeProcessShutdown(_ context.Context, run *bootstrapShutdownRun, err error) error {
	if run == nil || !shutdownDone(run.generation) {
		return err
	}
	if run.process.Load() && (run.controlGeneration == nil || !shutdownDone(run.controlGeneration)) {
		// A caller's deadline may expire while either generation is still doing
		// real work. Keep every captured reference reachable until a later Stop
		// joins the same generations to completion.
		return err
	}
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()
	if run.process.Load() {
		if run.finalized.Swap(true) {
			return err
		}
		teardownZLMManagementCore()
		clearSIPShutdownGlobals()
		clearControlPlaneGlobals()
	}
	return err
}

// StopContext begins or joins the process shutdown generation. The lifecycle
// mutex is never held while waiting for any component.
func StopContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sipLifecycleMu.Lock()
	stopping = true
	if activeShutdownRun != nil && shutdownDone(activeShutdownRun.generation) {
		lastShutdownGeneration = activeShutdownRun.generation
		if !activeShutdownRun.process.Load() && activeShutdownRun.generation.result() == nil {
			// Reload owns successful-generation cleanup. A failed generation
			// remains active so process Stop can promote and join its captured
			// result without invoking SIP shutdown a second time.
			clearSIPShutdownGlobals()
			activeShutdownRun = nil
		}
	}
	if activeShutdownRun == nil {
		run := newBootstrapShutdownRun(ctx, captureSIPShutdownSnapshot(), captureControlPlaneShutdownSnapshot(), true)
		activeShutdownRun = run
		sipLifecycleMu.Unlock()
		err := run.generation.Wait(ctx)
		err = errors.Join(err, run.waitControlGeneration(ctx))
		return finalizeProcessShutdown(ctx, run, err)
	}
	run := activeShutdownRun
	run.requestProcessStop(ctx)
	sipLifecycleMu.Unlock()
	err := run.generation.Wait(ctx)
	err = errors.Join(err, run.waitControlGeneration(ctx))
	return finalizeProcessShutdown(ctx, run, err)
}

// Stop preserves the historical no-result API for process callers.
func Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = StopContext(ctx)
}

// ReloadSIP is implemented in bootstrap.go; this helper is kept here to make
// the generation state available to its small lock-and-wait wrapper.
func reloadSIPGeneration(ctx context.Context) (*bootstrapShutdownRun, error) {
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()
	if stopping {
		return nil, errors.New("gb28181 正在停止,拒绝 SIP 热重载")
	}
	if activeShutdownRun != nil && shutdownDone(activeShutdownRun.generation) {
		run := activeShutdownRun
		lastShutdownGeneration = run.generation
		if err := run.generation.result(); err != nil {
			return nil, fmt.Errorf("上一次 SIP 热重载未完成: %w", err)
		}
		clearSIPShutdownGlobals()
		activeShutdownRun = nil
	}
	if activeShutdownRun != nil {
		return activeShutdownRun, nil
	}
	run := newBootstrapShutdownRun(ctx, captureSIPShutdownSnapshot(), captureControlPlaneShutdownSnapshot(), false)
	activeShutdownRun = run
	return run, nil
}

func finishReloadGeneration(run *bootstrapShutdownRun, err error) {
	if run == nil || !shutdownDone(run.generation) {
		return
	}
	generationErr := run.generation.result()
	sipLifecycleMu.Lock()
	defer sipLifecycleMu.Unlock()
	lastShutdownGeneration = run.generation
	// A failed generation keeps its captured references and active marker so a
	// later Reload cannot silently build on partially stopped dependencies.
	if generationErr != nil || run.process.Load() {
		return
	}
	clearSIPShutdownGlobals()
	if activeShutdownRun == run {
		activeShutdownRun = nil
	}
	_ = err
}

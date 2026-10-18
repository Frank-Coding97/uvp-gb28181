// Package runtime owns the independent lifecycle of each cascade upstream.
// It deliberately does not bind a SIP listener or construct a sipgo client.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const (
	defaultRegisterExpires           = 3600
	defaultKeepalive                 = 60 * time.Second
	defaultRetryDelay                = time.Minute
	defaultHeartbeatFailureThreshold = 3
	// 注册成功后首条 Keepalive 尽快发出以确认链路,缩短「等待心跳」展示窗口;
	// 后续心跳仍按平台配置的周期进行。
	firstKeepaliveDelay = time.Second
)

// PlatformClient is the per-platform transaction surface. A production adapter
// may wrap sipclient.TransactionClient; tests inject a deterministic fake.
type PlatformClient interface {
	Register(context.Context, int, string) sipclient.TransactionResult
	Logout(context.Context, string) sipclient.TransactionResult
	Keepalive(context.Context, string) sipclient.TransactionResult
}

type profileNegotiator interface {
	SetProfile(protocol.Version) error
}

type registrationVersionStore interface {
	RecordRegistrationVersion(context.Context, uint64, string, string, string, time.Time) error
}

type ClientFactory interface {
	NewClient(model.GbCascadePlatform) (PlatformClient, error)
}

// ResourceAcquirer owns listener or other platform-scoped external resources.
// The actor only calls the returned release function during termination.
type ResourceAcquirer interface {
	Acquire(model.GbCascadePlatform) (func() error, error)
}

type Timer interface{ Stop() }

type Scheduler interface {
	Schedule(time.Duration, func()) Timer
}

// PlatformStore is the narrow persistence boundary used by runtime actors.
// repository.GormRepository satisfies it without making runtime depend on CRUD.
type PlatformStore interface {
	ListEnabledPlatforms(context.Context) ([]model.GbCascadePlatform, error)
	RecordRegistrationSuccess(context.Context, uint64, time.Time, time.Time) error
	RecordRegistrationExpired(context.Context, uint64, time.Time) error
	RecordRegistrationFailure(context.Context, uint64, string, string, time.Time) error
	RecordHeartbeatSuccess(context.Context, uint64, time.Time) error
	RecordHeartbeatFailure(context.Context, uint64, string, string, time.Time) error
}

type Dependencies struct {
	Store         PlatformStore
	Clients       ClientFactory
	Resources     ResourceAcquirer
	Clock         service.Clock
	Scheduler     Scheduler
	RetryDelay    time.Duration
	CatalogPusher service.CatalogPusher
}

// Manager reconciles enabled platform configuration without affecting the
// shared device-side SIP runtime.
type Manager struct {
	deps Dependencies

	reloadMu sync.Mutex
	mu       sync.RWMutex
	actors   map[uint64]*Actor
	errs     map[uint64]error
	stopped  bool
}

func NewManager(deps Dependencies) *Manager {
	if deps.Clock == nil {
		deps.Clock = wallClock{}
	}
	if deps.Scheduler == nil {
		deps.Scheduler = wallScheduler{}
	}
	if deps.RetryDelay <= 0 {
		deps.RetryDelay = defaultRetryDelay
	}
	return &Manager{deps: deps, actors: make(map[uint64]*Actor), errs: make(map[uint64]error)}
}

// Reload replaces actors whose persisted configuration revision changed, stops
// removed actors, and starts new ones. An invalid platform is isolated.
func (m *Manager) Reload(ctx context.Context) error {
	if m == nil {
		return errors.New("cascade runtime manager is unavailable")
	}
	if m.deps.Store == nil {
		return errors.New("cascade platform store is unavailable")
	}
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	platforms, err := m.deps.Store.ListEnabledPlatforms(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[uint64]model.GbCascadePlatform, len(platforms))
	for _, platform := range platforms {
		if platform.ID != 0 {
			wanted[platform.ID] = platform
		}
	}

	m.mu.RLock()
	if m.stopped {
		m.mu.RUnlock()
		return errors.New("cascade runtime manager is stopped")
	}
	current := make(map[uint64]*Actor, len(m.actors))
	for id, actor := range m.actors {
		current[id] = actor
	}
	m.mu.RUnlock()

	for id, actor := range current {
		platform, exists := wanted[id]
		if !exists || actor.Revision() != platform.ConfigRevision {
			if stopErr := actor.Stop(ctx); stopErr != nil && ctx.Err() == nil {
				return stopErr
			}
			m.removeActor(id, actor)
		}
	}
	for id, platform := range wanted {
		m.mu.RLock()
		_, exists := m.actors[id]
		m.mu.RUnlock()
		if exists {
			m.mu.RLock()
			actor := m.actors[id]
			m.mu.RUnlock()
			if actor.projectionRevision.Swap(platform.ProjectionRevision) != platform.ProjectionRevision {
				actor.enqueue(context.Background(), commandCatalog)
			}
			continue
		}
		actor, createErr := m.newActor(platform)
		if createErr != nil {
			m.recordPlatformError(ctx, platform.ID, createErr)
			continue
		}
		m.mu.Lock()
		if !m.stopped {
			m.actors[id] = actor
			delete(m.errs, id)
			m.mu.Unlock()
			actor.Start()
			continue
		}
		m.mu.Unlock()
		_ = actor.Stop(context.Background())
	}
	return nil
}

func (m *Manager) newActor(platform model.GbCascadePlatform) (*Actor, error) {
	if m.deps.Clients == nil {
		return nil, errors.New("cascade platform client factory is unavailable")
	}
	client, err := m.deps.Clients.NewClient(platform)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("cascade platform client is unavailable")
	}
	release := func() error { return nil }
	if m.deps.Resources != nil {
		release, err = m.deps.Resources.Acquire(platform)
		if err != nil {
			return nil, err
		}
		if release == nil {
			return nil, errors.New("cascade platform resource release is unavailable")
		}
	}
	actor := newActor(platform, client, m.deps.Store, m.deps.Clock, m.deps.Scheduler, m.deps.RetryDelay, release)
	actor.pusher = m.deps.CatalogPusher
	actor.projectionRevision.Store(platform.ProjectionRevision)
	return actor, nil
}

func (m *Manager) removeActor(id uint64, actor *Actor) {
	m.mu.Lock()
	if m.actors[id] == actor {
		delete(m.actors, id)
	}
	m.mu.Unlock()
}

func (m *Manager) recordPlatformError(ctx context.Context, id uint64, cause error) {
	m.mu.Lock()
	m.errs[id] = cause
	m.mu.Unlock()
	_ = m.deps.Store.RecordRegistrationFailure(ctx, id, "config", cause.Error(), m.deps.Clock.Now().UTC())
}

func (m *Manager) PlatformIDs() []uint64 {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]uint64, 0, len(m.actors))
	for id := range m.actors {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) PlatformError(platformID uint64) error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errs[platformID]
}

// Shutdown uses the supplied context as the global lifecycle deadline. Each
// actor receives the same deadline so a blocked upstream cannot extend it.
func (m *Manager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.reloadMu.Lock()
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		m.reloadMu.Unlock()
		return nil
	}
	m.stopped = true
	actors := make([]*Actor, 0, len(m.actors))
	for _, actor := range m.actors {
		actors = append(actors, actor)
	}
	m.actors = make(map[uint64]*Actor)
	m.mu.Unlock()
	m.reloadMu.Unlock()

	var result error
	for _, actor := range actors {
		if err := actor.Stop(ctx); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

type actorCommand uint8

const (
	commandStart actorCommand = iota
	commandReconnect
	commandRefresh
	commandKeepalive
	commandStop
	commandForceStop
	commandCatalog
)

type transactionKind uint8

const (
	transactionRegister transactionKind = iota
	transactionLogout
	transactionKeepalive
)

type transactionEvent struct {
	kind   transactionKind
	result sipclient.TransactionResult
}

// Actor serializes commands for one platform. Transactions execute outside the
// loop, but their results return to this loop and inFlight prevents overlap.
type Actor struct {
	platform  model.GbCascadePlatform
	client    PlatformClient
	store     PlatformStore
	clock     service.Clock
	scheduler Scheduler
	retry     time.Duration
	release   func() error

	ctx                context.Context
	cancel             context.CancelFunc
	cmd                chan actorCommand
	result             chan transactionEvent
	done               chan struct{}
	once               sync.Once
	pusher             service.CatalogPusher
	projectionRevision atomic.Uint64
	pushWG             sync.WaitGroup
}

func newActor(platform model.GbCascadePlatform, client PlatformClient, store PlatformStore, clock service.Clock, scheduler Scheduler, retry time.Duration, release func() error) *Actor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Actor{platform: platform, client: client, store: store, clock: clock, scheduler: scheduler, retry: retry, release: release, ctx: ctx, cancel: cancel, cmd: make(chan actorCommand, 8), result: make(chan transactionEvent, 1), done: make(chan struct{})}
}

func (a *Actor) Revision() uint64 { return a.platform.ConfigRevision }

func (a *Actor) Start() {
	go a.run()
	a.enqueue(context.Background(), commandStart)
}

func (a *Actor) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.enqueue(ctx, commandStop)
	select {
	case <-a.done:
		return nil
	case <-ctx.Done():
		a.cancel()
		// 强停必须送达:用独立短超时,队列满也不会永久阻塞
		forceCtx, forceCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer forceCancel()
		a.enqueue(forceCtx, commandForceStop)
		return ctx.Err()
	}
}

func (a *Actor) enqueue(ctx context.Context, command actorCommand) {
	if a == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-a.done:
		return
	default:
	}
	// 队列满时监听 ctx:调用方的停止期限不被永久阻塞
	select {
	case a.cmd <- command:
	case <-a.done:
	case <-ctx.Done():
	}
}

func (a *Actor) run() {
	var refreshTimer, keepaliveTimer, retryTimer, catalogRetryTimer Timer
	var inFlight bool
	var registered bool
	var stopping bool
	var refreshPending, keepalivePending bool
	heartbeatFailures := 0
	var sequence uint64
	var pushInFlight, pushPending bool
	pushNeeded := true
	pushResults := make(chan error, 1)
	pushCtx, cancelPush := context.WithCancel(a.ctx)
	defer cancelPush()
	startPush := func() {
		if !registered || stopping || !a.platform.AutoPushCatalog || a.pusher == nil {
			return
		}
		if pushInFlight {
			pushPending = true
			return
		}
		pushInFlight = true
		pushNeeded = false
		a.pushWG.Add(1)
		go func() {
			defer a.pushWG.Done()
			ctx, cancel := context.WithTimeout(pushCtx, 2*time.Minute)
			defer cancel()
			_, _, err := a.pusher.PushCatalog(ctx, a.platform.ID)
			select {
			case pushResults <- err:
			case <-a.ctx.Done():
			}
		}()
	}
	stopTimers := func() {
		if catalogRetryTimer != nil {
			catalogRetryTimer.Stop()
			catalogRetryTimer = nil
		}
		if refreshTimer != nil {
			refreshTimer.Stop()
			refreshTimer = nil
		}
		if keepaliveTimer != nil {
			keepaliveTimer.Stop()
			keepaliveTimer = nil
		}
		if retryTimer != nil {
			retryTimer.Stop()
			retryTimer = nil
		}
	}
	stopHeartbeatTimers := func() {
		if refreshTimer != nil {
			refreshTimer.Stop()
			refreshTimer = nil
		}
		if keepaliveTimer != nil {
			keepaliveTimer.Stop()
			keepaliveTimer = nil
		}
	}
	finish := func() {
		a.once.Do(func() {
			stopTimers()
			a.cancel()
			a.pushWG.Wait()
			if a.release != nil {
				_ = a.release()
			}
			close(a.done)
		})
	}
	defer finish()

	callID := func(kind string) string {
		sequence++
		return fmt.Sprintf("cascade-%s-%d-%d", kind, a.platform.ID, sequence)
	}
	startTransaction := func(kind transactionKind) {
		if a.client == nil {
			return
		}
		if inFlight {
			switch kind {
			case transactionRegister:
				refreshPending = true
			case transactionKeepalive:
				keepalivePending = true
			}
			return
		}
		inFlight = true
		var id string
		switch kind {
		case transactionRegister:
			id = callID("register")
		case transactionLogout:
			id = callID("logout")
		case transactionKeepalive:
			id = callID("keepalive")
		}
		go func() {
			var result sipclient.TransactionResult
			switch kind {
			case transactionRegister:
				result = a.client.Register(a.ctx, registerExpires(a.platform), id)
			case transactionLogout:
				result = a.client.Logout(a.ctx, id)
			case transactionKeepalive:
				result = a.client.Keepalive(a.ctx, id)
			}
			select {
			case a.result <- transactionEvent{kind: kind, result: result}:
			case <-a.done:
			}
		}()
	}
	schedule := func(delay time.Duration, command actorCommand) Timer {
		if delay <= 0 {
			return nil
		}
		return a.scheduler.Schedule(delay, func() { a.enqueue(context.Background(), command) })
	}

	for {
		select {
		case err := <-pushResults:
			pushInFlight = false
			pushNeeded = err != nil
			if pushPending {
				pushPending = false
				startPush()
			} else if err != nil && !stopping {
				catalogRetryTimer = schedule(a.retry, commandCatalog)
			}
		case command := <-a.cmd:
			switch command {
			case commandCatalog:
				if catalogRetryTimer != nil {
					catalogRetryTimer.Stop()
					catalogRetryTimer = nil
				}
				pushNeeded = true
				startPush()
			case commandStart, commandReconnect:
				if !stopping {
					if command == commandReconnect && retryTimer != nil {
						retryTimer.Stop()
						retryTimer = nil
					}
					startTransaction(transactionRegister)
				}
			case commandRefresh:
				if !stopping {
					startTransaction(transactionRegister)
				}
			case commandKeepalive:
				if !stopping && registered {
					startTransaction(transactionKeepalive)
				}
			case commandStop:
				if stopping {
					continue
				}
				stopping = true
				refreshPending = false
				keepalivePending = false
				cancelPush()
				stopTimers()
				if !inFlight {
					if registered {
						startTransaction(transactionLogout)
					} else {
						return
					}
				}
			case commandForceStop:
				return
			}
		case event := <-a.result:
			inFlight = false
			now := a.clock.Now().UTC()
			switch event.kind {
			case transactionRegister:
				grantedExpires := registerExpiresForResult(a.platform, event.result)
				if event.result.Success && grantedExpires > 0 {
					registered = true
					heartbeatFailures = 0
					if a.platform.ProfileOverride == model.CascadeProfileOverrideAuto && event.result.NegotiatedVersionFound {
						if versionStore, ok := a.store.(registrationVersionStore); ok {
							effective := string(event.result.NegotiatedVersion)
							if err := versionStore.RecordRegistrationVersion(context.Background(), a.platform.ID, event.result.ReportedGBVersion, effective, "negotiated", now); err == nil {
								a.platform.ReportedGBVersion = event.result.ReportedGBVersion
								a.platform.EffectiveVersion = effective
								a.platform.EffectiveVersionFrom = "negotiated"
								a.platform.EffectiveVersionAt = &now
								if negotiator, ok := a.client.(profileNegotiator); ok {
									_ = negotiator.SetProfile(event.result.NegotiatedVersion)
								}
							}
						}
					}
					_ = a.store.RecordRegistrationSuccess(context.Background(), a.platform.ID, now, now.Add(time.Duration(grantedExpires)*time.Second))
					if !stopping {
						if pushNeeded {
							startPush()
						}
						if !refreshPending {
							refreshTimer = schedule(sipclient.RefreshDelay(grantedExpires), commandRefresh)
						}
						if !keepalivePending {
							keepaliveTimer = schedule(firstKeepaliveDelay, commandKeepalive)
						}
					}
				} else {
					registered = false
					pushNeeded = true
					code, message := resultCode(event.result), resultMessage(event.result)
					if event.result.GrantedExpiresFound && event.result.GrantedExpires == 0 {
						code, message = "expired", "upstream granted zero REGISTER expiry"
					}
					_ = a.store.RecordRegistrationFailure(context.Background(), a.platform.ID, code, message, now)
					if !stopping {
						retryTimer = schedule(a.retry, commandReconnect)
					}
				}
			case transactionKeepalive:
				if event.result.Success {
					heartbeatFailures = 0
					_ = a.store.RecordHeartbeatSuccess(context.Background(), a.platform.ID, now)
				} else {
					// Only an actual timeout contributes to the consecutive
					// failure threshold. A definitive SIP rejection is a
					// different state and must not be combined with later
					// timeouts to trigger a false re-registration.
					if isHeartbeatTimeout(event.result) {
						heartbeatFailures++
					} else {
						heartbeatFailures = 0
					}
					_ = a.store.RecordHeartbeatFailure(context.Background(), a.platform.ID, resultCode(event.result), resultMessage(event.result), now)
					if isHeartbeatTimeout(event.result) && heartbeatFailures >= defaultHeartbeatFailureThreshold {
						registered = false
						keepalivePending = false
						refreshPending = true
						stopHeartbeatTimers()
						if retryTimer != nil {
							retryTimer.Stop()
							retryTimer = nil
						}
						heartbeatFailures = 0
						_ = a.store.RecordRegistrationExpired(context.Background(), a.platform.ID, now)
					}
				}
				if !stopping && registered && !keepalivePending {
					keepaliveTimer = schedule(keepaliveDelay(a.platform), commandKeepalive)
				}
			case transactionLogout:
				if event.result.Success {
					_ = a.store.RecordRegistrationExpired(context.Background(), a.platform.ID, now)
				} else {
					_ = a.store.RecordRegistrationFailure(context.Background(), a.platform.ID, resultCode(event.result), resultMessage(event.result), now)
				}
				return
			}
			if !stopping && !inFlight {
				if refreshPending {
					refreshPending = false
					startTransaction(transactionRegister)
				} else if keepalivePending && registered {
					keepalivePending = false
					startTransaction(transactionKeepalive)
				}
			}
			if stopping && !inFlight {
				if registered {
					startTransaction(transactionLogout)
				} else {
					return
				}
			}
		}
	}
}

func registerExpires(platform model.GbCascadePlatform) int {
	if platform.RegisterExpires > 0 {
		return platform.RegisterExpires
	}
	return defaultRegisterExpires
}

func registerExpiresForResult(platform model.GbCascadePlatform, result sipclient.TransactionResult) int {
	if result.GrantedExpiresFound {
		return result.GrantedExpires
	}
	return registerExpires(platform)
}

func keepaliveDelay(platform model.GbCascadePlatform) time.Duration {
	if platform.KeepaliveInterval > 0 {
		return time.Duration(platform.KeepaliveInterval) * time.Second
	}
	return defaultKeepalive
}

func resultCode(result sipclient.TransactionResult) string {
	if result.GrantedExpiresErr != nil {
		return "expires"
	}
	if result.StatusCode > 0 {
		return fmt.Sprintf("%d", result.StatusCode)
	}
	if result.BuildErr != nil {
		return "build"
	}
	if result.TransportErr != nil {
		return "transport"
	}
	return "unknown"
}

func resultMessage(result sipclient.TransactionResult) string {
	if result.GrantedExpiresErr != nil {
		return result.GrantedExpiresErr.Error()
	}
	if result.BuildErr != nil {
		return result.BuildErr.Error()
	}
	if result.TransportErr != nil {
		return result.TransportErr.Error()
	}
	if result.StatusCode > 0 {
		return fmt.Sprintf("SIP %d", result.StatusCode)
	}
	return "cascade transaction failed"
}

func isHeartbeatTimeout(result sipclient.TransactionResult) bool {
	if result.TransportErr != nil {
		return errors.Is(result.TransportErr, context.DeadlineExceeded) || errors.Is(result.TransportErr, context.Canceled)
	}
	switch result.StatusCode {
	case 408, 480, 504:
		return true
	default:
		return false
	}
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type wallScheduler struct{}

func (wallScheduler) Schedule(delay time.Duration, callback func()) Timer {
	return wallTimer{time.AfterFunc(delay, callback)}
}

type wallTimer struct{ timer *time.Timer }

func (t wallTimer) Stop() {
	if t.timer != nil {
		t.timer.Stop()
	}
}

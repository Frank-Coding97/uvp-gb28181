package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var revocationWorkerDBID atomic.Int64

type revocationWorkerFixture struct {
	t       *testing.T
	db      *gorm.DB
	clock   time.Time
	factory *stubRevocationRuntimeFactory
	control *stubRevocationControl
	boot    string
	now     func() time.Time
}

type stubRevocationRuntimeFactory struct {
	mu                sync.Mutex
	runtime           RevocationRuntime
	err               error
	calls             int
	resolveDeadline   chan time.Time
	resolveStarted    chan struct{}
	resolveWaitCancel bool
}

func (f *stubRevocationRuntimeFactory) Resolve(ctx context.Context, _ string) (RevocationRuntime, error) {
	f.mu.Lock()
	f.calls++
	runtime, err := f.runtime, f.err
	deadlineChannel := f.resolveDeadline
	startedChannel := f.resolveStarted
	waitCancel := f.resolveWaitCancel
	f.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok && deadlineChannel != nil {
		deadlineChannel <- deadline
	}
	if startedChannel != nil {
		select {
		case <-startedChannel:
		default:
			close(startedChannel)
		}
	}
	if waitCancel {
		<-ctx.Done()
		return RevocationRuntime{}, ctx.Err()
	}
	return runtime, err
}

type stubRevocationControl struct {
	mu             sync.Mutex
	players        zlm.RuntimePlayers
	sessions       zlm.RuntimeSessions
	playersErr     error
	sessionsErr    error
	kickResult     zlm.ConditionalKickResult
	kickErr        error
	playerCalls    int
	sessionCalls   int
	kickCalls      []string
	blockPlayers   bool
	playersEntered chan struct{}
	releasePlayers chan struct{}
}

func (c *stubRevocationControl) GetRuntimeMediaPlayers(_ context.Context, _ zlm.StreamTarget) (zlm.RuntimePlayers, error) {
	c.mu.Lock()
	c.playerCalls++
	err := c.playersErr
	players := cloneRuntimePlayers(c.players)
	block := c.blockPlayers
	entered := c.playersEntered
	release := c.releasePlayers
	if block && entered != nil {
		select {
		case <-entered:
		default:
			close(entered)
		}
	}
	c.mu.Unlock()
	if block && release != nil {
		<-release
	}
	return players, err
}

func (c *stubRevocationControl) GetRuntimeSessions(_ context.Context) (zlm.RuntimeSessions, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionCalls++
	return cloneRuntimeSessions(c.sessions), c.sessionsErr
}

func (c *stubRevocationControl) KickSessionIfMatch(_ context.Context, _ string, identifier string) (zlm.ConditionalKickResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kickCalls = append(c.kickCalls, identifier)
	return c.kickResult, c.kickErr
}

func (c *stubRevocationControl) kickCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.kickCalls)
}

func (c *stubRevocationControl) calls() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playerCalls, c.sessionCalls
}

func newRevocationWorkerFixture(t *testing.T, budget time.Duration) *revocationWorkerFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:revocation_worker_%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)", revocationWorkerDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&models.PlayGrant{}, &models.Viewer{}))
	now := time.Date(2026, 9, 6, 4, 0, 0, 123456000, time.UTC)
	boot := "0123456789abcdef0123456789abcdef"
	control := &stubRevocationControl{
		players:    zlm.RuntimePlayers{BootNonce: boot, Players: []zlm.MediaPlayer{}},
		sessions:   zlm.RuntimeSessions{BootNonce: boot, Sessions: []zlm.Session{}},
		kickResult: zlm.KickShutdownScheduled,
	}
	factory := &stubRevocationRuntimeFactory{runtime: RevocationRuntime{
		Control: control, CurrentBootNonce: boot, HookBudget: budget, Trusted: true,
	}}
	f := &revocationWorkerFixture{t: t, db: db, clock: now, factory: factory, control: control, boot: boot}
	f.now = func() time.Time { return f.clock }
	return f
}

func (f *revocationWorkerFixture) worker(limit int) *RevocationWorker {
	f.t.Helper()
	return NewRevocationWorker(f.db, f.factory, f.now, limit)
}

func (f *revocationWorkerFixture) seed(t *testing.T, number int, state models.ViewerState) models.Viewer {
	t.Helper()
	grantID := fmt.Sprintf("00000000-0000-4000-8000-%012d", number)
	identifier := fmt.Sprintf("1-%d", number)
	deviceID := "34020000001320000001"
	channelID := "34020000001310000001"
	nodeUUID := "node-a"
	schema := "rtmp"
	vhost := "__defaultVhost__"
	app := "live"
	stream := fmt.Sprintf("stream-%d", number)
	protocol := "https-flv"
	generation := uint64(number + 1)
	grant := models.PlayGrant{
		GrantID: grantID, ClientID: 7, Scope: "play:live:apply", DeviceID: &deviceID, ChannelID: &channelID,
		ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1, NodeUUID: &nodeUUID, BootNonce: &f.boot,
		Schema: &schema, VHost: &vhost, App: &app, Stream: &stream, MediaGeneration: &generation,
		Protocol: &protocol, IssuedAt: f.clock.Add(-time.Minute), ExpiresAt: f.clock.Add(time.Minute),
		State: models.GrantStateRevoked, Reason: "client.disabled", CreatedAt: f.clock, UpdatedAt: f.clock,
	}
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := models.Viewer{
		GrantID: grantID, NodeUUID: nodeUUID, BootNonce: f.boot, Identifier: identifier,
		Schema: schema, VHost: vhost, App: app, Stream: stream, MediaGeneration: generation,
		State: state, RetryAt: timePtr(f.clock), Attempts: 0, LastErrorClass: "revocation_pending",
		CreatedAt: f.clock, UpdatedAt: f.clock,
	}
	require.NoError(t, f.db.Create(&viewer).Error)
	return viewer
}

func (f *revocationWorkerFixture) loadViewer(t *testing.T, id int64) models.Viewer {
	t.Helper()
	var viewer models.Viewer
	require.NoError(t, f.db.First(&viewer, "id = ?", id).Error)
	return viewer
}

func (f *revocationWorkerFixture) setSnapshot(players, sessions []string) {
	f.control.mu.Lock()
	defer f.control.mu.Unlock()
	f.control.players = zlm.RuntimePlayers{BootNonce: f.boot, Players: runtimePlayers(players)}
	f.control.sessions = zlm.RuntimeSessions{BootNonce: f.boot, Sessions: runtimeSessions(sessions)}
}

func TestRevocationWorkerUsesExactTargetAndBothFreshSources(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	a := f.seed(t, 1, models.ViewerStateRevokePending)
	b := f.seed(t, 2, models.ViewerStateActive)
	u := f.seed(t, 3, models.ViewerStateActive)
	f.setSnapshot([]string{a.Identifier, b.Identifier, u.Identifier}, []string{a.Identifier, b.Identifier, u.Identifier})

	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 0, result.Closed)
	require.Equal(t, 1, f.control.kickCount())
	f.control.mu.Lock()
	require.Equal(t, []string{a.Identifier}, f.control.kickCalls)
	f.control.mu.Unlock()
	require.Equal(t, models.ViewerStateRevokePending, f.loadViewer(t, a.ID).State)
	require.Equal(t, models.ViewerStateActive, f.loadViewer(t, b.ID).State)
	require.Equal(t, models.ViewerStateActive, f.loadViewer(t, u.ID).State)

	// A player without the matching global session is never kicked.
	f.clock = f.clock.Add(2 * time.Second)
	f.setSnapshot([]string{a.Identifier}, nil)
	result, err = f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.Closed)
	require.Equal(t, 1, f.control.kickCount())
	require.Equal(t, "partial_runtime_snapshot", f.loadViewer(t, a.ID).LastErrorClass)
}

func TestRevocationWorkerAbsenceNeedsIndependentSecondFreshSnapshotAndSurvivesRestart(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 10, models.ViewerStateRevokePending)
	f.setSnapshot([]string{}, []string{})

	first, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, first.Pending)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, "awaiting_late_session", row.LastErrorClass)
	require.NotNil(t, row.RetryAt)

	// A new worker instance uses only durable phase/lease state.
	f.clock = row.RetryAt.UTC().Add(2 * time.Second)
	second, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, second.Closed)
	row = f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateClosed, row.State)
	require.Equal(t, "already_gone", row.LastErrorClass)
	require.Nil(t, row.RetryAt)
}

func TestRevocationWorkerShutdownScheduledClosesOnlyAfterSameBootAbsence(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 11, models.ViewerStateRevokePending)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, "shutdown_scheduled", row.LastErrorClass)

	f.clock = row.RetryAt.UTC().Add(time.Second)
	f.setSnapshot([]string{}, []string{})
	result, err = f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Closed)
	row = f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateClosed, row.State)
	require.Equal(t, "kicked", row.LastErrorClass)
}

func TestRevocationWorkerLostKickResponseNeverClaimsKicked(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 12, models.ViewerStateRevokePending)
	f.control.mu.Lock()
	f.control.kickErr = errors.New("simulated response loss")
	f.control.mu.Unlock()
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, "network_unavailable", row.LastErrorClass)

	f.clock = row.RetryAt.UTC().Add(time.Second)
	f.setSnapshot([]string{}, []string{})
	result, err = f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.Closed)
	require.Equal(t, "awaiting_late_session", f.loadViewer(t, viewer.ID).LastErrorClass)
}

func TestRevocationWorkerFailsClosedForRuntimeTrustAndHookBudget(t *testing.T) {
	cases := []struct {
		name       string
		configure  func(*revocationWorkerFixture)
		alarmCount int
	}{
		{name: "missing resolver", configure: func(f *revocationWorkerFixture) { f.factory.err = errors.New("missing") }},
		{name: "untrusted tls", configure: func(f *revocationWorkerFixture) { f.factory.runtime.Trusted = false }},
		{name: "unknown boot", configure: func(f *revocationWorkerFixture) { f.factory.runtime.CurrentBootNonce = "" }},
		{name: "different boot", configure: func(f *revocationWorkerFixture) {
			f.factory.runtime.CurrentBootNonce = "ffffffffffffffffffffffffffffffff"
		}},
		{name: "zero hook budget", configure: func(f *revocationWorkerFixture) { f.factory.runtime.HookBudget = 0 }},
		{name: "overlong hook budget", alarmCount: 1, configure: func(f *revocationWorkerFixture) { f.factory.runtime.HookBudget = 6 * time.Second }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRevocationWorkerFixture(t, 500*time.Millisecond)
			viewer := f.seed(t, int(revocationWorkerDBID.Add(1)), models.ViewerStateRevokePending)
			tc.configure(f)
			result, err := f.worker(10).Tick(context.Background())
			require.NoError(t, err)
			require.Equal(t, 0, f.control.kickCount())
			require.Equal(t, tc.alarmCount, result.Alarms)
			require.Equal(t, models.ViewerStateRevokePending, f.loadViewer(t, viewer.ID).State)
		})
	}
}

func TestRevocationWorkerRejectsRevokedGrantBindingMismatchWithoutNetwork(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 20, models.ViewerStateRevokePending)
	// The grant tombstone and viewer must carry the same immutable media tuple.
	require.NoError(t, f.db.Model(&models.PlayGrant{}).Where("grant_id = ?", viewer.GrantID).Update("stream", "different-stream").Error)
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.Claimed)
	require.Equal(t, 0, f.control.kickCount())
	require.Equal(t, "binding_mismatch", f.loadViewer(t, viewer.ID).LastErrorClass)

	active := f.seed(t, 21, models.ViewerStateRevokePending)
	require.NoError(t, f.db.Model(&models.PlayGrant{}).Where("grant_id = ?", active.GrantID).Update("state", models.GrantStateBound).Error)
	result, err = f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.Claimed)
	require.Equal(t, "grant_not_revoked", f.loadViewer(t, active.ID).LastErrorClass)
}

func TestRevocationWorkerRejectsUnknownScopeOrProtocolWithoutNetwork(t *testing.T) {
	cases := []struct {
		name     string
		scope    string
		protocol string
	}{
		{name: "unknown scope", scope: "play:live:other", protocol: "https-flv"},
		{name: "unknown protocol", scope: "play:live:apply", protocol: "rtmp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRevocationWorkerFixture(t, 500*time.Millisecond)
			viewer := f.seed(t, int(revocationWorkerDBID.Add(1)), models.ViewerStateRevokePending)
			require.NoError(t, f.db.Model(&models.PlayGrant{}).Where("grant_id = ?", viewer.GrantID).Updates(map[string]any{
				"scope": tc.scope, "protocol": tc.protocol,
			}).Error)
			result, err := f.worker(10).Tick(context.Background())
			require.NoError(t, err)
			require.Equal(t, 0, result.Claimed)
			require.Equal(t, "binding_mismatch", f.loadViewer(t, viewer.ID).LastErrorClass)
			players, sessions := f.control.calls()
			require.Equal(t, 0, players)
			require.Equal(t, 0, sessions)
		})
	}
}

func TestRevocationWorkerNormalizesMicrosecondPersistenceBeforeCAS(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 22, models.ViewerStateRevokePending)
	installMicrosecondPersistence(t, f.db)
	f.clock = f.clock.Add(789 * time.Nanosecond)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})

	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, result.Stale, "a persisted microsecond claim must still CAS successfully")
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, "shutdown_scheduled", row.LastErrorClass)
	require.Equal(t, row.UpdatedAt, normalizeRevocationTime(row.UpdatedAt))
	require.NotNil(t, row.RetryAt)
	require.Equal(t, row.RetryAt.UTC(), normalizeRevocationTime(*row.RetryAt))
}

func TestRevocationWorkerResolverGetsIndependentBoundedContext(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 23, models.ViewerStateRevokePending)
	deadlineSeen := make(chan time.Time, 1)
	f.factory.mu.Lock()
	f.factory.resolveDeadline = deadlineSeen
	f.factory.mu.Unlock()
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	deadline := <-deadlineSeen
	require.Greater(t, deadline.Sub(time.Now()), time.Duration(0))
	require.LessOrEqual(t, deadline.Sub(time.Now()), maximumHookBudget)
	require.Equal(t, "awaiting_late_session", f.loadViewer(t, viewer.ID).LastErrorClass)

	f = newRevocationWorkerFixture(t, 500*time.Millisecond)
	f.seed(t, 24, models.ViewerStateRevokePending)
	resolveStarted := make(chan struct{})
	f.factory.mu.Lock()
	f.factory.resolveStarted = resolveStarted
	f.factory.resolveWaitCancel = true
	f.factory.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.worker(10).Tick(ctx)
		done <- err
	}()
	<-resolveStarted
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func installMicrosecondPersistence(t *testing.T, db *gorm.DB) {
	t.Helper()
	name := fmt.Sprintf("test:truncate_revocation_times_%d", revocationWorkerDBID.Add(1))
	err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		for _, key := range []string{"updated_at", "retry_at"} {
			if value, ok := values[key].(time.Time); ok {
				values[key] = normalizeRevocationTime(value)
			}
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Callback().Update().Remove(name) })
}

func TestRevocationWorkerStaleCompletionCannotApplyLateNetworkResult(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 30, models.ViewerStateRevokePending)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	f.control.mu.Lock()
	f.control.blockPlayers = true
	f.control.playersEntered = make(chan struct{})
	f.control.releasePlayers = make(chan struct{})
	f.control.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := f.worker(10).Tick(context.Background())
		done <- err
	}()
	<-f.control.playersEntered
	require.NoError(t, f.db.Model(&models.Viewer{}).Where("id = ?", viewer.ID).Updates(map[string]any{
		"attempts": 99, "updated_at": f.clock.Add(time.Minute), "last_error_class": "external_update",
	}).Error)
	close(f.control.releasePlayers)
	require.NoError(t, <-done)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, "external_update", row.LastErrorClass)
}

func TestRevocationWorkerReleasesClaimTransactionBeforeNetwork(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 31, models.ViewerStateRevokePending)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	f.control.mu.Lock()
	f.control.blockPlayers = true
	f.control.playersEntered = make(chan struct{})
	f.control.releasePlayers = make(chan struct{})
	f.control.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := f.worker(10).Tick(context.Background())
		done <- err
	}()
	<-f.control.playersEntered
	updated := make(chan error, 1)
	go func() {
		updated <- f.db.Model(&models.Viewer{}).Where("id = ?", viewer.ID).Update("last_error_class", "outside_lock").Error
	}()
	select {
	case err := <-updated:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("claim transaction remained open during network call")
	}
	close(f.control.releasePlayers)
	require.NoError(t, <-done)
}

func TestRevocationWorkerBoundedScanLimit(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewers := []models.Viewer{
		f.seed(t, 40, models.ViewerStateRevokePending),
		f.seed(t, 41, models.ViewerStateRevokePending),
		f.seed(t, 42, models.ViewerStateRevokePending),
	}
	players := make([]string, 0, len(viewers))
	for _, viewer := range viewers {
		players = append(players, viewer.Identifier)
	}
	f.setSnapshot(players, players)
	result, err := f.worker(2).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, result.Scanned)
	require.Equal(t, 2, result.Claimed)
	require.Equal(t, 2, f.control.kickCount())
	require.NotEqual(t, "shutdown_scheduled", f.loadViewer(t, viewers[2].ID).LastErrorClass)
}

func TestRevocationWorkerHandlesNetworkCancellationAsPending(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 50, models.ViewerStateRevokePending)
	f.control.mu.Lock()
	f.control.playersErr = context.Canceled
	f.control.mu.Unlock()
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	require.Equal(t, "network_canceled", f.loadViewer(t, viewer.ID).LastErrorClass)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.worker(10).Tick(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRevocationWorkerTreatsNilSnapshotsAsIncomplete(t *testing.T) {
	f := newRevocationWorkerFixture(t, 500*time.Millisecond)
	viewer := f.seed(t, 60, models.ViewerStateRevokePending)
	f.control.mu.Lock()
	f.control.players = zlm.RuntimePlayers{BootNonce: f.boot, Players: nil}
	f.control.sessions = zlm.RuntimeSessions{BootNonce: f.boot, Sessions: nil}
	f.control.mu.Unlock()
	result, err := f.worker(10).Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Pending)
	require.Equal(t, "snapshot_incomplete", f.loadViewer(t, viewer.ID).LastErrorClass)
}

func runtimePlayers(ids []string) []zlm.MediaPlayer {
	players := make([]zlm.MediaPlayer, 0, len(ids))
	for _, id := range ids {
		players = append(players, zlm.MediaPlayer{Identifier: id})
	}
	return players
}

func runtimeSessions(ids []string) []zlm.Session {
	sessions := make([]zlm.Session, 0, len(ids))
	for _, id := range ids {
		sessions = append(sessions, zlm.Session{ID: id, Identifier: id, Type: "tcp"})
	}
	return sessions
}

func cloneRuntimePlayers(snapshot zlm.RuntimePlayers) zlm.RuntimePlayers {
	if snapshot.Players != nil {
		snapshot.Players = append([]zlm.MediaPlayer{}, snapshot.Players...)
	}
	return snapshot
}

func cloneRuntimeSessions(snapshot zlm.RuntimeSessions) zlm.RuntimeSessions {
	if snapshot.Sessions != nil {
		snapshot.Sessions = append([]zlm.Session{}, snapshot.Sessions...)
	}
	return snapshot
}

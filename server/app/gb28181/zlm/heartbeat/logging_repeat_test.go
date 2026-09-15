package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type repeatTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newRepeatTestClock() *repeatTestClock {
	return &repeatTestClock{now: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}
}

func (c *repeatTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *repeatTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *repeatTestClock) NewTicker(time.Duration) *time.Ticker {
	return time.NewTicker(time.Hour)
}

type repeatLogSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *repeatLogSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *repeatLogSink) Sync() error  { return nil }
func (s *repeatLogSink) Close() error { return nil }

func newRepeatRuntime(t *testing.T, clock *repeatTestClock) (*logging.Runtime, *repeatLogSink) {
	t.Helper()
	cfg := logging.Config{
		Outputs:      []string{"stdout"},
		Level:        zapcore.DebugLevel,
		Modules:      map[string]zapcore.Level{"zlm": zapcore.DebugLevel},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   1,
		MaxAgeDays:   1,
	}
	sink := &repeatLogSink{}
	runtime, err := logging.NewRuntime(logging.Options{
		Config: cfg,
		Sinks:  map[string]zapcore.WriteSyncer{"stdout": sink},
		Clock:  clock,
	})
	require.NoError(t, err)
	oldRuntime, oldLogger := app.LogRuntime, app.ZapLog
	app.LogRuntime, app.ZapLog = runtime, runtime.Root
	t.Cleanup(func() {
		app.LogRuntime, app.ZapLog = oldRuntime, oldLogger
		require.NoError(t, runtime.Close())
	})
	return runtime, sink
}

func repeatRecords(t *testing.T, sink *repeatLogSink) []map[string]any {
	t.Helper()
	sink.mu.Lock()
	data := append([]byte(nil), sink.b.Bytes()...)
	sink.mu.Unlock()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal(line, &record))
		records = append(records, record)
	}
	return records
}

func repeatRecord(records []map[string]any, event, settlement string) (map[string]any, bool) {
	for _, record := range records {
		sourceEvent := record["source_event"]
		if sourceEvent == nil {
			sourceEvent = record["event"]
		}
		if sourceEvent == event && (settlement == "" || record["settlement"] == settlement) {
			return record, true
		}
	}
	return nil, false
}

type repeatRepo struct {
	mu           sync.Mutex
	nextID       int64
	rows         map[int64]node.Node
	updateErrors int
}

func newRepeatRepo() *repeatRepo { return &repeatRepo{rows: make(map[int64]node.Node)} }

func (r *repeatRepo) List(context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		rows = append(rows, n)
	}
	return rows, nil
}

func (r *repeatRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.rows[id]
	if !ok {
		return nil, nil
	}
	copy := n
	return &copy, nil
}

func (r *repeatRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *repeatRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErrors > 0 {
		r.updateErrors--
		return errors.New("database password=fixture-secret")
	}
	r.rows[n.ID] = n
	return nil
}

func (r *repeatRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

func (r *repeatRepo) SetUpdateErrors(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateErrors = count
}

func repeatNode(t *testing.T, reg *node.Registry, clock *repeatTestClock) *node.Node {
	t.Helper()
	added, err := reg.Add(context.Background(), node.Node{
		Name:            "zlm-repeat",
		MediaServerUUID: "repeat-uuid",
		State:           node.StateActive,
	})
	require.NoError(t, err)
	reg.UpdateStats(added.MediaServerUUID, node.Stats{LastHeartbeatAt: clock.Now()})
	return added
}

type repeatFetcher struct {
	netLoad, workLoad float64
	netErr, workErr   error
}

func (f *repeatFetcher) GetThreadsLoad(context.Context, *node.Node) (float64, error) {
	return f.netLoad, f.netErr
}

func (f *repeatFetcher) GetWorkThreadsLoad(context.Context, *node.Node) (float64, error) {
	return f.workLoad, f.workErr
}

func TestLoggingNodeRecovery(t *testing.T) {
	t.Run("watcher failure window and recovery", func(t *testing.T) {
		clock := newRepeatTestClock()
		runtime, sink := newRepeatRuntime(t, clock)
		repo := newRepeatRepo()
		reg := node.NewRegistry(repo)
		target := repeatNode(t, reg, clock)
		watcher := NewWatcher(reg, clock, time.Second, time.Minute)
		repo.SetUpdateErrors(100)
		clock.Advance(61 * time.Second)

		for i := 0; i < 100; i++ {
			watcher.Tick()
		}
		records := repeatRecords(t, sink)
		first, ok := repeatRecord(records, "zlm.node.offline_persist_failed", "")
		require.True(t, ok)
		require.Equal(t, "zlm.node.offline_persist_failed", first["event"])
		require.Equal(t, "Background operation failed", first["message"])
		require.Equal(t, float64(target.ID), first["node_id"])
		require.Len(t, records, 1, "repeated failures emit the first warning immediately")

		require.NoError(t, runtime.Maintain())
		require.Len(t, repeatRecords(t, sink), 1, "window must not close early")
		clock.Advance(time.Minute)
		require.NoError(t, runtime.Maintain())
		records = repeatRecords(t, sink)
		summary, ok := repeatRecord(records, "zlm.node.offline_persist_failed", "window")
		require.True(t, ok)
		require.Equal(t, float64(99), summary["suppressed_count"])

		repo.SetUpdateErrors(0)
		watcher.Tick()
		records = repeatRecords(t, sink)
		require.NotContains(t, fmt.Sprint(records), "fixture-secret")
		recovered, ok := repeatRecord(records, "zlm.node.offline_persist_failed", "recovered")
		require.True(t, ok)
		require.Equal(t, float64(0), recovered["suppressed_count"])
		require.Equal(t, "info", recovered["level"])
		offline, ok := repeatRecord(records, "zlm.node.offline", "")
		require.True(t, ok)
		require.Equal(t, "info", offline["level"])
		state, ok := reg.Get(target.ID)
		require.True(t, ok)
		require.Equal(t, node.StateOffline, state.State)
	})

	t.Run("thread failures recover independently", func(t *testing.T) {
		clock := newRepeatTestClock()
		runtime, sink := newRepeatRuntime(t, clock)
		repo := newRepeatRepo()
		reg := node.NewRegistry(repo)
		target := repeatNode(t, reg, clock)
		fetcher := &repeatFetcher{netLoad: 0.4, workLoad: 0.3}
		poller := NewThreadLoadPoller(reg, fetcher, time.Second)
		n, ok := reg.GetByUUID(target.MediaServerUUID)
		require.True(t, ok)

		fetcher.netErr = errors.New("net password=fixture-secret")
		poller.fetchOne(context.Background(), n)
		state, ok := reg.GetByUUID(target.MediaServerUUID)
		require.True(t, ok)
		require.Zero(t, state.Stats.NetThreadLoadAvg)
		require.Zero(t, state.Stats.WorkThreadLoadAvg)

		fetcher.netErr = nil
		fetcher.workErr = errors.New("work password=fixture-secret")
		poller.fetchOne(context.Background(), n)
		fetcher.workErr = nil
		poller.fetchOne(context.Background(), n)
		state, ok = reg.GetByUUID(target.MediaServerUUID)
		require.True(t, ok)
		require.InDelta(t, 0.4, state.Stats.NetThreadLoadAvg, 0.001)
		require.InDelta(t, 0.3, state.Stats.WorkThreadLoadAvg, 0.001)

		records := repeatRecords(t, sink)
		require.NotContains(t, fmt.Sprint(records), "fixture-secret")
		netRecovery, ok := repeatRecord(records, "zlm.thread_load.net_failed", "recovered")
		require.True(t, ok)
		require.Equal(t, "info", netRecovery["level"])
		workRecovery, ok := repeatRecord(records, "zlm.thread_load.work_failed", "recovered")
		require.True(t, ok)
		require.Equal(t, "info", workRecovery["level"])
		require.Len(t, records, 4, "net/work failures are separate repeated states")
		require.NoError(t, runtime.Maintain())
	})

	t.Run("missing runtime keeps direct warning visible", func(t *testing.T) {
		oldRuntime, oldLogger := app.LogRuntime, app.ZapLog
		core, observed := observer.New(zap.DebugLevel)
		app.LogRuntime = nil
		app.ZapLog = zap.New(core)
		t.Cleanup(func() { app.LogRuntime, app.ZapLog = oldRuntime, oldLogger })

		clock := newRepeatTestClock()
		repo := newRepeatRepo()
		reg := node.NewRegistry(repo)
		repeatNode(t, reg, clock)
		repo.SetUpdateErrors(1)
		clock.Advance(61 * time.Second)
		NewWatcher(reg, clock, time.Second, time.Minute).Tick()

		require.Len(t, observed.All(), 1)
		entry := observed.All()[0]
		require.Equal(t, zap.WarnLevel, entry.Level)
		require.Equal(t, "zlm.node.offline_persist_failed", entry.ContextMap()["event"])
		require.Equal(t, "Background operation failed", entry.Message)
		require.NotContains(t, fmt.Sprint(entry.ContextMap()), "fixture-secret")
	})
}

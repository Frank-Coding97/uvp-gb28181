package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	stdRuntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	t15Callers         = 64
	t15RatePerSecond   = 1000
	t15DurationSeconds = 60
	t15ExpectedRecords = t15RatePerSecond * t15DurationSeconds
	t15MaxBytes        = 64 << 10
	t15MaxBackups      = 8
	t15HeapBudget      = 16 << 20
)

var t15FirstRunHeap atomic.Uint64

type t15TickerClock struct {
	tickers atomic.Uint64
}

func (c *t15TickerClock) Now() time.Time { return time.Now() }
func (c *t15TickerClock) NewTicker(d time.Duration) *time.Ticker {
	c.tickers.Add(1)
	return time.NewTicker(d)
}

type t15ManualClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers atomic.Uint64
}

func newT15ManualClock() *t15ManualClock {
	return &t15ManualClock{now: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
}
func (c *t15ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *t15ManualClock) NewTicker(d time.Duration) *time.Ticker {
	c.tickers.Add(1)
	return time.NewTicker(d)
}
func (c *t15ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type t15JSONSink struct {
	mu      sync.Mutex
	records uint64
	invalid uint64
	next    [t15Callers]uint64
	syncs   atomic.Uint64
	closes  atomic.Uint64
	closed  atomic.Bool
}

type t15LoadRecord struct {
	Event    string `json:"event"`
	Caller   int    `json:"caller"`
	Sequence uint64 `json:"sequence"`
}

func (s *t15JSONSink) Write(p []byte) (int, error) {
	var row t15LoadRecord
	if err := json.Unmarshal(bytes.TrimSpace(p), &row); err != nil {
		s.mu.Lock()
		s.invalid++
		s.mu.Unlock()
		return len(p), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records++
	if row.Event != "logging.load.record" || row.Caller < 0 || row.Caller >= t15Callers || row.Sequence != s.next[row.Caller] {
		s.invalid++
		return len(p), nil
	}
	s.next[row.Caller]++
	return len(p), nil
}
func (s *t15JSONSink) Sync() error {
	s.syncs.Add(1)
	return nil
}
func (s *t15JSONSink) Close() error {
	s.closes.Add(1)
	s.closed.Store(true)
	return nil
}
func (s *t15JSONSink) snapshot() (records, invalid uint64, next [t15Callers]uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records, s.invalid, s.next
}

type t15FaultSink struct {
	writeErr, syncErr, maintainErr, closeErr error
	writes, syncs, maintains, closes         atomic.Uint64
}

func (s *t15FaultSink) Write(p []byte) (int, error) {
	s.writes.Add(1)
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}
func (s *t15FaultSink) Sync() error {
	s.syncs.Add(1)
	return s.syncErr
}
func (s *t15FaultSink) Maintain() error {
	s.maintains.Add(1)
	return s.maintainErr
}
func (s *t15FaultSink) Close() error {
	s.closes.Add(1)
	return s.closeErr
}

type t15CountingSink struct {
	forbidden []byte
	writes    atomic.Uint64
	syncs     atomic.Uint64
	closes    atomic.Uint64
	leaks     atomic.Uint64
}

func (s *t15CountingSink) Write(p []byte) (int, error) {
	if len(s.forbidden) > 0 && bytes.Contains(p, s.forbidden) {
		s.leaks.Add(1)
	}
	s.writes.Add(1)
	return len(p), nil
}
func (s *t15CountingSink) Sync() error {
	s.syncs.Add(1)
	return nil
}
func (s *t15CountingSink) Close() error {
	s.closes.Add(1)
	return nil
}

type t15BlockingSink struct {
	entered, release chan struct{}
	once             sync.Once
	writes, closes   atomic.Uint64
}

func (s *t15BlockingSink) Write(p []byte) (int, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	s.writes.Add(1)
	return len(p), nil
}
func (s *t15BlockingSink) Sync() error { return nil }
func (s *t15BlockingSink) Close() error {
	s.closes.Add(1)
	return nil
}

func TestLoggingLoad(t *testing.T) {
	r, stdout, path, clock := newT15LoadRuntime(t)
	before := t15LiveHeap()
	elapsed := runT15Load(t, r.Root)
	if elapsed < 59*time.Second {
		t.Fatalf("load ended too early: %s", elapsed)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	assertT15LoadOutput(t, r, stdout, path)
	assertT15MaintenanceClosed(t, r, clock)
	after := t15LiveHeap()
	if delta := t15HeapDelta(after, before); delta > t15HeapBudget {
		t.Fatalf("one load retained %d bytes, budget %d", delta, t15HeapBudget)
	}
	t15FirstRunHeap.Store(after)
	t.Logf("load records=%d elapsed=%s heap_before=%d heap_after=%d heap_delta=%d", t15ExpectedRecords, elapsed, before, after, t15HeapDelta(after, before))
}

func TestLoggingResourceBounds(t *testing.T) {
	t.Run("load_repeat", func(t *testing.T) {
		r, stdout, path, clock := newT15LoadRuntime(t)
		before := t15LiveHeap()
		prior := t15FirstRunHeap.Load()
		elapsed := runT15Load(t, r.Root)
		mid := t15LiveHeap()
		if delta := t15HeapDelta(mid, before); delta > t15HeapBudget {
			t.Fatalf("load retained %d bytes before close, budget %d", delta, t15HeapBudget)
		}
		if prior != 0 && t15HeapDelta(mid, prior) > t15HeapBudget {
			t.Fatalf("repeat retained %d bytes over first closed run, budget %d", t15HeapDelta(mid, prior), t15HeapBudget)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		assertT15LoadOutput(t, r, stdout, path)
		assertT15MaintenanceClosed(t, r, clock)
		after := t15LiveHeap()
		if delta := t15HeapDelta(after, before); delta > t15HeapBudget {
			t.Fatalf("repeat retained %d bytes after close, budget %d", delta, t15HeapBudget)
		}
		if prior != 0 && t15HeapDelta(after, prior) > t15HeapBudget {
			t.Fatalf("repeat closed heap grew %d bytes over first run, budget %d", t15HeapDelta(after, prior), t15HeapBudget)
		}
		if elapsed < 59*time.Second {
			t.Fatalf("load ended too early: %s", elapsed)
		}
		t.Logf("repeat records=%d elapsed=%s heap_before=%d heap_after=%d first_closed_heap=%d repeat_delta=%d", t15ExpectedRecords, elapsed, before, after, prior, t15HeapDelta(after, prior))
	})

	t.Run("sink_write", func(t *testing.T) {
		const secret = "write-failure-secret"
		clock := newT15ManualClock()
		emergency := &memorySink{}
		sink := &t15FaultSink{writeErr: errors.New("write failed: " + secret)}
		r := newT15FaultRuntime(t, sink, emergency, clock)
		for i := 0; i < 1000; i++ {
			r.Root.Info("fault record", zap.String("event", "logging.test.fault"))
		}
		stats := r.Stats()["stdout"]
		if stats.Attempted != 1000 || stats.Written != 0 || stats.Failed != 1000 {
			t.Fatalf("write stats: %+v", stats)
		}
		if rows := records(t, emergency); len(rows) != 1 {
			t.Fatalf("write emergency rate: %d", len(rows))
		}
		clock.Advance(time.Minute)
		if err := r.Maintain(); err != nil {
			t.Fatalf("maintain flush: %v", err)
		}
		rows := records(t, emergency)
		if len(rows) != 2 || rows[1]["failed"] != float64(1000) {
			t.Fatalf("write failure count: %v", rows)
		}
		if strings.Contains(emergency.String(), secret) {
			t.Fatal("write error leaked")
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("sink_sync", func(t *testing.T) {
		const secret = "sync-failure-secret"
		clock := newT15ManualClock()
		emergency := &memorySink{}
		sink := &t15FaultSink{syncErr: errors.New("sync failed: " + secret)}
		r := newT15FaultRuntime(t, sink, emergency, clock)
		for i := 0; i < 100; i++ {
			r.Root.Info("sync record", zap.String("event", "logging.test.fault"))
			if err := r.Root.Sync(); err == nil {
				t.Fatal("sync failure hidden")
			}
		}
		stats := r.Stats()["stdout"]
		if stats.Attempted != 100 || stats.Written != 100 || stats.Failed != 0 || stats.DurabilityUnknown != 100 {
			t.Fatalf("sync stats: %+v", stats)
		}
		if sink.syncs.Load() != 100 {
			t.Fatalf("sync calls: %d", sink.syncs.Load())
		}
		if rows := records(t, emergency); len(rows) != 1 {
			t.Fatalf("sync emergency rate: %d", len(rows))
		}
		clock.Advance(time.Minute)
		if err := r.Maintain(); err != nil {
			t.Fatalf("sync summary flush: %v", err)
		}
		rows := records(t, emergency)
		if len(rows) != 2 || rows[1]["durability_unknown"] != float64(100) {
			t.Fatalf("sync durability summary: %v", rows)
		}
		if strings.Contains(emergency.String(), secret) {
			t.Fatal("sync error leaked")
		}
		if err := r.Close(); err == nil {
			t.Fatal("close hid sync failure")
		}
		if stats := r.Stats()["stdout"]; stats.DurabilityUnknown != 101 || sink.syncs.Load() != 101 {
			t.Fatalf("close sync accounting: %+v calls=%d", stats, sink.syncs.Load())
		}
	})

	t.Run("sink_maintain", func(t *testing.T) {
		const secret = "maintain-failure-secret"
		clock := newT15ManualClock()
		emergency := &memorySink{}
		sink := &t15FaultSink{maintainErr: errors.New("maintain failed: " + secret)}
		r := newT15FaultRuntime(t, sink, emergency, clock)
		for i := 0; i < 100; i++ {
			if err := r.Maintain(); err == nil {
				t.Fatal("maintain failure hidden")
			}
		}
		if sink.maintains.Load() != 100 {
			t.Fatalf("maintain calls: %d", sink.maintains.Load())
		}
		if rows := records(t, emergency); len(rows) != 1 {
			t.Fatalf("maintain emergency rate: %d", len(rows))
		}
		// Stop injecting after the measured 100 failures so the next Maintain
		// only flushes the one-minute emergency summary.
		sink.maintainErr = nil
		clock.Advance(time.Minute)
		if err := r.Maintain(); err != nil {
			t.Fatalf("maintain flush: %v", err)
		}
		rows := records(t, emergency)
		if len(rows) != 2 || rows[1]["maintenance_failed"] != float64(100) || rows[1]["failed"] != float64(0) {
			t.Fatalf("maintain failure count: %v", rows)
		}
		if stats := r.Stats()["stdout"]; stats.MaintenanceFailed != 100 || stats.Failed != 0 || stats.Attempted != 0 {
			t.Fatalf("maintenance mixed with record accounting: %+v", stats)
		}
		if strings.Contains(emergency.String(), secret) {
			t.Fatal("maintain error leaked")
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("repeater_bounds", func(t *testing.T) {
		const secret = "repeat-failure-secret"
		sink := &t15CountingSink{forbidden: []byte(secret)}
		r := newT15CountingRuntime(t, sink)
		for i := int64(0); i <= 4096; i++ {
			r.Repeats().Fail(RepeatKey{Component: "load", Event: "logging.test.repeat", NodeID: i}, errors.New("failure: "+secret))
		}
		if got := r.Repeats().Len(); got != 4096 {
			t.Fatalf("repeater state bound: %d", got)
		}
		if got := sink.writes.Load(); got != 4098 {
			t.Fatalf("repeater first/capacity writes: %d", got)
		}
		if sink.leaks.Load() != 0 {
			t.Fatal("repeater error leaked")
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if got := r.Repeats().Len(); got != 0 {
			t.Fatalf("repeater state after close: %d", got)
		}
		if got := sink.writes.Load(); got != 8194 {
			t.Fatalf("repeater close writes: %d", got)
		}
	})

	t.Run("blocking_sink", func(t *testing.T) {
		sink := &t15BlockingSink{entered: make(chan struct{}), release: make(chan struct{})}
		r := newT15CountingRuntime(t, sink)
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(sink.release) }) }
		// If an assertion fails while Write is blocked, release before the
		// runtime cleanup calls Close; otherwise cleanup would wait forever.
		t.Cleanup(release)
		returned := make(chan struct{})
		go func() {
			r.Root.Info("blocking record", zap.String("event", "logging.test.blocking"))
			close(returned)
		}()
		select {
		case <-sink.entered:
		case <-time.After(time.Second):
			t.Fatal("synchronous sink was not entered")
		}
		select {
		case <-returned:
			t.Fatal("caller returned while sink was blocked")
		case <-time.After(50 * time.Millisecond):
		}
		closed := make(chan struct{})
		go func() {
			_ = r.Close()
			close(closed)
		}()
		select {
		case <-closed:
			t.Fatal("Close returned while sink was blocked")
		case <-time.After(50 * time.Millisecond):
		}
		release()
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("caller did not return after sink release")
		}
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("Close did not complete after sink release")
		}
		if sink.writes.Load() != 1 || sink.closes.Load() != 1 {
			t.Fatalf("blocking sink calls: writes=%d closes=%d", sink.writes.Load(), sink.closes.Load())
		}
	})
}

func newT15LoadRuntime(t *testing.T) (*Runtime, *t15JSONSink, string, *t15TickerClock) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uvp-load.log")
	file, err := openFileWriter(writerOptions{
		path:       path,
		maxBytes:   t15MaxBytes,
		maxBackups: t15MaxBackups,
		maxAge:     24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	stdout := &t15JSONSink{}
	clock := &t15TickerClock{}
	cfg := Config{
		Outputs:      []string{"file", "stdout"},
		Level:        zap.InfoLevel,
		Modules:      map[string]zapcore.Level{"load": zap.InfoLevel},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   t15MaxBackups,
		MaxAgeDays:   1,
	}
	r, err := NewRuntime(Options{
		Config:   cfg,
		Service:  "logging-load-test",
		Version:  "test",
		Instance: "load",
		Clock:    clock,
		Sinks: map[string]zapcore.WriteSyncer{
			"file":   file,
			"stdout": stdout,
		},
	})
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, stdout, path, clock
}

func newT15FaultRuntime(t *testing.T, sink *t15FaultSink, emergency *memorySink, clock *t15ManualClock) *Runtime {
	t.Helper()
	cfg := t15SingleSinkConfig()
	r, err := NewRuntime(Options{Config: cfg, Clock: clock, ErrorOutput: emergency, Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func newT15CountingRuntime(t *testing.T, sink zapcore.WriteSyncer) *Runtime {
	t.Helper()
	cfg := t15SingleSinkConfig()
	r, err := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func t15SingleSinkConfig() Config {
	return Config{
		Outputs:      []string{"stdout"},
		Level:        zap.InfoLevel,
		Modules:      map[string]zapcore.Level{"test": zap.InfoLevel},
		FileFormat:   "json",
		StdoutFormat: "json",
		MaxSizeMB:    1,
		MaxBackups:   1,
		MaxAgeDays:   1,
	}
}

func runT15Load(t *testing.T, logger *zap.Logger) time.Duration {
	t.Helper()
	type job struct{ sequence uint64 }
	jobs := make([]chan job, t15Callers)
	ready := make(chan struct{}, t15Callers)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(t15Callers)
	for caller := 0; caller < t15Callers; caller++ {
		jobs[caller] = make(chan job, 64)
		go func(caller int) {
			defer workers.Done()
			ready <- struct{}{}
			<-start
			for item := range jobs[caller] {
				logger.Info("logging load record",
					zap.String("event", "logging.load.record"),
					zap.Int("caller", caller),
					zap.Uint64("sequence", item.sequence))
			}
		}(caller)
	}
	for i := 0; i < t15Callers; i++ {
		<-ready
	}
	close(start)

	started := time.Now()
	for second := 0; second < t15DurationSeconds; second++ {
		for i := 0; i < t15RatePerSecond; i++ {
			index := second*t15RatePerSecond + i
			caller := index % t15Callers
			jobs[caller] <- job{sequence: uint64(index / t15Callers)}
		}
		until := started.Add(time.Duration(second+1) * time.Second)
		if wait := time.Until(until); wait > 0 {
			time.Sleep(wait)
		}
	}
	for _, queue := range jobs {
		close(queue)
	}
	workers.Wait()
	return time.Since(started)
}

func assertT15LoadOutput(t *testing.T, r *Runtime, stdout *t15JSONSink, path string) {
	t.Helper()
	stats := r.Stats()
	for _, target := range []string{"file", "stdout"} {
		got := stats[target]
		if got.Attempted != t15ExpectedRecords || got.Written != t15ExpectedRecords || got.Failed != 0 {
			t.Fatalf("%s stats: %+v", target, got)
		}
	}
	records, invalid, next := stdout.snapshot()
	if records != t15ExpectedRecords || invalid != 0 {
		t.Fatalf("stdout records=%d invalid=%d", records, invalid)
	}
	for caller := 0; caller < t15Callers; caller++ {
		want := t15ExpectedRecords / t15Callers
		if caller < t15ExpectedRecords%t15Callers {
			want++
		}
		if next[caller] != uint64(want) {
			t.Fatalf("caller %d sequence count=%d want=%d", caller, next[caller], want)
		}
	}
	fileCount, retainedBytes, retainedRecords, invalidFiles := readT15ManagedFiles(t, path)
	if fileCount < 2 || fileCount > t15MaxBackups+1 {
		t.Fatalf("managed file count=%d want rotation and <=%d", fileCount, t15MaxBackups+1)
	}
	if retainedBytes > int64(t15MaxBytes)*(t15MaxBackups+1) || retainedRecords == 0 || invalidFiles != 0 {
		t.Fatalf("retained files bytes=%d records=%d invalid=%d", retainedBytes, retainedRecords, invalidFiles)
	}
	if retainedRecords >= t15ExpectedRecords {
		t.Fatalf("retention did not separate retained records from writes: %d", retainedRecords)
	}
	if stdout.syncs.Load() == 0 || stdout.closes.Load() != 1 || !stdout.closed.Load() {
		t.Fatalf("stdout lifecycle sync=%d close=%d closed=%v", stdout.syncs.Load(), stdout.closes.Load(), stdout.closed.Load())
	}
}

func assertT15MaintenanceClosed(t *testing.T, r *Runtime, clock *t15TickerClock) {
	t.Helper()
	if clock.tickers.Load() != 1 {
		t.Fatalf("maintenance tickers=%d", clock.tickers.Load())
	}
	select {
	case <-r.maintenanceDone:
	default:
		t.Fatal("maintenance goroutine not done after Close")
	}
}

func readT15ManagedFiles(t *testing.T, path string) (count int, totalBytes int64, records, invalid int) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(path)
	for _, entry := range entries {
		name := entry.Name()
		if name != base && !strings.HasPrefix(name, base+".uvp-v1-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		count++
		totalBytes += info.Size()
		file, err := os.Open(filepath.Join(filepath.Dir(path), name))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1024), 128<<10)
		for scanner.Scan() {
			if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
				continue
			}
			records++
			if !json.Valid(scanner.Bytes()) {
				invalid++
			}
		}
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return count, totalBytes, records, invalid
}

func t15LiveHeap() uint64 {
	stdRuntime.GC()
	var stats stdRuntime.MemStats
	stdRuntime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func t15HeapDelta(after, before uint64) uint64 {
	if after <= before {
		return 0
	}
	return after - before
}

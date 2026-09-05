package logging

import (
	"errors"
	"go.uber.org/zap/zapcore"
	"strings"
	"sync"
	"testing"
	"time"
)

type faultySink struct {
	err    error
	writes int
}

func (f *faultySink) Write(p []byte) (int, error) { f.writes++; return 0, f.err }
func (f *faultySink) Sync() error                 { return f.err }
func TestLoggingSinkFailure(t *testing.T) {
	cfg := configForTest(t, values{"logs.outputs": []string{"file", "stdout"}, "logs.textformat": "json", "logs.stdoutformat": "json"})
	good, emergency := &memorySink{}, &memorySink{}
	bad := &faultySink{err: errors.New(testSecret)}
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	r, e := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"file": bad, "stdout": good}, ErrorOutput: emergency, Clock: fixedClock{now}})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		r.Root.Info("event")
	}
	stats := r.Stats()
	if stats["file"].Attempted != 100 || stats["file"].Failed != 100 || stats["stdout"].Written != 100 {
		t.Errorf("sink stats %+v", stats)
	}
	if len(records(t, good)) != 100 {
		t.Error("healthy output duplicated or lost")
	}
	if strings.Contains(emergency.String(), testSecret) {
		t.Error("error output leaked raw failure")
	}
	if len(records(t, emergency)) != 1 {
		t.Errorf("emergency not limited: %d records", len(records(t, emergency)))
	}
	if e := r.Close(); e == nil {
		t.Error("Close hid sync failure")
	}
	if r.Stats()["file"].DurabilityUnknown == 0 {
		t.Error("sync durability unknown not counted")
	}
}
func TestLoggingSinkFailureEmergency(t *testing.T) {
	cfg := configForTest(t, values{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"})
	bad := &faultySink{err: errors.New(testSecret)}
	r, e := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": bad}, ErrorOutput: &faultySink{err: errors.New("stderr failed")}})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		r.Root.Info("event")
	}
	if r.Stats()["stdout"].Failed != 100 {
		t.Fatal("lost failure counters")
	}
	if e = r.Close(); e == nil {
		t.Fatal("stderr failure hidden")
	}
}

type blockingSink struct {
	entered, release chan struct{}
	once             sync.Once
	closed           bool
	mu               sync.Mutex
}

func (b *blockingSink) Write(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, errors.New("closed before write completed")
	}
	return len(p), nil
}
func (b *blockingSink) Sync() error  { return nil }
func (b *blockingSink) Close() error { b.mu.Lock(); defer b.mu.Unlock(); b.closed = true; return nil }
func TestLoggingSinkFailureConcurrentClose(t *testing.T) {
	cfg := configForTest(t, values{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"})
	b := &blockingSink{entered: make(chan struct{}), release: make(chan struct{})}
	r, e := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": b}})
	if e != nil {
		t.Fatal(e)
	}
	written := make(chan struct{})
	go func() { r.Root.Info("event"); close(written) }()
	<-b.entered
	closed := make(chan struct{})
	go func() { _ = r.Close(); close(closed) }()
	select {
	case <-closed:
		t.Error("runtime closed an active sink")
	case <-time.After(20 * time.Millisecond):
	}
	close(b.release)
	<-written
	<-closed
}

type adjustableClock struct{ fixedClock }

func (c *adjustableClock) Now() time.Time { return c.now }
func TestLoggingSinkFailureWindow(t *testing.T) {
	clock := &adjustableClock{fixedClock{}}
	cfg := configForTest(t, values{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"})
	out := &memorySink{}
	r, e := NewRuntime(Options{Config: cfg, Clock: clock, Sinks: map[string]zapcore.WriteSyncer{"stdout": &faultySink{err: errors.New("write failed")}}, ErrorOutput: out})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	for i := 0; i < 100; i++ {
		r.Root.Info("event")
	}
	if n := len(records(t, out)); n != 1 {
		t.Fatalf("initial window emitted %d records", n)
	}
	clock.now = clock.now.Add(time.Minute)
	if e = r.Maintain(); e != nil {
		t.Fatal(e)
	}
	rows := records(t, out)
	if len(rows) != 2 || rows[1]["failed"] != float64(100) {
		t.Fatalf("window counts: %v", rows)
	}
}

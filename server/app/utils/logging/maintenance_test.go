package logging

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"go.uber.org/zap/zapcore"
)

type maintenanceClock struct{ tickers atomic.Int64 }

func (c *maintenanceClock) Now() time.Time { return time.Now() }
func (c *maintenanceClock) NewTicker(d time.Duration) *time.Ticker {
	if d != time.Minute {
		panic("unexpected maintenance interval")
	}
	c.tickers.Add(1)
	return time.NewTicker(time.Millisecond)
}

type maintenanceSink struct{ maintained atomic.Int64 }

func (*maintenanceSink) Write(p []byte) (int, error) { return len(p), nil }
func (*maintenanceSink) Sync() error                 { return nil }
func (s *maintenanceSink) Maintain() error           { s.maintained.Add(1); return nil }
func TestLoggingRepeatSharedMaintenance(t *testing.T) {
	clock := &maintenanceClock{}
	sink := &maintenanceSink{}
	cfg := configForTest(t, values{"logs.outputs": []string{"stdout"}})
	r, err := NewRuntime(Options{Config: cfg, Clock: clock, Sinks: map[string]zapcore.WriteSyncer{"stdout": sink}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	deadline := time.Now().Add(time.Second)
	for sink.maintained.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sink.maintained.Load() == 0 {
		t.Fatal("idle runtime never maintained its sink")
	}
	if clock.tickers.Load() != 1 {
		t.Fatalf("maintenance tickers: %d", clock.tickers.Load())
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	n := sink.maintained.Load()
	time.Sleep(5 * time.Millisecond)
	if sink.maintained.Load() != n {
		t.Fatal("maintenance continued after Close")
	}
}
func TestLoggingRepeatFixedStateBudget(t *testing.T) {
	if n := unsafe.Sizeof(repeatKey{}) + unsafe.Sizeof(repeatState{}); n > 512 {
		t.Fatalf("state costs %d bytes", n)
	}
	r, _, _ := repeatFixture()
	// Differing long identifiers must not collide or retain input backing arrays.
	for _, suffix := range []string{"a", "b"} {
		r.Fail(RepeatKey{Component: "zlm", Event: "poll", JobID: string(make([]byte, 1<<20)) + suffix}, context.Canceled)
	}
	if r.Len() != 2 {
		t.Fatal("long identifier collision")
	}
}

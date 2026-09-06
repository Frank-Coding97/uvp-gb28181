package logging

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func repeatFixture() (*Repeater, *observer.ObservedLogs, *time.Time) {
	core, rows := observer.New(zap.DebugLevel)
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	return newRepeater(zap.New(core), func() time.Time { return now }), rows, &now
}
func TestLoggingRepeatWindow(t *testing.T) {
	r, rows, now := repeatFixture()
	key := RepeatKey{Component: "zlm", Event: "node.failed", NodeID: 7}
	for i := 0; i < 100; i++ {
		r.Fail(key, errors.New("secret raw failure"))
	}
	if rows.Len() != 1 {
		t.Fatalf("first failure must be immediate, got %d", rows.Len())
	}
	*now = now.Add(time.Minute - time.Nanosecond)
	r.maintain()
	if rows.Len() != 1 {
		t.Fatal("window closed early")
	}
	*now = now.Add(time.Nanosecond)
	r.maintain()
	if rows.Len() != 2 {
		t.Fatalf("missing window summary: %d", rows.Len())
	}
	fields := rows.All()[1].ContextMap()
	if fields["suppressed_count"] != uint64(99) || fields["settlement"] != "window" {
		t.Fatalf("wrong summary: %v", fields)
	}
	if fields["first_seen"] != rows.All()[0].ContextMap()["first_seen"] {
		t.Fatal("first occurrence lost")
	}
	if strings.Contains(fmt.Sprint(rows.All()), "secret raw failure") {
		t.Fatal("raw error leaked")
	}
}
func TestLoggingRepeatRecovery(t *testing.T) {
	r, rows, now := repeatFixture()
	key := RepeatKey{Component: "zlm", Event: "node.failed", NodeID: 1}
	r.Fail(key, context.DeadlineExceeded)
	r.Fail(key, context.DeadlineExceeded)
	*now = now.Add(time.Second)
	r.Recovered(key)
	if rows.Len() != 2 || r.Len() != 0 {
		t.Fatalf("recovery not settled: %d/%d", rows.Len(), r.Len())
	}
	f := rows.All()[1].ContextMap()
	if f["suppressed_count"] != uint64(1) || f["event"] != "logging.repeat_recovered" || rows.All()[1].Level != zap.InfoLevel {
		t.Fatalf("bad recovery: %v", f)
	}
	r.Recovered(key)
	if rows.Len() != 2 {
		t.Fatal("duplicate recovery")
	}
	r.Fail(key, context.DeadlineExceeded)
	if rows.Len() != 3 {
		t.Fatal("new failure must be immediate")
	}
}
func TestLoggingRepeatIsolation(t *testing.T) {
	r, rows, _ := repeatFixture()
	keys := []RepeatKey{{Component: "zlm", Event: "net.failed", NodeID: 1}, {Component: "zlm", Event: "work.failed", NodeID: 1}, {Component: "zlm", Event: "net.failed", NodeID: 2}, {Component: "job", Event: "sync.failed", JobID: "a"}, {Component: "job", Event: "sync.failed", JobID: "b"}}
	for _, k := range keys {
		r.Fail(k, context.Canceled)
	}
	r.Fail(keys[0], context.DeadlineExceeded)
	if r.Len() != 6 || rows.Len() != 6 {
		t.Fatalf("keys collided: %d/%d", r.Len(), rows.Len())
	}
	r.Recovered(keys[0])
	if r.Len() != 4 || rows.Len() != 8 {
		t.Fatal("recovery must settle only matching object/event classes")
	}
}
func TestLoggingRepeatBoundsAndSettlement(t *testing.T) {
	r, rows, now := repeatFixture()
	key := RepeatKey{Component: "zlm", Event: "net.failed"}
	r.Fail(key, context.Canceled)
	r.Fail(key, context.Canceled)
	for i := 1; i <= 4096; i++ {
		k := key
		k.NodeID = int64(i)
		r.Fail(k, context.Canceled)
	}
	if r.Len() != 4096 {
		t.Fatalf("state limit: %d", r.Len())
	}
	summaries := rows.FilterField(zap.String("settlement", "capacity"))
	if summaries.Len() != 1 || summaries.All()[0].ContextMap()["suppressed_count"] != uint64(1) {
		t.Fatal("eviction lost unsettled count")
	}
	*now = now.Add(10 * time.Minute)
	r.maintain()
	if r.Len() != 0 || rows.FilterField(zap.String("settlement", "idle")).Len() != 4096 {
		t.Fatal("idle states were not settled")
	}
	r.Fail(key, context.Canceled)
	r.Fail(key, context.Canceled)
	r.close()
	r.close()
	if r.Len() != 0 || rows.FilterField(zap.String("settlement", "close")).Len() != 1 {
		t.Fatal("close must settle once")
	}
	n := rows.Len()
	r.Fail(key, context.Canceled)
	r.Recovered(key)
	r.maintain()
	if rows.Len() != n {
		t.Fatal("closed repeater accepted new state")
	}
}
func TestLoggingRepeatDoesNotSampleOtherEvents(t *testing.T) {
	r, rows, _ := repeatFixture()
	for i := 0; i < 100; i++ {
		r.Fail(RepeatKey{Component: "zlm", Event: "net.failed", NodeID: 1}, context.Canceled)
		for _, event := range []string{"http.access", "audit.persisted", "operation.failed"} {
			r.logger.Info("event completed", zap.String("event", event))
		}
	}
	for _, event := range []string{"http.access", "audit.persisted", "operation.failed"} {
		if rows.FilterField(zap.String("event", event)).Len() != 100 {
			t.Fatalf("ordinary events sampled: %s", event)
		}
	}
}

func TestLoggingRepeatRuntimeCloseFlushesBeforeSink(t *testing.T) {
	runtime, sink := runtimeForTest(t, nil)
	key := RepeatKey{Component: "zlm", Event: "poll.failed", NodeID: 9}
	for i := 0; i < 100; i++ {
		runtime.Repeats().Fail(key, context.Canceled)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	rows := records(t, sink)
	if len(rows) != 2 || rows[1]["settlement"] != "close" || rows[1]["suppressed_count"] != float64(99) {
		t.Fatalf("shutdown summary missing: %v", rows)
	}
	if sink.closes != 1 || runtime.Repeats().Len() != 0 {
		t.Fatal("runtime resources not closed")
	}
}
func TestLoggingRepeatConcurrentCloseAndMaintain(t *testing.T) {
	runtime, _ := runtimeForTest(t, nil)
	done := make(chan struct{}, 33)
	for i := 0; i < 32; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for n := 0; n < 100; n++ {
				key := RepeatKey{Component: "zlm", Event: "poll.failed", NodeID: int64(id)}
				runtime.Repeats().Fail(key, context.Canceled)
				_ = runtime.Maintain()
			}
		}(i)
	}
	go func() { _ = runtime.Close(); done <- struct{}{} }()
	deadline := time.After(5 * time.Second)
	for i := 0; i < 33; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatal("maintenance/Close deadlocked")
		}
	}
	if runtime.Repeats().Len() != 0 {
		t.Fatal("closed state leaked")
	}
}

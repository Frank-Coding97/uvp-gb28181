package logging

import (
	"context"
	"go.uber.org/zap"
	"testing"
)

func TestLoggingShutdownSettlesBeforeFinalStatus(t *testing.T) {
	runtime, sink := runtimeForTest(t, nil)
	key := RepeatKey{Component: "zlm", Event: "poll.failed", NodeID: 7}
	for i := 0; i < 100; i++ {
		runtime.Repeats().Fail(key, context.Canceled)
	}
	runtime.Repeats().Close()
	runtime.Root.Named("lifecycle").Info("Application work stopped", zap.String("event", "lifecycle.stopped"))
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.Repeats().Close()
	rows := records(t, sink)
	if len(rows) != 3 || rows[1]["settlement"] != "close" || rows[1]["suppressed_count"] != float64(99) || rows[2]["event"] != "lifecycle.stopped" {
		t.Fatalf("settlement must precede final status without duplicate: %v", rows)
	}
	if sink.closes != 1 {
		t.Fatalf("sink closed %d times", sink.closes)
	}
}

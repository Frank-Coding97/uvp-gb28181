package logging

import (
	"bytes"
	"errors"
	"go.uber.org/zap"
	"log"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

func TestLoggingBootstrapFailure(t *testing.T) {
	var early bytes.Buffer
	ReportStartupFailure(&early, nil, "config", errors.New("password=fixture-secret"))
	if !strings.Contains(early.String(), "startup.failed") || strings.Contains(early.String(), "fixture-secret") {
		t.Fatalf("invalid safe early output: %q", early.String())
	}
	r, sink := runtimeForTest(t, nil)
	ReportStartupFailure(&early, r.Root, "database", errors.New("password=fixture-secret"))
	rows := records(t, sink)
	if len(rows) != 1 || rows[0]["event"] != "startup.failed" || rows[0]["phase"] != "database" {
		t.Fatalf("missing dependency failure: %v", rows)
	}
}
func TestLoggingBootstrapSourcesAndReload(t *testing.T) {
	r, sink := runtimeForTest(t, nil)
	restore := InstallStandardBridge(r.Root.Named("stdlib"))
	defer restore()
	r.Root.Info("business operation", zap.String("event", "business.test"))
	log.Print("secret fixture-secret")
	slog.New(NewSlogHandler(r.Root.Named("sipgo"))).Info("SIP INVITE fixture-secret")
	rows := records(t, sink)
	if len(rows) != 3 {
		t.Fatalf("sources: %d", len(rows))
	}
	if strings.Contains(sink.String(), "fixture-secret") {
		t.Fatal("legacy source leaked")
	}
	before := runtime.NumGoroutine()
	for i := 0; i < 1000; i++ {
		r.Root.Info("bounded operation")
	}
	if runtime.NumGoroutine() > before {
		t.Fatal("per-entry goroutine")
	}
	cfg := r.Config()
	cfg.Level = zap.WarnLevel
	r.NoticeReload(cfg)
	r.NoticeReload(cfg)
	if strings.Count(sink.String(), `"event":"logging.restart_required"`) != 1 {
		t.Fatal("reload notice not once")
	}
	r.Root.Info("still using original snapshot", zap.String("event", "snapshot.test"))
	if !strings.Contains(sink.String(), "snapshot.test") {
		t.Fatal("reload changed sink or level")
	}
}

package logging

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// The sanitizer is a security boundary, so every relaxation is a contract: what
// the core now retains, what it still refuses, and how a retained value stays
// bounded and single-line. See
// docs/logging-governance/contracts/sanitize-policy.md.
//
// Why "reason" specifically: it is a controlled diagnostic code at every call
// site, and omitting it hid the answer on the highest-volume WARN in the tree
// (ptz.response.unmatched) — the log file carried ~600 "[text omitted]" lines
// with no way to tell why the response could not be matched.

func TestLoggingSanitizeReasonRetained(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	r.Root.Named("ptz").Warn("GB28181 PTZ 应答无法唯一关联",
		zap.String("event", "ptz.response.unmatched"),
		zap.String("reason", "no_candidate"))
	rows := records(t, b)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	if got := rows[0]["reason"]; got != "no_candidate" {
		t.Fatalf("reason not retained: %v", got)
	}
	if strings.Contains(b.String(), "[text omitted]") {
		t.Fatal("reason still rendered as omitted text")
	}
}

// A retained value must never be able to split a record across lines, and must
// stay readable in the operator-facing console form rather than in JSON.
func TestLoggingSanitizeReasonStaysSingleLineInConsole(t *testing.T) {
	sink := &memorySink{}
	c := configForTest(t, values{"logs.outputs": []string{"file"}, "logs.textformat": "console", "logs.level": "info"})
	r, err := NewRuntime(Options{
		Config: c, Service: "uvp", Version: "v1", Instance: "node",
		Sinks: map[string]zapcore.WriteSyncer{"file": sink},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Root.Named("ptz").Warn("GB28181 PTZ 应答无法唯一关联",
		zap.String("event", "ptz.response.unmatched"),
		zap.String("reason", "no_candidate\nsecond line"),
		zap.String("candidateOperationIds", "operation-1,operation-2"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(sink.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("record spans multiple lines:\n%s", line)
	}
	for _, want := range []string{
		`reason="no_candidate\nsecond line"`,
		"candidateOperationIds=operation-1,operation-2",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("console line missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "omitted") {
		t.Fatalf("console line still hides content:\n%s", line)
	}
}

// Removing reason from the omit list must not weaken the guard it sat next to.
// These keys are error-derived and have a structured alternative (logging.Error),
// so the string form stays omitted — including under mixed-case keys.
func TestLoggingSanitizeErrorDerivedGuardsIntact(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	r.Root.Info("guarded",
		zap.String("event", "guard.check"),
		zap.String("error", "raw error text"),
		zap.String("err", "raw err text"),
		zap.String("Error", "mixed case key"),
		zap.String("panic", "panic text"),
		zap.String("recover", "recover text"),
		zap.String("detail", "detail text"))
	rows := records(t, b)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	for _, key := range []string{"error", "err", "Error", "panic", "recover", "detail"} {
		if got := rows[0][key]; got != "[text omitted]" {
			t.Errorf("%s guard lost: %v", key, got)
		}
	}
}

// Retaining reason puts it on the ordinary string path, so it inherits the same
// bound and the same truncation signal as every other diagnostic string.
func TestLoggingSanitizeReasonBounded(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	r.Root.Warn("bounded",
		zap.String("event", "bounded.reason"),
		zap.String("reason", strings.Repeat("x", 4096)))
	rows := records(t, b)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	got, _ := rows[0]["reason"].(string)
	if len(got) == 0 || len(got) > 2048 {
		t.Fatalf("reason bound broken: %d bytes", len(got))
	}
	if rows[0]["truncated"] != true {
		t.Fatalf("truncation not reported: %v", rows[0])
	}
}

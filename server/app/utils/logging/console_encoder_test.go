package logging

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// The console encoder is the operator-facing contract. These assertions pin the
// properties that make a line readable — aligned time/level columns, bare
// key=value pairs, one record per line — rather than exact spacing.
func TestConsoleEncoderLayout(t *testing.T) {
	sink := &memorySink{}
	c := configForTest(t, values{"logs.outputs": []string{"file"}, "logs.textformat": "console", "logs.level": "info"})
	r, err := NewRuntime(Options{
		Config: c, Service: "uvp-gb28181", Version: "v1", Instance: "node",
		Sinks: map[string]zapcore.WriteSyncer{"file": sink},
		Clock: fixedClock{time.Date(2026, 9, 14, 17, 16, 25, 61000000, time.FixedZone("CST", 28800))},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Root.Named("gb28181.register").Info("GB28181 设备注册成功",
		zap.String("event", "gb28181.register.succeeded"),
		zap.String("device_id", "35020000001310000999"),
		zap.Bool("is_first", true),
		zap.Int("cseq", 2),
	)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(sink.String(), "\n")
	for _, want := range []string{
		"2026-09-14 17:16:25.061",
		"INFO ",
		"gb28181.register",
		"GB28181 设备注册成功",
		"event=gb28181.register.succeeded",
		"device_id=35020000001310000999",
		"is_first=true",
		"cseq=2",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("console line missing %q:\n%s", want, line)
		}
	}
	// No JSON wrapper, no ANSI, no repeated per-process constants.
	for _, unwanted := range []string{`{"`, `"device_id"`, "service=", "version=", "instance=", "\x1b["} {
		if strings.Contains(line, unwanted) {
			t.Fatalf("console line still contains %q:\n%s", unwanted, line)
		}
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("console record spans multiple lines:\n%s", line)
	}
}

// The encoder may omit the per-process constants because the runtime core
// already reserves those keys (`fixedKey`), so a call site cannot log its own
// "version" that the omission would then hide.
func TestConsoleEncoderConstantOmissionCannotHideCallSiteData(t *testing.T) {
	sink := &memorySink{}
	c := configForTest(t, values{"logs.outputs": []string{"file"}, "logs.textformat": "console", "logs.level": "info"})
	r, err := NewRuntime(Options{
		Config: c, Service: "uvp-gb28181", Version: "v1", Instance: "node",
		Sinks: map[string]zapcore.WriteSyncer{"file": sink},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Root.Named("upgrade").Info("schema check", zap.String("event", "upgrade.check"), zap.String("version", "v9"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	line := sink.String()
	for _, unwanted := range []string{"service=", "version=", "instance="} {
		if strings.Contains(line, unwanted) {
			t.Fatalf("process constant repeated on every line (%q): %s", unwanted, line)
		}
	}
}

// A value containing control characters or whitespace must stay on one line,
// otherwise downstream consumers lose the one-record-per-line guarantee.
func TestConsoleEncoderEscapesControlCharacters(t *testing.T) {
	sink := &memorySink{}
	c := configForTest(t, values{"logs.outputs": []string{"file"}, "logs.textformat": "console", "logs.level": "info"})
	r, err := NewRuntime(Options{
		Config: c, Service: "uvp-gb28181", Version: "v1", Instance: "node",
		Sinks: map[string]zapcore.WriteSyncer{"file": sink},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Root.Named("gb28181.sip").Warn("parse failed", zap.String("event", "gb28181.sip.failed"), zap.String("header", "line1\nline2"))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimRight(sink.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("newline in a value split the record:\n%s", line)
	}
	if !strings.Contains(line, `header="line1\nline2"`) {
		t.Fatalf("value was not escaped: %s", line)
	}
}

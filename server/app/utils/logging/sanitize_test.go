package logging

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const testSecret = "never-log-this-credential-927"

type hostileValue struct{ calls *int }

func (h hostileValue) String() string { *h.calls++; return testSecret }
func (h hostileValue) Error() string  { *h.calls++; return testSecret }
func (h hostileValue) MarshalLogObject(e zapcore.ObjectEncoder) error {
	*h.calls++
	e.AddString("secret", testSecret)
	return nil
}
func (h hostileValue) MarshalLogArray(e zapcore.ArrayEncoder) error {
	*h.calls++
	e.AppendString(testSecret)
	return nil
}
func (h hostileValue) LogValue() slog.Value { *h.calls++; return slog.StringValue(testSecret) }

func TestLoggingSanitizeSensitiveFields(t *testing.T) {
	cfg := configForTest(t, values{"logs.outputs": []string{"file", "stdout"}, "logs.textformat": "json", "logs.stdoutformat": "json"})
	a, b := &memorySink{}, &memorySink{}
	r, err := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"file": a, "stdout": b}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	fields := []zap.Field{zap.String("password", testSecret), zap.String("Authorization", testSecret), zap.String("access_token", testSecret), zap.String("Cookie", testSecret), zap.String("secret_key", testSecret), zap.String("url", "https://user:"+testSecret+"@example.test/private/"+testSecret+"?cap="+testSecret), zap.String("device_id", "device-1")}
	l := WithIdentity(r.Root.Named("test"), zap.String("request_id", "request-1"))
	l.Info("safe event", fields...)
	l.With(fields...).Info("safe bound event")
	for _, out := range []*memorySink{a, b} {
		if strings.Contains(out.String(), testSecret) {
			t.Error("secret reached sink")
		}
		rows := records(t, out)
		if len(rows) != 2 {
			t.Fatalf("records %d", len(rows))
		}
		for _, row := range rows {
			if row["request_id"] != "request-1" || row["device_id"] != "device-1" {
				t.Errorf("safe identities lost: %v", row)
			}
		}
	}
}
func TestLoggingSanitizeUnknownObjects(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	calls := 0
	h := hostileValue{&calls}
	fields := []zap.Field{zap.Any("any", map[string]interface{}{"nested": h}), zap.Object("object", h), zap.Array("array", h), zap.Stringer("stringer", h), zap.Error(h), zap.Reflect("reflect", h)}
	r.Root.With(fields...).Info("bound")
	r.Root.Info("direct", fields...)
	if calls != 0 {
		t.Errorf("invoked unknown formatting %d times", calls)
	}
	if strings.Contains(b.String(), testSecret) {
		t.Error("unknown object exposed")
	}
	if len(records(t, b)) != 2 {
		t.Fatal("logs disappeared")
	}
}
func TestLoggingSanitizeErrors(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	inputs := []error{&mysql.MySQLError{Number: 1062, Message: testSecret}, fmt.Errorf("%s: %w", testSecret, syscall.ECONNREFUSED), &net.OpError{Op: "dial", Err: syscall.ETIMEDOUT}, errors.New(testSecret)}
	for _, err := range inputs {
		r.Root.Error("operation failed", zap.Error(err), zap.NamedError("secondary", err))
	}
	if strings.Contains(b.String(), testSecret) {
		t.Error("raw error leaked")
	}
	rows := records(t, b)
	if len(rows) != 4 {
		t.Fatalf("rows %d", len(rows))
	}
	for _, row := range rows {
		detail, ok := row["error"].(map[string]interface{})
		if !ok || detail["class"] == "" || detail["type"] == "" {
			t.Fatalf("missing error summary: %v", row)
		}
	}
	if rows[0]["error"].(map[string]interface{})["code"] != float64(1062) {
		t.Fatal("database error code lost")
	}
}
func TestLoggingSanitizeCheckedEntry(t *testing.T) {
	r, b := runtimeForTest(t, values{"logs.level": "debug"})
	l := r.Root.Named("nested").With(zap.String("password", testSecret))
	ce := l.Check(zap.DebugLevel, "checked")
	if ce == nil {
		t.Fatal("missing debug entry")
	}
	ce.Write(zap.String("token", testSecret))
	if strings.Contains(b.String(), testSecret) {
		t.Fatal("Check bypassed sanitizer")
	}
	if len(records(t, b)) != 1 {
		t.Fatal("missing checked output")
	}
}
func TestLoggingBridge(t *testing.T) {
	r, b := runtimeForTest(t, values{"logs.level": "debug"})
	legacy := log.New(StandardWriter(r.Root), "", 0)
	legacy.Print(testSecret)
	calls := 0
	s := slog.New(NewSlogHandler(r.Root.Named("sip"))).With("object", hostileValue{&calls})
	s.WithGroup("transport").ErrorContext(context.Background(), testSecret, "password", testSecret, "node_id", "node-1")
	if calls != 0 {
		t.Fatal("slog invoked unknown LogValue")
	}
	if strings.Contains(b.String(), testSecret) {
		t.Fatal("bridge exposed arbitrary text")
	}
	rows := records(t, b)
	if len(rows) != 2 {
		t.Fatalf("bridge records %d", len(rows))
	}
	for _, row := range rows {
		if row["event"] != "legacy.log" || row["text_omitted"] != true {
			t.Errorf("bridge metadata missing: %v", row)
		}
	}
}
func TestLoggingSanitizeSizeLimits(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	fields := []zap.Field{zap.String("event", "bounded.event"), zap.String("error_class", "internal"), zap.String("stack", strings.Repeat("panicFunction\n", 3000))}
	for i := 0; i < 200; i++ {
		fields = append(fields, zap.String(fmt.Sprintf("field_%03d", i), strings.Repeat("\x00\"\\", 4096)))
	}
	r.Root.With(fields...).Info(strings.Repeat("m", 40000), fields...)
	if b.Len() > 32768 {
		t.Errorf("encoded event %d exceeds 32KiB", b.Len())
	}
	rows := records(t, b)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	row := rows[0]
	if row["truncated"] != true || row["error_class"] != "internal" || row["event"] != "bounded.event" {
		t.Errorf("required summary lost")
	}
	if stack, ok := row["stack"].(string); !ok || len(stack) > 16384 {
		t.Error("invalid stack bound")
	}
}

func TestLoggingSanitizeInvalidUTF8AndFullWith(t *testing.T) {
	r, b := runtimeForTest(t, nil)
	fields := make([]zap.Field, 100)
	for i := range fields {
		fields[i] = zap.String(fmt.Sprintf("field%d", i), strings.Repeat("\xff", 10000))
	}
	r.Root.With(fields...).Info("bounded")
	if b.Len() > MaxEventBytes {
		t.Errorf("invalid UTF8 output %d exceeds limit", b.Len())
	}
	rows := records(t, b)
	if len(rows) != 1 || rows[0]["event"] != "legacy.log" {
		t.Fatal("full With lost required event")
	}
}

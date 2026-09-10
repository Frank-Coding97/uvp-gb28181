package logging

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLoggingUnchangedEntrySafety(t *testing.T) {
	safe := []zap.Field{zap.String("event", "device.ready"), zap.String("device_id", "device-1"), zap.Int("count", 3), zap.Bool("online", true)}
	entry := zapcore.Entry{Time: time.Unix(10, 0), Level: zapcore.InfoLevel, LoggerName: "device", Message: "device ready"}
	cases := []struct {
		name   string
		fields []zap.Field
		entry  zapcore.Entry
		bound  map[string]bool
		used   int
		want   bool
	}{
		{name: "primitives", fields: safe, entry: entry, want: true},
		{name: "unicode", fields: []zap.Field{zap.String("event", "设备.ready"), zap.String("label", "设备\n甲")}, entry: entry, want: true},
		{name: "missing_event", fields: []zap.Field{zap.Int("count", 1)}, entry: entry},
		{name: "sensitive", fields: append(append([]zap.Field{}, safe...), zap.String("Access_Token", "fixture-secret")), entry: entry},
		{name: "unknown", fields: append(append([]zap.Field{}, safe...), zap.Any("object", map[string]any{"x": "fixture-secret"})), entry: entry},
		{name: "raw_error", fields: append(append([]zap.Field{}, safe...), zap.String("error", "fixture-secret")), entry: entry},
		{name: "endpoint", fields: append(append([]zap.Field{}, safe...), zap.String("endpoint", "https://u:fixture-secret@example.test/private")), entry: entry},
		{name: "identity_override", fields: append(append([]zap.Field{}, safe...), zap.String("request_id", "spoof")), entry: entry},
		{name: "fixed_override", fields: append(append([]zap.Field{}, safe...), zap.String("service", "spoof")), entry: entry},
		{name: "bound_override", fields: safe, entry: entry, bound: map[string]bool{"device_id": true}},
		{name: "budget", fields: safe, entry: entry, used: fieldBudget - 32},
		{name: "long_string", fields: append(append([]zap.Field{}, safe...), zap.String("label", strings.Repeat("x", 2049))), entry: entry},
	}
	stack := entry
	stack.Stack = "panic stack"
	cases = append(cases, struct {
		name   string
		fields []zap.Field
		entry  zapcore.Entry
		bound  map[string]bool
		used   int
		want   bool
	}{name: "stack", fields: safe, entry: stack})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := &runtimeCore{bound: tc.bound, used: tc.used}
			before := append([]zap.Field(nil), tc.fields...)
			if got := core.canWriteUnchanged(tc.entry, tc.fields); got != tc.want {
				t.Fatalf("eligible=%v want=%v", got, tc.want)
			}
			if !reflect.DeepEqual(before, tc.fields) {
				t.Fatal("caller fields mutated")
			}
			if tc.want {
				normalized, _, cut := sanitizeFields(tc.fields, fieldBudget-tc.used, maxFields-len(tc.bound))
				if cut || !reflect.DeepEqual(normalized, tc.fields) {
					t.Fatal("fast path would differ from sanitizer")
				}
			}
		})
	}
}

func TestLoggingUnchangedEntryMatchesFullNormalization(t *testing.T) {
	cases := []struct {
		name     string
		bound    []zap.Field
		identity []zap.Field
		fields   []zap.Field
	}{
		{name: "primitives", fields: []zap.Field{zap.String("event", "scan.completed"), zap.Int("count", 8), zap.Bool("changed", true), zap.Float64("duration_ms", 1.25)}},
		{name: "event_after_fields", fields: []zap.Field{zap.String("label", "设备\n甲"), zap.Duration("elapsed", time.Second), zap.String("event", "status.updated")}},
		{name: "bound_event", bound: []zap.Field{zap.String("event", "task.completed")}, fields: []zap.Field{zap.Uint64("attempt", 2), zap.String("result", "ready")}},
		{name: "bound_identity", identity: []zap.Field{zap.String("request_id", "trusted-request")}, fields: []zap.Field{zap.Int8("status", 1), zap.String("event", "status.ready")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, actualSink := runtimeForTest(t, nil)
			reference, referenceSink := runtimeForTest(t, nil)
			a, r := actual.Root, reference.Root
			if len(tc.bound) > 0 {
				a = a.With(tc.bound...)
				r = r.With(tc.bound...)
			}
			if len(tc.identity) > 0 {
				a = WithIdentity(a, tc.identity...)
				r = WithIdentity(r, tc.identity...)
			}
			ac, rc := a.Core().(*runtimeCore), r.Core().(*runtimeCore)
			entry := zapcore.Entry{Time: time.Unix(10, 0), Level: zapcore.InfoLevel, LoggerName: "operation", Message: "operation completed"}
			if !ac.canWriteUnchanged(entry, tc.fields) {
				t.Fatal("safe fixture did not exercise unchanged path")
			}
			if err := ac.Write(entry, tc.fields); err != nil {
				t.Fatal(err)
			}
			normalized, _, cut := sanitizeFields(tc.fields, fieldBudget-rc.used, maxFields-len(rc.bound))
			if cut {
				t.Fatal("reference unexpectedly truncated")
			}
			if err := rc.inner.Write(entry, normalized); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(records(t, actualSink), records(t, referenceSink)) {
				t.Fatal("JSON semantics differ from full normalization")
			}
		})
	}
}

func TestLoggingUnchangedEntryRejectsUnprovenBounds(t *testing.T) {
	core := &runtimeCore{}
	entry := zapcore.Entry{LoggerName: "test", Message: "test"}
	fields := []zap.Field{zap.String("event", "fixture")}
	variants := []struct {
		name   string
		entry  zapcore.Entry
		fields []zap.Field
	}{
		{"long_key", entry, append(append([]zap.Field{}, fields...), zap.Int(strings.Repeat("a", 129), 1))},
		{"reason", entry, append(append([]zap.Field{}, fields...), zap.String("reason", "fixture-secret"))},
		{"namespace", entry, append(append([]zap.Field{}, fields...), zap.Namespace("nested"))},
		{"too_many_fields", entry, append(fields, make([]zap.Field, maxFields)...)},
	}
	for _, name := range []string{"message", "component", "stack"} {
		e := entry
		switch name {
		case "message":
			e.Message = strings.Repeat("x", 1025)
		case "component":
			e.LoggerName = strings.Repeat("x", 257)
		case "stack":
			e.Stack = "stack"
		}
		variants = append(variants, struct {
			name   string
			entry  zapcore.Entry
			fields []zap.Field
		}{name, e, fields})
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			if core.canWriteUnchanged(tc.entry, tc.fields) {
				t.Fatal("unsafe or unbounded input accepted")
			}
		})
	}
	core.truncated = true
	if core.canWriteUnchanged(entry, fields) {
		t.Fatal("lost existing truncation marker")
	}
}

func TestLoggingSensitiveKeyClassification(t *testing.T) {
	fragments := []string{"password", "passwd", "secret", "token", "authorization", "cookie", "credential", "privatekey", "accesskey", "apikey"}
	exact := []string{"pwd", "cap", "key", "headers", "body", "payload", "raw", "rawsql", "sql", "sip", "requestbody", "responsebody"}
	for _, word := range fragments {
		for _, prefix := range []string{"", "my", "label"} {
			for _, suffix := range []string{"", "value", "x"} {
				for _, separator := range []string{"", "_", "-", "."} {
					key := prefix + strings.Join(strings.Split(strings.ToUpper(word), ""), separator) + suffix
					if !sensitiveKey(key) {
						t.Errorf("sensitive key missed: %q", key)
					}
				}
			}
		}
	}
	for _, word := range exact {
		if !sensitiveKey(strings.ToUpper(word)) {
			t.Errorf("exact sensitive key missed: %s", word)
		}
	}
	for _, key := range []string{"event", "device_id", "scanned", "changed", "duration_ms", "operation", "attempt", "template_fingerprint", "rows", "status", "labels", "platform", "pressure", "active", "creation", "requests", "bytes", "stream_id", "endpoint"} {
		if sensitiveKey(key) {
			t.Errorf("safe key classified sensitive: %s", key)
		}
	}
	if !sensitiveKey(strings.Repeat("x", 129)) {
		t.Fatal("oversized key accepted")
	}
}

func TestLoggingUnchangedEntryJSONReplacement(t *testing.T) {
	inputs := []string{string([]byte{0xff}), string([]byte{0xc2, 0x20}), string([]byte{0xe2, 0x82}), string([]byte{0xc0, 0xaf}), "\ufffd"}
	for index, input := range inputs {
		for _, location := range []string{"key", "value", "message", "component"} {
			t.Run(fmt.Sprintf("%d/%s", index, location), func(t *testing.T) {
				actual, aout := runtimeForTest(t, nil)
				reference, rout := runtimeForTest(t, nil)
				ac, rc := actual.Root.Core().(*runtimeCore), reference.Root.Core().(*runtimeCore)
				entry := zapcore.Entry{Time: time.Unix(10, 0), Level: zapcore.InfoLevel, LoggerName: "test", Message: "test"}
				fields := []zap.Field{zap.String("event", "fixture")}
				switch location {
				case "key":
					fields = append(fields, zap.String(input, "value"))
				case "value":
					fields = append(fields, zap.String("label", input))
				case "message":
					entry.Message = input
				case "component":
					entry.LoggerName = input
				}
				if !ac.canWriteUnchanged(entry, fields) {
					t.Fatal("bounded input did not hit unchanged path")
				}
				if err := ac.Write(entry, fields); err != nil {
					t.Fatal(err)
				}
				normalized, _, cut := sanitizeFields(fields, fieldBudget, maxFields)
				if cut {
					t.Fatal("reference unexpectedly truncated")
				}
				if err := rc.inner.Write(entry, normalized); err != nil {
					t.Fatal(err)
				}
				a, r := records(t, aout), records(t, rout)
				if !reflect.DeepEqual(a, r) {
					t.Fatal("invalid UTF-8 changes JSON semantics")
				}
				if !utf8.ValidString(aout.String()) {
					t.Fatal("JSON contains invalid bytes")
				}
				replaced := false
				for key, value := range a[0] {
					if strings.Contains(key, "\ufffd") {
						replaced = true
					}
					if str, ok := value.(string); ok && strings.Contains(str, "\ufffd") {
						replaced = true
					}
				}
				if !replaced {
					t.Fatal("JSON replacement rune missing")
				}
			})
		}
	}
}

func TestLoggingUnchangedEntryConservativeBoundaries(t *testing.T) {
	core := &runtimeCore{}
	base := zapcore.Entry{LoggerName: "test", Message: "test"}
	for _, part := range []string{"key", "value", "message", "component"} {
		limit := map[string]int{"key": 128, "value": 2048, "message": 1024, "component": 256}[part]
		for extra := 0; extra <= 1; extra++ {
			t.Run(fmt.Sprintf("%s/%d", part, extra), func(t *testing.T) {
				entry := base
				fields := []zap.Field{zap.String("event", "fixture")}
				text := strings.Repeat("x", limit/6+extra)
				switch part {
				case "key":
					fields = append(fields, zap.String(text, "x"))
				case "value":
					fields = append(fields, zap.String("label", text))
				case "message":
					entry.Message = text
				case "component":
					entry.LoggerName = text
				}
				if got := core.canWriteUnchanged(entry, fields); got != (extra == 0) {
					t.Fatalf("eligible=%v at conservative boundary +%d", got, extra)
				}
			})
		}
	}
	fields := []zap.Field{zap.String("event", "fixture"), zap.Int("count", 1)}
	core.used = fieldBudget - 182 // 80 bytes for event + 102 for count, including key budgets.
	if !core.canWriteUnchanged(base, fields) {
		t.Fatal("exact cumulative bound rejected")
	}
	core.used++
	if core.canWriteUnchanged(base, fields) {
		t.Fatal("cumulative budget overflow accepted")
	}
}

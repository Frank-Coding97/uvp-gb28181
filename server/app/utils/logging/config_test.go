package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type values map[string]interface{}

func (v values) Get(k string) interface{} { return v[k] }

type memorySink struct {
	bytes.Buffer
	syncs, closes int
}

func (m *memorySink) Sync() error  { m.syncs++; return nil }
func (m *memorySink) Close() error { m.closes++; return nil }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time                         { return c.now }
func (c fixedClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func configForTest(t *testing.T, v values) Config {
	t.Helper()
	c, err := ParseConfig(v, "/app")
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func runtimeForTest(t *testing.T, v values) (*Runtime, *memorySink) {
	t.Helper()
	if v == nil {
		v = values{}
	}
	v["logs.outputs"] = []string{"stdout"}
	v["logs.stdoutformat"] = "json"
	c := configForTest(t, v)
	m := &memorySink{}
	r, err := NewRuntime(Options{Config: c, Service: "uvp", Version: "v1", Instance: "instance-test", Sinks: map[string]zapcore.WriteSyncer{"stdout": m}, Clock: fixedClock{time.Date(2026, 9, 5, 12, 13, 14, 123000000, time.FixedZone("CST", 8*3600))}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, m
}
func records(t *testing.T, b *memorySink) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	for _, line := range bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row map[string]interface{}
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		result = append(result, row)
	}
	return result
}

func TestLoggingConfigOutputs(t *testing.T) {
	cases := []struct {
		name    string
		input   values
		outputs string
		notices bool
	}{
		{"legacy default", values{}, "file,stdout", true},
		{"legacy console off", values{"logs.console": false}, "file", true},
		{"explicit", values{"logs.outputs": []string{"stdout"}}, "stdout", false},
		{"explicit wins", values{"logs.outputs": []interface{}{"stdout"}, "logs.console": false}, "stdout", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := configForTest(t, tc.input)
			if strings.Join(c.Outputs, ",") != tc.outputs {
				t.Fatalf("outputs %v want %s", c.Outputs, tc.outputs)
			}
			if (len(c.Notices) > 0) != tc.notices {
				t.Fatalf("notices %v", c.Notices)
			}
			if c.MaxSizeMB != 5 || c.MaxBackups != 7 || c.MaxAgeDays != 15 {
				t.Fatalf("invalid bounds: %+v", c)
			}
		})
	}
}
func TestLoggingConfigRejectsInvalid(t *testing.T) {
	cases := []values{{"logs.outputs": []string{}}, {"logs.outputs": []string{"file", "file"}}, {"logs.outputs": []string{"unknown"}}, {"logs.outputs": "stdout"}, {"logs.level": "verbose"}, {"logs.modules": map[string]interface{}{"db": "verbose"}}, {"logs.textformat": "yaml"}, {"logs.stdoutformat": "xml"}, {"logs.maxsize": 0}, {"logs.maxbackups": -1}, {"logs.maxage": 0}, {"logs.maxsize": 1.5}}
	for i, v := range cases {
		if _, err := ParseConfig(v, "/app"); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
func TestLoggingRuntimeFormats(t *testing.T) {
	for _, format := range []string{"json", "console"} {
		t.Run(format, func(t *testing.T) {
			c := configForTest(t, values{"logs.outputs": []string{"file", "stdout"}, "logs.textformat": format, "logs.stdoutformat": format})
			a, b := &memorySink{}, &memorySink{}
			r, err := NewRuntime(Options{Config: c, Service: "uvp", Version: "v1", Instance: "node", Sinks: map[string]zapcore.WriteSyncer{"file": a, "stdout": b}, Clock: fixedClock{time.Date(2026, 9, 5, 12, 13, 14, 123000000, time.FixedZone("CST", 28800))}})
			if err != nil {
				t.Fatal(err)
			}
			r.Root.Named("db").Info("query completed", zap.String("event", "db.query"), zap.Float64("duration_ms", 12.5), zap.Int64("rows", -1))
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			_ = r.Close()
			for _, sink := range []*memorySink{a, b} {
				if strings.Contains(sink.String(), "\x1b[") || !strings.Contains(sink.String(), "2026-09-05T12:13:14.123+08:00") {
					t.Fatalf("timestamp/ANSI contract: %s", sink.String())
				}
				if sink.syncs != 1 || sink.closes != 1 {
					t.Fatalf("close %d/%d", sink.syncs, sink.closes)
				}
			}
			if format == "json" {
				rows := records(t, a)
				if len(rows) != 1 {
					t.Fatalf("records %d", len(rows))
				}
				row := rows[0]
				for k, want := range map[string]interface{}{"service": "uvp", "version": "v1", "instance": "node", "component": "db", "event": "db.query", "message": "query completed", "duration_ms": 12.5, "rows": float64(-1)} {
					if row[k] != want {
						t.Errorf("%s = %v want %v", k, row[k], want)
					}
				}
			}
		})
	}
}
func TestLoggingRuntimeModuleLevels(t *testing.T) {
	r, b := runtimeForTest(t, values{"logs.level": "warn", "logs.modules": map[string]interface{}{"db": "error", "db.query": "debug"}})
	r.Root.Info("hidden")
	r.Root.Named("access").Info("access")
	r.Root.Named("db").Warn("hidden")
	r.Root.Named("db").Named("query").Debug("query")
	r.Root.Named("other").Warn("warn")
	rows := records(t, b)
	if len(rows) != 3 {
		t.Fatalf("got %d records want 3: %s", len(rows), b.String())
	}
	if rows[0]["component"] != "access" || rows[1]["component"] != "db.query" {
		t.Fatalf("module fields %v", rows)
	}
}
func TestLoggingRuntimeIdentityAndSnapshot(t *testing.T) {
	input := values{"logs.level": "warn", "logs.modules": map[string]interface{}{"db": "debug"}}
	r, b := runtimeForTest(t, input)
	scoped := WithIdentity(r.Root, zap.String("request_id", "trusted"), zap.String("request_id", "duplicate"))
	scoped.With(zap.String("request_id", "evil1"), zap.String("instance", "evil2")).Warn("one", zap.String("request_id", "evil3"), zap.String("component", "evil4"), zap.String("service", "evil5"))
	r.Root.Warn("root")
	cfg := r.Config()
	if cfg.Modules == nil {
		t.Fatal("runtime has no configuration snapshot")
	}
	cfg.Modules["db"] = zap.FatalLevel
	cfg.Outputs[0] = "file"
	changed := r.Config()
	changed.Level = zap.DebugLevel
	r.NoticeReload(changed)
	r.NoticeReload(changed)
	r.Root.Info("still hidden")
	r.Root.Named("db").Debug("still visible")
	rows := records(t, b)
	if len(rows) != 4 {
		t.Fatalf("records %d: %s", len(rows), b.String())
	}
	if rows[0]["request_id"] != "trusted" || rows[0]["instance"] != "instance-test" || rows[0]["service"] != "uvp" || rows[0]["component"] != "app" {
		t.Fatalf("identity overwritten: %v", rows[0])
	}
	if _, ok := rows[1]["request_id"]; ok {
		t.Fatal("request leaked into root")
	}
	if rows[2]["event"] != "logging.restart_required" {
		t.Fatalf("reload notice: %v", rows[2])
	}
}

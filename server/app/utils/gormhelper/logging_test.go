package gormhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	gormLog "gorm.io/gorm/logger"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type logTestValues map[string]interface{}

func (v logTestValues) Get(k string) interface{} { return v[k] }
func gormLogFixture(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	cfg, e := logging.ParseConfig(logTestValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json", "logs.level": "debug"}, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b := &bytes.Buffer{}
	r, e := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(b)}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	return logging.WithContext(context.Background(), logging.WithIdentity(r.Root, zap.String("request_id", "request-db"))), b
}
func TestLoggingGORMModes(t *testing.T) {
	ctx, b := gormLogFixture(t)
	for _, tc := range []struct {
		mode gormLog.LogLevel
		err  error
		want int
	}{{gormLog.Silent, errors.New("fixture-secret"), 0}, {gormLog.Warn, nil, 0}, {gormLog.Error, nil, 0}, {gormLog.Info, nil, 1}, {gormLog.Error, errors.New("fixture-secret"), 1}} {
		l := &logger{Config: gormLog.Config{LogLevel: tc.mode, SlowThreshold: 30 * time.Second}}
		calls := 0
		b.Reset()
		l.Trace(ctx, time.Now(), func() (string, int64) { calls++; return "SELECT 'fixture-secret'", -1 }, tc.err)
		if calls != tc.want {
			t.Errorf("mode %v callback %d want %d", tc.mode, calls, tc.want)
		}
		if strings.Contains(b.String(), "fixture-secret") {
			t.Fatal("SQL or error leaked")
		}
		if tc.want == 1 && !strings.Contains(b.String(), "request-db") {
			t.Error("missing DB correlation")
		}
	}
}
func TestLoggingGORMSlowThreshold(t *testing.T) {
	ctx, b := gormLogFixture(t)
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	l := &logger{Config: gormLog.Config{LogLevel: gormLog.Warn, SlowThreshold: 30 * time.Second}, now: func() time.Time { return now }}
	for _, elapsed := range []time.Duration{29 * time.Second, 30 * time.Second, 30*time.Second + time.Nanosecond} {
		b.Reset()
		calls := 0
		l.Trace(ctx, now.Add(-elapsed), func() (string, int64) { calls++; return "SELECT ?", -1 }, nil)
		want := 0
		if elapsed > 30*time.Second {
			want = 1
		}
		if calls != want {
			t.Errorf("elapsed %s callback %d", elapsed, calls)
		}
		if want == 1 {
			var row map[string]interface{}
			if e := json.Unmarshal(b.Bytes(), &row); e != nil {
				t.Fatal(e)
			}
			if row["rows"] != float64(-1) || row["event"] != "db.slow_query" {
				t.Errorf("slow summary %v", row)
			}
		}
	}
}
func TestLoggingGORMParamsFilter(t *testing.T) {
	l := &logger{Config: gormLog.Config{LogLevel: gormLog.Info}}
	filter, ok := interface{}(l).(gorm.ParamsFilter)
	if !ok {
		t.Fatal("missing pre-interpolation ParamsFilter")
	}
	sql, params := filter.ParamsFilter(context.Background(), "SELECT ?", "fixture-secret")
	if sql != "SELECT ?" || len(params) != 0 {
		t.Fatal("parameters retained")
	}
	ctx, b := gormLogFixture(t)
	base, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	conn, e := base.DB()
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	for _, dialector := range []gorm.Dialector{mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), postgres.New(postgres.Config{Conn: conn}), sqlserver.New(sqlserver.Config{Conn: conn})} {
		t.Run(dialector.Name(), func(t *testing.T) {
			b.Reset()
			explained := &explainRecorder{Dialector: dialector}
			db, e := gorm.Open(explained, &gorm.Config{DisableAutomaticPing: true, DryRun: true, SkipDefaultTransaction: true, Logger: l})
			if e != nil {
				t.Fatal(e)
			}
			if e := db.WithContext(ctx).Exec("UPDATE users SET password=? WHERE id=?", "fixture-secret", 1).Error; e != nil {
				t.Fatal(e)
			}
			if explained.parameters != 0 {
				t.Error("bound values reached dialect Explain")
			}
			if b.Len() == 0 || bytes.Contains(b.Bytes(), []byte("fixture-secret")) {
				t.Fatalf("unsafe or missing dialect log: %s", b.String())
			}
		})
	}
}
func TestLoggingGORMRecordNotFoundAndRollback(t *testing.T) {
	ctx, b := gormLogFixture(t)
	l := &logger{Config: gormLog.Config{LogLevel: gormLog.Error, IgnoreRecordNotFoundError: true}}
	called := false
	l.Trace(ctx, time.Now(), func() (string, int64) { called = true; return "SELECT 1", 0 }, gorm.ErrRecordNotFound)
	if called {
		t.Error("ignored not found evaluated SQL")
	}
	copy := l.LogMode(gormLog.Info)
	if l.LogLevel != gormLog.Error || copy == l {
		t.Error("LogMode mutated parent")
	}
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: copy})
	if e != nil {
		t.Fatal(e)
	}
	conn, _ := db.DB()
	defer conn.Close()
	db.Exec("CREATE TABLE test_rows (id INTEGER)")
	sentinel := errors.New("fixture-secret")
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("INSERT INTO test_rows VALUES (1)").Error; e != nil {
			return e
		}
		return sentinel
	})
	if e != sentinel {
		t.Fatal("transaction error changed")
	}
	var count int64
	db.Table("test_rows").Count(&count)
	if count != 0 {
		t.Fatal("transaction did not rollback")
	}
	if bytes.Contains(b.Bytes(), []byte("fixture-secret")) {
		t.Fatal("transaction secret logged")
	}
}

func TestLoggingGORMSchemaAndFingerprint(t *testing.T) {
	ctx, b := gormLogFixture(t)
	l := &logger{Config: gormLog.Config{LogLevel: gormLog.Info}}
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: l})
	if e != nil {
		t.Fatal(e)
	}
	conn, _ := db.DB()
	defer conn.Close()
	installLogContext(db)
	db.WithContext(ctx).Session(&gorm.Session{DryRun: true}).Model(&createHookRow{}).Where("created_by = ?", 42).Find(&[]createHookRow{})
	if !strings.Contains(b.String(), `"table":"create_hook_rows"`) {
		t.Fatal("missing known schema table")
	}
	op, a := statementSummary("SELECT password FROM users WHERE password='fixture-secret' AND id=42")
	_, other := statementSummary("SELECT password FROM users WHERE password='another-secret' AND id=99")
	if op != "select" || a != other || a == "" {
		t.Error("fingerprint depends on literal value")
	}
	_, bound := statementSummary("SELECT password FROM users WHERE password=? AND id=?")
	if bound != a {
		t.Error("bound and literal query shape diverged")
	}
}

type explainRecorder struct {
	gorm.Dialector
	parameters int
}

func (d *explainRecorder) Explain(sql string, vars ...interface{}) string {
	d.parameters += len(vars)
	return d.Dialector.Explain(sql, vars...)
}
func TestLoggingGORMConcurrentScopes(t *testing.T) {
	cfg, err := logging.ParseConfig(logTestValues{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := &bytes.Buffer{}
	runtime, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.Lock(zapcore.AddSync(b))}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	l := &logger{Config: gormLog.Config{LogLevel: gormLog.Info}}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := strconv.Itoa(i)
			scope := logging.WithContext(context.Background(), logging.WithIdentity(runtime.Root, zap.String("request_id", id)))
			l.Trace(scope, time.Now(), func() (string, int64) { return "SELECT ?", int64(i) }, nil)
		}(i)
	}
	wg.Wait()
	lines := bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n"))
	if len(lines) != 64 {
		t.Fatal("lost SQL records")
	}
	for _, line := range lines {
		var r map[string]interface{}
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		if r["request_id"] != strconv.Itoa(int(r["rows"].(float64))) {
			t.Error("SQL correlation crossed requests")
		}
	}
}

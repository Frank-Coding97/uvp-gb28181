package metrics_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type concurrentWriters struct {
	recorder   *metrics.PersistentRecorder
	login      *service.LoginLogService
	loginEvent globalapp.LoginLogEvent
	repo       *recording.GormRepo
	session    *gbmodels.GbRecordingSession
}

type writerErrors struct {
	metric    error
	login     error
	recording error
}

func newConcurrentSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cross-module.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func newConcurrentWriters(db *gorm.DB, tag string, nodeID int64, metricNow time.Time) concurrentWriters {
	recorder := metrics.NewPersistentRecorder(db, nil)
	recorder.SetClock(func() time.Time { return metricNow })
	recorder.Begin(metrics.Transaction{
		Kind: metrics.TxRegister, Direction: metrics.DirIn,
		CallID: tag + "-metric", CSeq: "1", StartedAt: metricNow,
	})
	recorder.End(tag+"-metric", "1", 200, true)
	start := metricNow.Add(time.Second)
	return concurrentWriters{
		recorder: recorder,
		login:    service.NewLoginLogService(db),
		loginEvent: globalapp.LoginLogEvent{
			Username: tag + "-login", Result: service.LoginResultSuccess,
			IP: "127.0.0.1", Location: "test", UserAgent: "sqlite-test", Browser: "test", OS: "test",
		},
		repo: recording.NewGormRepo(db),
		session: &gbmodels.GbRecordingSession{
			ChannelID: 1, DeviceID: tag + "-device", NodeID: nodeID,
			VHost: gbmodels.DefaultRecordingVHost, App: gbmodels.DefaultRecordingApp,
			Stream: tag + "-stream", State: gbmodels.RecordingSessionStateRecording, StartedAt: &start,
		},
	}
}

func runConcurrentWrites(ctx context.Context, writers concurrentWriters) writerErrors {
	start := make(chan struct{})
	results := make(chan struct {
		kind string
		err  error
	}, 3)
	var wg sync.WaitGroup
	startWriter := func(kind string, write func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- struct {
				kind string
				err  error
			}{kind: kind, err: write()}
		}()
	}
	startWriter("metric", func() error { return writers.recorder.Flush(ctx) })
	startWriter("login", func() error { return writers.login.RecordLogin(ctx, writers.loginEvent) })
	startWriter("recording", func() error { return writers.repo.UpsertSession(ctx, writers.session) })
	close(start)
	wg.Wait()
	close(results)
	var out writerErrors
	for result := range results {
		switch result.kind {
		case "metric":
			out.metric = result.err
		case "login":
			out.login = result.err
		case "recording":
			out.recording = result.err
		}
	}
	return out
}

func flushMetric(t *testing.T, ctx context.Context, recorder *metrics.PersistentRecorder) {
	t.Helper()
	require.NoError(t, recorder.Flush(ctx))
}

func writeLogin(t *testing.T, ctx context.Context, writers concurrentWriters) {
	t.Helper()
	require.NoError(t, writers.login.RecordLogin(ctx, writers.loginEvent))
}

func writeSession(t *testing.T, ctx context.Context, writers concurrentWriters) {
	t.Helper()
	require.NoError(t, writers.repo.UpsertSession(ctx, writers.session))
}

func installInsertAbortTrigger(t *testing.T, db *gorm.DB, trigger, table string) {
	t.Helper()
	sql := fmt.Sprintf(`CREATE TRIGGER "%s" BEFORE INSERT ON "%s" BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`, trigger, table)
	require.NoError(t, db.Exec(sql).Error)
	t.Cleanup(func() { _ = db.Exec(fmt.Sprintf(`DROP TRIGGER IF EXISTS "%s"`, trigger)).Error })
}

func countRows(t *testing.T, db *gorm.DB, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Where(query, args...).Count(&count).Error)
	return count
}

func TestSQLiteBaselineConcurrentMediaWritesSurviveReopen(t *testing.T) {
	db, path := newConcurrentSQLiteBaselineDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 7, 16, 0, 23, 0, time.UTC)
	writers := newConcurrentWriters(db, "success", 301, now)

	got := runConcurrentWrites(ctx, writers)
	require.NoError(t, got.metric)
	require.NoError(t, got.login)
	require.NoError(t, got.recording)

	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	reopened, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })

	require.EqualValues(t, 1, countRows(t, reopened, &gbmodels.GbSipMetricFlush{}, "flush_id <> ''"))
	require.EqualValues(t, 1, countRows(t, reopened, &gbmodels.GbSipMetricMinute{}, "request_count = 1"))
	require.EqualValues(t, 1, countRows(t, reopened, &appmodels.SysLoginLog{}, "username = ?", writers.loginEvent.Username))
	require.EqualValues(t, 1, countRows(t, reopened, &gbmodels.GbRecordingSession{}, "node_id = ? AND stream = ?", writers.session.NodeID, writers.session.Stream))
}

func TestSQLiteBaselineConcurrentMediaWriteFailuresAreIsolated(t *testing.T) {
	tests := []struct {
		name   string
		target string
		table  string
	}{
		{name: "metric", target: "metric", table: "gb_sip_metric_flush"},
		{name: "login", target: "login", table: "sys_login_logs"},
		{name: "recording", target: "recording", table: "gb_recording_session"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := newConcurrentSQLiteBaselineDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			base := time.Date(2026, 9, 7, 17, 0, 23, 0, time.UTC)
			confirmed := newConcurrentWriters(db, "confirmed", int64(310+index), base)
			writers := newConcurrentWriters(db, "fail"+test.target, int64(320+index), base.Add(time.Minute))

			switch test.target {
			case "metric":
				writeLogin(t, ctx, confirmed)
				writeSession(t, ctx, confirmed)
			case "login":
				flushMetric(t, ctx, confirmed.recorder)
				writeSession(t, ctx, confirmed)
			case "recording":
				flushMetric(t, ctx, confirmed.recorder)
				writeLogin(t, ctx, confirmed)
			}

			installInsertAbortTrigger(t, db, "abort_"+test.target, test.table)
			got := runConcurrentWrites(ctx, writers)
			switch test.target {
			case "metric":
				require.ErrorContains(t, got.metric, "injected write failure")
				require.NoError(t, got.login)
				require.NoError(t, got.recording)
				require.EqualValues(t, 1, countRows(t, db, &appmodels.SysLoginLog{}, "username = ?", confirmed.loginEvent.Username))
				require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbRecordingSession{}, "node_id = ?", confirmed.session.NodeID))
				require.EqualValues(t, 1, countRows(t, db, &appmodels.SysLoginLog{}, "username = ?", writers.loginEvent.Username))
				require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbRecordingSession{}, "node_id = ?", writers.session.NodeID))
				require.Zero(t, countRows(t, db, &gbmodels.GbSipMetricFlush{}, "1 = 1"))
				require.NoError(t, db.Exec(`DROP TRIGGER IF EXISTS "abort_metric"`).Error)
				flushMetric(t, ctx, writers.recorder)
				require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbSipMetricFlush{}, "1 = 1"))
			case "login":
				require.NoError(t, got.metric)
				require.ErrorContains(t, got.login, "injected write failure")
				require.NoError(t, got.recording)
				require.EqualValues(t, 2, countRows(t, db, &gbmodels.GbSipMetricFlush{}, "1 = 1"))
				require.Zero(t, countRows(t, db, &appmodels.SysLoginLog{}, "username = ?", writers.loginEvent.Username))
				require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbRecordingSession{}, "node_id = ?", confirmed.session.NodeID))
				require.EqualValues(t, 1, countRows(t, db, &gbmodels.GbRecordingSession{}, "node_id = ?", writers.session.NodeID))
			case "recording":
				require.NoError(t, got.metric)
				require.NoError(t, got.login)
				require.ErrorContains(t, got.recording, "injected write failure")
				require.EqualValues(t, 2, countRows(t, db, &gbmodels.GbSipMetricFlush{}, "1 = 1"))
				require.EqualValues(t, 1, countRows(t, db, &appmodels.SysLoginLog{}, "username = ?", confirmed.loginEvent.Username))
				require.EqualValues(t, 1, countRows(t, db, &appmodels.SysLoginLog{}, "username = ?", writers.loginEvent.Username))
				require.Zero(t, countRows(t, db, &gbmodels.GbRecordingSession{}, "node_id = ?", writers.session.NodeID))
			}
		})
	}
}

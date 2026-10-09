package logcleanup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbrepo "uvplatform.com/uvp-gb28181/app/gb28181/zlm/repo"
	appmodels "uvplatform.com/uvp-gb28181/app/models"
)

func newCleanerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&appmodels.SysOperationLog{}, &appmodels.SysLoginLog{}, &appmodels.SysJobResults{},
		&gbrepo.SchedulerLogDTO{}, &models.GbSipTraceMessage{}, &models.GbSipTraceSessionDiagnosis{},
		&models.GbSIPTraceCapture{}, &models.GbPlayAttempt{}, &models.GbPlayLifecycleEvent{},
	))
	return db
}

func TestCleanOperationPhysicallyDeletesExpiredRowsIncludingSoftDeletedAndPreservesCutoff(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	old := appmodels.SysOperationLog{Operation: "old", BaseModel: appmodels.BaseModel{CreatedAt: cutoff.Add(-time.Second)}}
	softDeleted := appmodels.SysOperationLog{Operation: "soft", BaseModel: appmodels.BaseModel{CreatedAt: cutoff.Add(-time.Hour)}}
	boundary := appmodels.SysOperationLog{Operation: "boundary", BaseModel: appmodels.BaseModel{CreatedAt: cutoff}}
	require.NoError(t, db.Create(&[]appmodels.SysOperationLog{old, softDeleted, boundary}).Error)
	require.NoError(t, db.Where("id = ?", softDeleted.ID).Delete(&appmodels.SysOperationLog{}).Error)

	deleted, err := Clean(context.Background(), db, Operation, cutoff, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 1, countRows(t, db.Unscoped(), &appmodels.SysOperationLog{}))
	var remaining appmodels.SysOperationLog
	require.NoError(t, db.First(&remaining).Error)
	require.Equal(t, "boundary", remaining.Operation)
}

func TestCleanLoginPhysicallyDeletesExpiredRowsIncludingSoftDeleted(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	deletedAt := cutoff.Add(-time.Minute)
	require.NoError(t, db.Create(&[]appmodels.SysLoginLog{
		loginRow("old", cutoff.Add(-time.Second), nil),
		loginRow("soft", cutoff.Add(-time.Hour), &deletedAt),
		loginRow("boundary", cutoff, nil),
	}).Error)

	deleted, err := Clean(context.Background(), db, Login, cutoff, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 1, countRows(t, db.Unscoped(), &appmodels.SysLoginLog{}))
}

func TestCleanJobDeletesOnlyEndedResultsStrictlyBeforeCutoff(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	old, boundary := cutoff.Add(-time.Second), cutoff
	require.NoError(t, db.Create(&[]appmodels.SysJobResults{
		{JobId: "old", Status: "success", StartTime: &old, EndTime: &old},
		{JobId: "boundary", Status: "success", StartTime: &boundary, EndTime: &boundary},
	}).Error)

	deleted, err := Clean(context.Background(), db, Job, cutoff, 500)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.EqualValues(t, 1, countRows(t, db, &appmodels.SysJobResults{}))
}

func TestCleanSchedulerDeletesExpiredRowsInMoreThanTwoBatches(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	rows := make([]gbrepo.SchedulerLogDTO, 7)
	for i := range rows {
		rows[i] = gbrepo.SchedulerLogDTO{HappenedAt: cutoff.Add(-time.Duration(i+1) * time.Minute)}
	}
	rows = append(rows, gbrepo.SchedulerLogDTO{HappenedAt: cutoff})
	require.NoError(t, db.Create(&rows).Error)

	deleted, err := Clean(context.Background(), db, Scheduler, cutoff, 2)
	require.NoError(t, err)
	require.EqualValues(t, 7, deleted)
	require.EqualValues(t, 1, countRows(t, db, &gbrepo.SchedulerLogDTO{}))
}

func TestCleanSIPDeletesExpiredRowsButKeepsBoundaryAndActiveCaptureEvidence(t *testing.T) {
	db := newCleanerDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	cutoff := now.Add(-7 * 24 * time.Hour)
	activeStarted := cutoff.Add(-time.Hour)
	activePlannedEnd := now.Add(time.Hour)
	activeKey := "capture-active"
	endedAt := cutoff.Add(-time.Second)
	require.NoError(t, db.Create(&[]models.GbSIPTraceCapture{
		{ID: "active", DeviceCode: "device-active", StartedAt: activeStarted, PlannedEndAt: activePlannedEnd, ActiveKey: &activeKey},
		{ID: "ended", DeviceCode: "device-ended", StartedAt: cutoff.Add(-2 * time.Hour), PlannedEndAt: cutoff, EndedAt: &endedAt},
		{ID: "boundary", DeviceCode: "device-boundary", StartedAt: cutoff.Add(-time.Hour), PlannedEndAt: cutoff, EndedAt: &cutoff},
	}).Error)
	require.NoError(t, db.Create(&[]models.GbSipTraceMessage{
		sipMessage("old", "device-old", cutoff.Add(-time.Second)),
		sipMessage("active-evidence", "device-active", cutoff.Add(-time.Minute)),
		sipMessage("ended-evidence", "device-ended", cutoff.Add(-time.Minute)),
		sipMessage("boundary", "device-old", cutoff),
	}).Error)
	require.NoError(t, db.Create(&[]models.GbSipTraceSessionDiagnosis{
		diagnosisRow(1, "old-active-diagnosis", "device-old", cutoff.Add(-time.Second), "active"),
		diagnosisRow(2, "capture-diagnosis", "device-active", cutoff.Add(-time.Minute), "active"),
		diagnosisRow(3, "boundary-diagnosis", "device-old", cutoff, "resolved"),
	}).Error)

	deleted, err := Clean(context.Background(), db, SIP, cutoff, 1)
	require.NoError(t, err)
	require.EqualValues(t, 4, deleted) // two messages + one diagnosis + ended capture
	require.EqualValues(t, 2, countRows(t, db, &models.GbSipTraceMessage{}))
	require.EqualValues(t, 2, countRows(t, db, &models.GbSipTraceSessionDiagnosis{}))
	require.EqualValues(t, 2, countRows(t, db, &models.GbSIPTraceCapture{}))
	var captureDiagnosis models.GbSipTraceSessionDiagnosis
	require.NoError(t, db.First(&captureDiagnosis, "correlation_key = ?", "capture-diagnosis").Error)
	var activeMessage models.GbSipTraceMessage
	require.NoError(t, db.First(&activeMessage, "event_id = ?", "active-evidence").Error)
}

func TestCleanPlaybackDeletesWholeExpiredTerminalLifecycleAndOrphanEvents(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	old, boundary := cutoff.Add(-time.Second), cutoff
	require.NoError(t, db.Create(&[]models.GbPlayAttempt{
		{CorrelationID: "expired", LifecycleState: "completed", Outcome: "success", StartedAt: old, FinishedAt: &old},
		{CorrelationID: "recent-event", LifecycleState: "failed", Outcome: "failure", StartedAt: old, FinishedAt: &old},
		{CorrelationID: "active", LifecycleState: "in_progress", Outcome: "started", StartedAt: old},
		{CorrelationID: "boundary", LifecycleState: "completed", Outcome: "success", StartedAt: boundary, FinishedAt: &boundary},
	}).Error)
	require.NoError(t, db.Create(&[]models.GbPlayLifecycleEvent{
		playEvent("expired-event", "expired", old),
		playEvent("recent-event-old", "recent-event", old),
		playEvent("recent-event-new", "recent-event", cutoff.Add(time.Second)),
		playEvent("active-event", "active", old),
		playEvent("boundary-event", "boundary", old),
		playEvent("orphan", "missing-attempt", old),
	}).Error)

	deleted, err := Clean(context.Background(), db, Playback, cutoff, 2)
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted) // expired attempt + its event + orphan event
	require.EqualValues(t, 3, countRows(t, db, &models.GbPlayAttempt{}))
	require.EqualValues(t, 4, countRows(t, db, &models.GbPlayLifecycleEvent{}))
	var retained models.GbPlayAttempt
	require.NoError(t, db.First(&retained, "correlation_id = ?", "recent-event").Error)
}

func TestCleanReturnsCommittedBatchCountWhenLaterBatchFails(t *testing.T) {
	db := newCleanerDB(t)
	cutoff := time.Now().UTC()
	for i := 0; i < 5; i++ {
		row := appmodels.SysOperationLog{Operation: fmt.Sprintf("old-%d", i), BaseModel: appmodels.BaseModel{CreatedAt: cutoff.Add(-time.Hour)}}
		require.NoError(t, db.Create(&row).Error)
	}
	deleteCalls := 0
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:fail_second_cleaner_batch", func(tx *gorm.DB) {
		deleteCalls++
		if deleteCalls == 2 {
			tx.AddError(errors.New("injected second batch failure"))
		}
	}))

	deleted, err := Clean(context.Background(), db, Operation, cutoff, 2)
	require.ErrorContains(t, err, "injected second batch failure")
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 3, countRows(t, db.Unscoped(), &appmodels.SysOperationLog{}))
}

func TestCleanReturnsCommittedBatchCountWhenCanceledBetweenBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cleaner-cancel.sqlite")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&appmodels.SysOperationLog{}))
	cutoff := time.Now().UTC()
	for i := 0; i < 5; i++ {
		row := appmodels.SysOperationLog{Operation: fmt.Sprintf("old-%d", i), BaseModel: appmodels.BaseModel{CreatedAt: cutoff.Add(-time.Hour)}}
		require.NoError(t, db.Create(&row).Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	selectCalls := 0
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:cancel_before_second_cleaner_batch", func(tx *gorm.DB) {
		selectCalls++
		if selectCalls == 2 {
			cancel()
		}
	}))

	deleted, err := Clean(ctx, db, Operation, cutoff, 2)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 3, countRows(t, db.Unscoped(), &appmodels.SysOperationLog{}))
}

func TestCleanHonorsCanceledContextAndRejectsUnknownKind(t *testing.T) {
	db := newCleanerDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deleted, err := Clean(ctx, db, Operation, time.Now(), 500)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, deleted)

	deleted, err = Clean(context.Background(), db, Kind("other"), time.Now(), 500)
	require.Error(t, err)
	require.Zero(t, deleted)
}

func TestCleanErrorsWhenRequiredLogTableIsMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	tests := []struct {
		kind  Kind
		table string
	}{
		{kind: Operation, table: "sys_operation_logs"},
		{kind: Login, table: "sys_login_logs"},
		{kind: Job, table: "sys_job_results"},
		{kind: Scheduler, table: "scheduler_log"},
		{kind: SIP, table: "gb_sip_trace_message"},
		{kind: Playback, table: "gb_play_attempt"},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			deleted, err := Clean(context.Background(), db, test.kind, time.Now().UTC(), 500)
			require.ErrorContains(t, err, test.table)
			require.Zero(t, deleted)
		})
	}
}

func TestCleanChecksSIPAndPlaybackProtectionTablesBeforeDeleting(t *testing.T) {
	cutoff := time.Now().UTC()
	t.Run("missing SIP diagnosis table", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&models.GbSipTraceMessage{}))
		oldMessage := sipMessage("old", "device", cutoff.Add(-time.Hour))
		require.NoError(t, db.Create(&oldMessage).Error)

		deleted, err := Clean(context.Background(), db, SIP, cutoff, 500)
		require.ErrorContains(t, err, "gb_sip_trace_session_diagnosis")
		require.Zero(t, deleted)
		require.EqualValues(t, 1, countRows(t, db, &models.GbSipTraceMessage{}))
	})

	t.Run("missing SIP capture table", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&models.GbSipTraceMessage{}, &models.GbSipTraceSessionDiagnosis{}))
		oldMessage := sipMessage("old", "device", cutoff.Add(-time.Hour))
		require.NoError(t, db.Create(&oldMessage).Error)

		deleted, err := Clean(context.Background(), db, SIP, cutoff, 500)
		require.ErrorContains(t, err, "gb_sip_trace_capture")
		require.Zero(t, deleted)
		require.EqualValues(t, 1, countRows(t, db, &models.GbSipTraceMessage{}))
	})

	t.Run("missing playback event table", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&models.GbPlayAttempt{}))
		old := cutoff.Add(-time.Hour)
		attempt := models.GbPlayAttempt{CorrelationID: "expired", LifecycleState: "completed", Outcome: "success", StartedAt: old, FinishedAt: &old}
		require.NoError(t, db.Create(&attempt).Error)

		deleted, err := Clean(context.Background(), db, Playback, cutoff, 500)
		require.ErrorContains(t, err, "gb_play_lifecycle_event")
		require.Zero(t, deleted)
		require.EqualValues(t, 1, countRows(t, db, &models.GbPlayAttempt{}))
	})
}

func TestCleanupCorrelatedQueriesBuildWithSQLServerDialect(t *testing.T) {
	dryRunConn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	db, err := gorm.Open(sqlserver.New(sqlserver.Config{Conn: dryRunConn.ConnPool}), &gorm.Config{
		DryRun: true, DisableAutomaticPing: true,
	})
	require.NoError(t, err)

	cutoff := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	now := cutoff.Add(24 * time.Hour)
	messageCondition := `occurred_at < ? AND NOT EXISTS (
		SELECT 1 FROM gb_sip_trace_capture AS capture
		WHERE capture.device_code = gb_sip_trace_message.device_id
		  AND capture.active_key IS NOT NULL AND capture.active_key <> ''
		  AND capture.ended_at IS NULL AND capture.planned_end_at > ?
		  AND gb_sip_trace_message.occurred_at >= capture.started_at
		  AND gb_sip_trace_message.occurred_at <= capture.planned_end_at
	)`
	var messageIDs []string
	messageQuery := db.Model(&models.GbSipTraceMessage{}).Where(messageCondition, cutoff, now).
		Order("occurred_at ASC").Limit(500).Pluck("event_id", &messageIDs)
	require.NoError(t, messageQuery.Error)
	require.Contains(t, messageQuery.Statement.SQL.String(), "NOT EXISTS")
	require.Contains(t, messageQuery.Statement.SQL.String(), "gb_sip_trace_capture AS capture")

	lifecycleIDs := []string{"life-1"}
	eventDelete := db.Where("lifecycle_id IN ? AND NOT EXISTS (SELECT 1 FROM gb_play_attempt AS attempt WHERE attempt.correlation_id = gb_play_lifecycle_event.lifecycle_id)", lifecycleIDs).
		Delete(&models.GbPlayLifecycleEvent{})
	require.NoError(t, eventDelete.Error)
	require.Contains(t, eventDelete.Statement.SQL.String(), `DELETE FROM "gb_play_lifecycle_event"`)
	require.Contains(t, eventDelete.Statement.SQL.String(), "NOT EXISTS")
	require.Contains(t, eventDelete.Statement.SQL.String(), "gb_play_attempt AS attempt")
	require.Contains(t, eventDelete.Statement.SQL.String(), "gb_play_lifecycle_event.lifecycle_id")
}

func loginRow(username string, createdAt time.Time, deletedAt *time.Time) appmodels.SysLoginLog {
	return appmodels.SysLoginLog{
		Username: username, Result: "success", IP: "127.0.0.1", Location: "local",
		Browser: "test", OS: "test", CreatedAt: createdAt, DeletedAt: deletedAt,
	}
}

func sipMessage(eventID, deviceID string, occurredAt time.Time) models.GbSipTraceMessage {
	return models.GbSipTraceMessage{
		EventID: eventID, DeviceID: deviceID, OccurredAt: occurredAt, Direction: "in", Transport: "udp",
		LocalAddr: "127.0.0.1:5060", RemoteAddr: "127.0.0.2:5060", Method: "REGISTER",
		CallID: eventID, CSeqMethod: "REGISTER", FromURI: "sip:a", ToURI: "sip:b", FromID: "a", ToID: "b",
		BusinessCode: "register", BusinessType: "register", BusinessConfidence: "high",
		PayloadNonce: []byte{1}, PayloadCiphertext: []byte{1}, PayloadAlgorithm: "AES-GCM",
		PayloadKeyVersion: "v1", PayloadDigestSHA256: "digest",
	}
}

func diagnosisRow(id uint64, key, deviceID string, observedAt time.Time, state string) models.GbSipTraceSessionDiagnosis {
	return models.GbSipTraceSessionDiagnosis{
		ID: id, SessionDay: time.Date(observedAt.Year(), observedAt.Month(), observedAt.Day(), 0, 0, 0, 0, time.UTC),
		ObservedAt: observedAt, CorrelationKey: key, State: state, Category: "play", Code: "timeout",
		Stage: "media", Source: "runtime", DeviceID: deviceID,
	}
}

func playEvent(eventID, lifecycleID string, eventAt time.Time) models.GbPlayLifecycleEvent {
	return models.GbPlayLifecycleEvent{
		EventID: eventID, LifecycleID: lifecycleID, Sequence: int64(eventAt.UnixNano()), EventAt: eventAt, Stage: "media",
		EventName: "media_ready", FactState: "confirmed", Source: "play_service",
		DeviceCode: "device", ChannelCode: "channel", CreatedAt: eventAt,
	}
}

func countRows(t *testing.T, db *gorm.DB, model any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	return count
}

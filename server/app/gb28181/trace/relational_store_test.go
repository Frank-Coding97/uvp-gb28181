package trace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newRelationalStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	dbConn, err := db.DB()
	require.NoError(t, err)
	dbConn.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipTraceMessage{}))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipTraceSessionDiagnosis{}))
	return db
}

func TestRelationalStoreInsertBatchPersistsEncryptedEvents(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	event := StoredEvent{
		EventID: "019f7a0c-a48d-7ddb-a44d-30a8ab2eed39", OccurredAt: time.Date(2026, 8, 10, 10, 0, 0, 123456000, time.FixedZone("CST", 8*60*60)),
		Direction: DirectionInbound, Transport: "UDP", LocalAddr: "127.0.0.1:5060", RemoteAddr: "127.0.0.1:15060",
		DeviceID: "34020000001320000001", Method: "REGISTER", StatusCode: 401, CallID: "call-1", CSeq: 7, CSeqMethod: "REGISTER",
		FromURI: "device@example", ToURI: "platform@example", UserAgent: "fixture", Malformed: true, ParseError: "safe error",
		Payload: EncryptedPayload{Nonce: []byte{1, 2}, Ciphertext: []byte{3, 4}, Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("a", 64)},
	}

	require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{event}))
	var row gbmodels.GbSipTraceMessage
	require.NoError(t, db.First(&row, "event_id = ?", event.EventID).Error)
	require.Equal(t, event.OccurredAt.UTC(), row.OccurredAt.UTC())
	require.Equal(t, event.CallID, row.CallID)
	require.Equal(t, event.Payload.Nonce, row.PayloadNonce)
	require.Equal(t, event.Payload.Ciphertext, row.PayloadCiphertext)
	require.Equal(t, event.Payload.DigestSHA256, row.PayloadDigestSHA256)
}

func TestRelationalStoreInsertBatchEmptyAndDuplicate(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	require.NoError(t, store.InsertBatch(context.Background(), nil))

	event := StoredEvent{
		EventID: "019f7a0c-a48d-7ddb-a44d-30a8ab2eed39", OccurredAt: time.Now().UTC(),
		Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte{2}, Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("b", 64)},
	}
	require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{event}))
	require.Error(t, store.InsertBatch(context.Background(), []StoredEvent{event}))

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbSipTraceMessage{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestNewRelationalStoreRejectsNilDB(t *testing.T) {
	_, err := NewRelationalStore(nil)
	require.ErrorIs(t, err, ErrTraceStoreUnavailable)
}

func TestRelationalStoreQueryCursorFiltersAndDetail(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	events := []StoredEvent{
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed37", at, "device-a", "call-a", "REGISTER", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed38", at, "device-a", "call-a", "REGISTER", 401),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed39", at.Add(time.Second), "device-b", "call-b", "INVITE", 0),
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))

	filter := MessageFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), DeviceIDs: []string{"device-a", "device-b"}, Keyword: "CALL", Limit: 2}
	page, err := store.ListMessages(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotEmpty(t, page.NextCursor)
	require.Equal(t, events[2].EventID, page.Items[0].EventID)

	filter.Cursor = page.NextCursor
	next, err := store.ListMessages(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Equal(t, events[0].EventID, next.Items[0].EventID)

	detail, err := store.GetMessage(t.Context(), events[1].EventID)
	require.NoError(t, err)
	require.Equal(t, events[1].Payload.Ciphertext, detail.Payload.Ciphertext)
	_, err = store.GetMessage(t.Context(), "019f7a0c-a48d-7ddb-a44d-30a8ab2eed40")
	require.ErrorIs(t, err, ErrTraceMessageNotFound)
}

func TestRelationalStoreDerivesSessionsAndUnpagedStats(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	events := []StoredEvent{
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed31", at, "device-a", "register", "REGISTER", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed32", at.Add(time.Second), "device-a", "register", "REGISTER", 401),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed33", at.Add(2*time.Second), "device-a", "register", "REGISTER", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed34", at.Add(3*time.Second), "device-a", "register", "REGISTER", 200),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed35", at.Add(4*time.Second), "device-a", "invite", "INVITE", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed36", at.Add(5*time.Second), "device-b", "message", "MESSAGE", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed37", at.Add(6*time.Second), "device-b", "message", "MESSAGE", 200),
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))
	require.NoError(t, db.Create(&gbmodels.GbSipTraceSessionDiagnosis{
		SessionDay: at.Truncate(24 * time.Hour), ObservedAt: at.Add(7 * time.Second),
		CorrelationKey: "play-request", State: "active", Category: "play_stuck", Code: "signaling_timeout",
		Stage: "signaling", Source: "runtime", DeviceID: "device-a", CallID: "invite", CSeq: 1, Method: "INVITE",
	}).Error)
	filter := SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), Limit: 2}

	sessions, err := store.ListSessions(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, "message", sessions[0].CallID)
	require.False(t, sessions[0].Anomaly)
	require.Equal(t, "invite", sessions[1].CallID)
	require.True(t, sessions[1].MissingResponse)

	stats, err := store.GetSessionStats(t.Context(), filter)
	require.NoError(t, err)
	require.Equal(t, SessionStats{Total: 3, Anomaly: 1, RegisterFail: 0, PlayStuck: 1, InvitePending: 1}, stats)

	filter.Anomaly = true
	filter.Limit = 10
	sessions, err = store.ListSessions(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "invite", sessions[0].CallID)
}

func TestRelationalStoreFiltersActiveDiagnosisBeforePagination(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	events := make([]StoredEvent, 301)
	for i := range events {
		events[i] = testRelationalEvent(
			fmt.Sprintf("event-diagnosis-%03d", i), at.Add(time.Duration(i)*time.Second),
			"device-a", fmt.Sprintf("call-diagnosis-%03d", i), "MESSAGE", 200,
		)
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))
	require.NoError(t, db.Create(&gbmodels.GbSipTraceSessionDiagnosis{
		SessionDay: at.Truncate(24 * time.Hour), ObservedAt: at.Add(time.Second),
		CorrelationKey: "register-attempt", State: "active", Category: "register_failure", Code: "digest_failure",
		Stage: "register", Source: "runtime", DeviceID: "device-a", CallID: "call-diagnosis-001", CSeq: 1, Method: "REGISTER",
	}).Error)

	filter := SessionFilter{
		From: at.Add(-time.Minute), To: at.Add(time.Hour), Limit: 20,
		DiagnosisCategory: "register_failure", DiagnosisCode: "digest_failure",
	}
	sessions, err := store.ListSessions(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "call-diagnosis-001", sessions[0].CallID)
	require.NotNil(t, sessions[0].Diagnosis)
	require.Equal(t, "digest_failure", string(sessions[0].Diagnosis.Code))

	stats, err := store.GetSessionStats(t.Context(), filter)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Total)
	require.EqualValues(t, 1, stats.RegisterFail)
}

func TestRelationalStoreExcludesResolvedDiagnosisAndReconstructsOnlyClearRegisterFailure(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
		testRelationalEvent("challenge-request", at, "device-a", "challenge", "REGISTER", 0),
		testRelationalEvent("challenge-response", at.Add(time.Second), "device-a", "challenge", "REGISTER", 401),
		testRelationalEvent("failure-request", at.Add(2*time.Second), "device-a", "failure", "REGISTER", 0),
		testRelationalEvent("failure-response", at.Add(3*time.Second), "device-a", "failure", "REGISTER", 403),
		testRelationalEvent("invite-request", at.Add(4*time.Second), "device-a", "invite", "INVITE", 0),
	}))
	require.NoError(t, db.Create(&gbmodels.GbSipTraceSessionDiagnosis{
		SessionDay: at.Truncate(24 * time.Hour), ObservedAt: at.Add(5 * time.Second),
		CorrelationKey: "resolved-attempt", State: "resolved", Category: "play_stuck", Code: "media_timeout",
		Stage: "media", Source: "runtime", DeviceID: "device-a", CallID: "invite", CSeq: 1, Method: "INVITE",
	}).Error)

	sessions, err := store.ListSessions(t.Context(), SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), Limit: 10})
	require.NoError(t, err)
	byCallID := make(map[string]SessionSummary, len(sessions))
	for _, session := range sessions {
		byCallID[session.CallID] = session
	}
	require.False(t, byCallID["challenge"].Anomaly)
	require.Nil(t, byCallID["challenge"].Diagnosis)
	require.Nil(t, byCallID["invite"].Diagnosis)
	require.NotNil(t, byCallID["failure"].Diagnosis)
	require.Equal(t, "register_failure", string(byCallID["failure"].Diagnosis.Category))
	require.Equal(t, "undetermined", string(byCallID["failure"].Diagnosis.Code))
	require.Equal(t, "reconstructed", string(byCallID["failure"].Diagnosis.Source))

	stats, err := store.GetSessionStats(t.Context(), SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute)})
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.RegisterFail)
	require.Zero(t, stats.PlayStuck)
	require.Equal(t, stats.PlayStuck, stats.InvitePending)
}

func TestRelationalStoreListSessionsBoundsCandidateRows(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	events := make([]StoredEvent, 50)
	for i := range events {
		events[i] = testRelationalEvent(
			fmt.Sprintf("event-%03d", i),
			at.Add(time.Duration(i)*time.Second),
			"device-a",
			fmt.Sprintf("call-%03d", i),
			"MESSAGE",
			200,
		)
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))

	var rowsRead []int64
	require.NoError(t, db.Callback().Query().After("gorm:after_query").Register("test:capture-session-query-rows", func(tx *gorm.DB) {
		rowsRead = append(rowsRead, tx.RowsAffected)
	}))

	sessions, err := store.ListSessions(t.Context(), SessionFilter{
		From:  at.Add(-time.Minute),
		To:    at.Add(time.Minute),
		Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "call-049", sessions[0].CallID)
	require.NotEmpty(t, rowsRead)
	for _, rows := range rowsRead {
		require.LessOrEqual(t, rows, int64(1), "session list query must not load unrelated sessions")
	}
}

func TestRelationalStoreListSessionsKeepsDailySessionIdentity(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	firstDay := time.Date(2026, 8, 9, 23, 59, 0, 0, time.UTC)
	secondDay := firstDay.Add(2 * time.Minute)
	require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
		testRelationalEvent("event-day-1", firstDay, "device-a", "same-call", "MESSAGE", 200),
		testRelationalEvent("event-day-2", secondDay, "device-a", "same-call", "MESSAGE", 200),
	}))

	sessions, err := store.ListSessions(t.Context(), SessionFilter{
		From:  firstDay.Add(-time.Minute),
		To:    secondDay.Add(time.Minute),
		Limit: 2,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, secondDay.Truncate(24*time.Hour), sessions[0].Day)
	require.Equal(t, firstDay.Truncate(24*time.Hour), sessions[1].Day)
}

func testRelationalEvent(id string, at time.Time, deviceID, callID, method string, status uint16) StoredEvent {
	direction := DirectionInbound
	if status >= 100 {
		direction = DirectionOutbound
	}
	return StoredEvent{
		EventID: id, OccurredAt: at, Direction: direction, Transport: "UDP", LocalAddr: "platform:5060", RemoteAddr: "device:15060",
		DeviceID: deviceID, Method: method, StatusCode: status, CallID: callID, CSeq: 1, CSeqMethod: method,
		FromURI: "from@example", ToURI: "to@example", UserAgent: "fixture",
		Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte(id), Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("c", 64)},
	}
}

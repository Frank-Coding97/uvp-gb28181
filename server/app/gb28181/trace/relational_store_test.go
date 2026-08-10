package trace

import (
	"context"
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
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipTraceMessage{}))
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
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed32", at.Add(time.Second), "device-a", "register", "REGISTER", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed33", at.Add(2*time.Second), "device-a", "register", "REGISTER", 401),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed34", at.Add(3*time.Second), "device-a", "invite", "INVITE", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed35", at.Add(4*time.Second), "device-b", "message", "MESSAGE", 0),
		testRelationalEvent("019f7a0c-a48d-7ddb-a44d-30a8ab2eed36", at.Add(5*time.Second), "device-b", "message", "MESSAGE", 200),
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))
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
	require.Equal(t, SessionStats{Total: 3, Anomaly: 2, RegisterFail: 1, InvitePending: 1}, stats)

	filter.Anomaly = true
	filter.Limit = 10
	sessions, err = store.ListSessions(t.Context(), filter)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
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

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

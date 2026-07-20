package trace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClickHouseBatch struct {
	rows    [][]any
	sent    bool
	aborted bool
}

func (b *fakeClickHouseBatch) Append(values ...any) error {
	b.rows = append(b.rows, append([]any(nil), values...))
	return nil
}

func (b *fakeClickHouseBatch) Send() error {
	b.sent = true
	return nil
}

func (b *fakeClickHouseBatch) Abort() error {
	b.aborted = true
	return nil
}

func (b *fakeClickHouseBatch) Close() error { return nil }

type fakeClickHouseConn struct {
	execQueries []string
	batchQuery  string
	batch       *fakeClickHouseBatch
}

type closeRecordingStore struct {
	recordingStore
	closed bool
}

func (s *closeRecordingStore) Close() error {
	s.closed = true
	return nil
}

func (c *fakeClickHouseConn) Exec(_ context.Context, query string, _ ...any) error {
	c.execQueries = append(c.execQueries, query)
	return nil
}

func (c *fakeClickHouseConn) PrepareBatch(_ context.Context, query string) (clickHouseBatch, error) {
	c.batchQuery = query
	c.batch = &fakeClickHouseBatch{}
	return c.batch, nil
}

func (c *fakeClickHouseConn) Query(context.Context, string, ...any) (clickHouseRows, error) {
	return nil, nil
}

func (c *fakeClickHouseConn) Ping(context.Context) error { return nil }
func (c *fakeClickHouseConn) Close() error               { return nil }

func TestClickHouseSchemaUsesDailyPartitionAndSevenDayTTL(t *testing.T) {
	conn := &fakeClickHouseConn{}
	store, err := NewClickHouseStoreWithConn(conn, "uvp_sip_trace")
	require.NoError(t, err)
	require.NoError(t, store.EnsureSchema(context.Background()))
	require.NotEmpty(t, conn.execQueries)
	ddl := conn.execQueries[0]
	require.Contains(t, ddl, "uvp_sip_trace.sip_trace_message")
	require.Contains(t, ddl, "PARTITION BY toYYYYMMDD(occurred_at)")
	require.Contains(t, ddl, "TTL occurred_at + INTERVAL 7 DAY")
	require.Contains(t, ddl, "ORDER BY (occurred_at, event_id)")

	_, err = NewClickHouseStoreWithConn(conn, "uvp_sip_trace; DROP DATABASE default")
	require.ErrorIs(t, err, ErrInvalidClickHouseIdentifier)
}

func TestClickHouseInsertBatchWritesEncryptedColumns(t *testing.T) {
	conn := &fakeClickHouseConn{}
	store, err := NewClickHouseStoreWithConn(conn, "uvp_sip_trace")
	require.NoError(t, err)
	event := StoredEvent{
		EventID:    "019f7a0c-a48d-7ddb-a44d-30a8ab2eed39",
		OccurredAt: time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC),
		Direction:  DirectionInbound,
		Transport:  "UDP",
		LocalAddr:  "127.0.0.1:5060",
		RemoteAddr: "127.0.0.1:15060",
		DeviceID:   "34020000001320000001",
		Method:     "REGISTER",
		CallID:     "call-1",
		CSeq:       1,
		CSeqMethod: "REGISTER",
		Payload: EncryptedPayload{
			Ciphertext: []byte{1, 2, 3}, Nonce: []byte{4, 5}, Algorithm: EncryptionAES256GCM,
			KeyVersion: "v1", DigestSHA256: strings.Repeat("a", 64),
		},
	}

	require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{event}))
	require.True(t, conn.batch.sent)
	require.Len(t, conn.batch.rows, 1)
	require.Contains(t, conn.batchQuery, "INSERT INTO uvp_sip_trace.sip_trace_message")
	require.Equal(t, []byte{1, 2, 3}, conn.batch.rows[0][15])
	require.NotContains(t, conn.batchQuery, "call-1")
}

func TestMessageQueryRequiresBoundedTimeAndParameterizesFilters(t *testing.T) {
	_, _, err := buildMessageListQuery("uvp_sip_trace.sip_trace_message", MessageFilter{})
	require.ErrorIs(t, err, ErrTraceTimeRangeRequired)

	maliciousCallID := "x' OR 1=1 --"
	filter := MessageFilter{
		From:       time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		To:         time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		DeviceID:   "34020000001320000001",
		CallID:     maliciousCallID,
		Direction:  DirectionInbound,
		Method:     "REGISTER",
		StatusCode: 401,
		Limit:      10_000,
	}
	query, args, err := buildMessageListQuery("uvp_sip_trace.sip_trace_message", filter)
	require.NoError(t, err)
	require.NotContains(t, query, maliciousCallID)
	require.Contains(t, query, "device_id = ?")
	require.Contains(t, query, "call_id = ?")
	require.Contains(t, query, "direction = ?")
	require.Contains(t, query, "method = ?")
	require.Contains(t, query, "status_code = ?")
	require.Contains(t, query, "LIMIT ?")
	require.Contains(t, args, maliciousCallID)
	require.Equal(t, MaxMessagePageSize+1, args[len(args)-1])
}

func TestMessageQuerySupportsMultipleDevicesAndStatusRange(t *testing.T) {
	filter := MessageFilter{
		From:      time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		To:        time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		DeviceIDs: []string{"device-a", "device-b"}, StatusMin: 400, StatusMax: 499, Limit: 10,
	}
	query, args, err := buildMessageListQuery("uvp_sip_trace.sip_trace_message", filter)
	require.NoError(t, err)
	require.Contains(t, query, "device_id IN (?, ?)")
	require.Contains(t, query, "status_code >= ?")
	require.Contains(t, query, "status_code <= ?")
	require.Contains(t, args, "device-a")
	require.Contains(t, args, "device-b")
	require.Contains(t, args, uint16(400))
	require.Contains(t, args, uint16(499))
}

func TestMessageCursorRoundTripAndRejectsInvalidInput(t *testing.T) {
	cursor := MessageCursor{
		OccurredAt: time.Date(2026, 7, 19, 12, 30, 45, 123456000, time.UTC),
		EventID:    "019f7a0c-a48d-7ddb-a44d-30a8ab2eed39",
	}
	encoded := EncodeMessageCursor(cursor)
	decoded, err := DecodeMessageCursor(encoded)
	require.NoError(t, err)
	require.Equal(t, cursor, decoded)
	_, err = DecodeMessageCursor("not-a-cursor")
	require.ErrorIs(t, err, ErrInvalidMessageCursor)
}

func TestExtractSIPMetadataForRequestResponseAndMalformedPayload(t *testing.T) {
	request := []byte("REGISTER sip:34020000002000000001@3402000000 SIP/2.0\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=1\r\n" +
		"To: <sip:34020000002000000001@3402000000>\r\n" +
		"Call-ID: request-call\r\nCSeq: 7 REGISTER\r\nContent-Length: 0\r\n\r\n")
	metadata := extractSIPMetadata(request, DirectionInbound)
	require.Equal(t, "34020000001320000001", metadata.DeviceID)
	require.Equal(t, "REGISTER", metadata.Method)
	require.Equal(t, "request-call", metadata.CallID)
	require.EqualValues(t, 7, metadata.CSeq)
	require.Empty(t, metadata.ParseError)

	response := []byte("SIP/2.0 401 Unauthorized\r\n" +
		"From: <sip:34020000001320000001@3402000000>;tag=1\r\n" +
		"To: <sip:34020000002000000001@3402000000>;tag=2\r\n" +
		"Call-ID: response-call\r\nCSeq: 8 REGISTER\r\nContent-Length: 0\r\n\r\n")
	metadata = extractSIPMetadata(response, DirectionOutbound)
	require.EqualValues(t, 401, metadata.StatusCode)
	require.Equal(t, "34020000001320000001", metadata.DeviceID)
	require.Equal(t, "REGISTER", metadata.Method)
	require.Equal(t, "REGISTER", metadata.CSeqMethod)

	metadata = extractSIPMetadata([]byte("not SIP credentials=secret"), DirectionInbound)
	require.NotEmpty(t, metadata.ParseError)
	require.NotContains(t, metadata.ParseError, "credentials=secret")
}

func TestExtractSIPMetadataUsesRemoteDeviceForResponseDirection(t *testing.T) {
	inbound := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:34020000002000000001@3402000000>;tag=platform\r\n" +
		"To: <sip:34020000001320000001@3402000000>;tag=device\r\n" +
		"Call-ID: inbound-response\r\nCSeq: 8 MESSAGE\r\nContent-Length: 0\r\n\r\n")
	outbound := []byte("SIP/2.0 200 OK\r\n" +
		"From: <sip:34020000001320000002@3402000000>;tag=device\r\n" +
		"To: <sip:34020000002000000001@3402000000>;tag=platform\r\n" +
		"Call-ID: outbound-response\r\nCSeq: 9 MESSAGE\r\nContent-Length: 0\r\n\r\n")

	require.Equal(t, "34020000001320000001", extractSIPMetadata(inbound, DirectionInbound).DeviceID)
	require.Equal(t, "34020000001320000002", extractSIPMetadata(outbound, DirectionOutbound).DeviceID)
}

func TestModuleShutdownClosesClickHouseStore(t *testing.T) {
	store := &closeRecordingStore{}
	module := NewModule(testTraceConfig(4, 1, 5), store, testPayloadCipher())
	require.NoError(t, module.Shutdown(context.Background()))
	require.True(t, store.closed)
}

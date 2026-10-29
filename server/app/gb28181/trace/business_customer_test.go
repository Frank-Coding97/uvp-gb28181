package trace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestClassifyCustomerBusinessAndTransportPayloads(t *testing.T) {
	tests := []struct {
		name  string
		raw   []byte
		code  BusinessCode
		label string
	}{
		{
			name: "config download",
			raw: []byte("MESSAGE sip:device SIP/2.0\r\nContent-Type: Application/MANSCDP+xml\r\n\r\n" +
				"<Query><CmdType>ConfigDownload</CmdType></Query>"),
			code: BusinessCode("config_download"), label: "配置查询",
		},
		{name: "CRLF whitespace keepalive", raw: []byte("\r\n \t\r\n"), code: BusinessCode("transport_keepalive"), label: "传输保活"},
		{name: "STUN binding request", raw: validSTUNBindingRequest(), code: BusinessCode("stun_binding"), label: "STUN探测"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifySIPBusiness(tt.raw)
			require.Equal(t, tt.code, got.Code)
			require.Equal(t, tt.label, got.Label)
			require.Equal(t, "high", got.Confidence)

			metadata := extractSIPMetadata(tt.raw, DirectionInbound)
			require.Equal(t, tt.code, metadata.BusinessCode)
			require.Empty(t, metadata.ParseError)
		})
	}
}

func TestClassifyCustomerTransportPayloadsDoesNotGuess(t *testing.T) {
	for _, raw := range [][]byte{
		{0, 1, 0, 0, 0, 0, 0, 0},                         // invalid STUN cookie and truncated header
		{0x80, 0x00, 0x00, 0x00, 0x21, 0x12, 0xa4, 0x42}, // not a STUN binding request
		[]byte("\n"), // whitespace without a CRLF keepalive marker
	} {
		require.Equal(t, BusinessUnknown, classifySIPBusiness(raw).Code)
	}
}

func TestFrameAssemblerKeepsValidTransportFramesNonMalformed(t *testing.T) {
	assembler := NewFrameAssembler(1024)
	props := sip.TransportReadProps{Transport: "UDP"}
	for _, raw := range [][]byte{[]byte("\r\n"), validSTUNBindingRequest()} {
		frames := assembler.Push(props, raw)
		require.Len(t, frames, 1)
		require.False(t, frames[0].Malformed)
		require.Empty(t, frames[0].Error)
	}

	tcpProps := sip.TransportReadProps{Transport: "TCP"}
	frames := assembler.Push(tcpProps, []byte("\r\n"))
	require.Len(t, frames, 1)
	require.False(t, frames[0].Malformed)
}

func TestFrameAssemblerDoesNotTreatTCPBodyCRLFAsTransportKeepalive(t *testing.T) {
	assembler := NewFrameAssembler(1024)
	props := sip.TransportReadProps{Transport: "TCP"}
	header := []byte("MESSAGE sip:device SIP/2.0\r\nContent-Length: 4\r\n\r\n")

	frames := assembler.Push(props, header)
	require.Empty(t, frames)
	require.Equal(t, 1, assembler.StreamCount())

	frames = assembler.Push(props, []byte("\r\n"))
	require.Empty(t, frames)
	require.Equal(t, 1, assembler.StreamCount())

	frames = assembler.Push(props, []byte("xy"))
	require.Len(t, frames, 1)
	require.False(t, frames[0].Malformed)
	require.Equal(t, append(append([]byte(nil), header...), []byte("\r\nxy")...), frames[0].Data)
	require.Zero(t, assembler.StreamCount())
}

func TestExtractSIPMetadataMarksOnlyEmptyResponseBody(t *testing.T) {
	response := []byte("SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP platform:5060\r\n" +
		"From: <sip:platform@local>;tag=1\r\nTo: <sip:device@local>;tag=2\r\n" +
		"Call-ID: empty-response\r\nCSeq: 7 MESSAGE\r\nContent-Length: 0\r\n\r\n")
	metadata := extractSIPMetadata(response, DirectionInbound)
	require.True(t, metadata.ResponseBodyEmpty)
	require.Equal(t, uint16(200), metadata.StatusCode)

	responseWithBody := []byte("SIP/2.0 200 OK\r\nVia: SIP/2.0/UDP platform:5060\r\n" +
		"From: <sip:platform@local>;tag=1\r\nTo: <sip:device@local>;tag=2\r\n" +
		"Call-ID: nonempty-response\r\nCSeq: 7 INVITE\r\nContent-Length: 2\r\n\r\nok")
	require.False(t, extractSIPMetadata(responseWithBody, DirectionInbound).ResponseBodyEmpty)
}

func TestRelationalStoreCorrelatesEmptyResponseWithUniqueRequest(t *testing.T) {
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	t.Run("same batch", func(t *testing.T) {
		db := newRelationalStoreTestDB(t)
		store, err := NewRelationalStore(db)
		require.NoError(t, err)
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
			customerConfigRequest("config-request", at),
			customerEmptyResponse("config-response", at.Add(time.Second), DirectionOutbound),
		}))
		assertCustomerResponseBusiness(t, db, "config-response", BusinessCode("config_download"), "配置查询")
	})

	t.Run("same timestamp in same batch", func(t *testing.T) {
		db := newRelationalStoreTestDB(t)
		store, err := NewRelationalStore(db)
		require.NoError(t, err)
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
			customerConfigRequest("same-time-config-request", at),
			customerEmptyResponse("same-time-config-response", at, DirectionOutbound),
		}))
		assertCustomerResponseBusiness(t, db, "same-time-config-response", BusinessCode("config_download"), "配置查询")
	})

	t.Run("previous batch", func(t *testing.T) {
		db := newRelationalStoreTestDB(t)
		store, err := NewRelationalStore(db)
		require.NoError(t, err)
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{customerConfigRequest("prior-request", at)}))
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
			customerEmptyResponse("prior-response", at.Add(time.Second), DirectionOutbound),
		}))
		assertCustomerResponseBusiness(t, db, "prior-response", BusinessCode("config_download"), "配置查询")
	})

	t.Run("same timestamp in previous batch", func(t *testing.T) {
		db := newRelationalStoreTestDB(t)
		store, err := NewRelationalStore(db)
		require.NoError(t, err)
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{customerConfigRequest("same-time-prior-request", at)}))
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{
			customerEmptyResponse("same-time-prior-response", at, DirectionOutbound),
		}))
		assertCustomerResponseBusiness(t, db, "same-time-prior-response", BusinessCode("config_download"), "配置查询")
	})
}

func TestRelationalStoreLeavesAmbiguousOrMismatchedEmptyResponsesUnknown(t *testing.T) {
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		requests []StoredEvent
		response StoredEvent
	}{
		{
			name:     "conflicting requests",
			requests: []StoredEvent{customerConfigRequest("config-request-a", at), customerKeepaliveRequest("keepalive-request-b", at.Add(time.Millisecond))},
			response: customerEmptyResponse("conflicting-response", at.Add(time.Second), DirectionOutbound),
		},
		{
			name:     "different endpoint",
			requests: []StoredEvent{customerConfigRequest("different-endpoint-request", at)},
			response: func() StoredEvent {
				response := customerEmptyResponse("different-endpoint-response", at.Add(time.Second), DirectionOutbound)
				response.RemoteAddr = "device-b:15060"
				return response
			}(),
		},
		{
			name:     "different device",
			requests: []StoredEvent{customerConfigRequest("different-device-request", at)},
			response: func() StoredEvent {
				response := customerEmptyResponse("different-device-response", at.Add(time.Second), DirectionOutbound)
				response.DeviceID = "device-b"
				return response
			}(),
		},
		{
			name:     "wrong direction",
			requests: []StoredEvent{customerConfigRequest("wrong-direction-request", at)},
			response: customerEmptyResponse("wrong-direction-response", at.Add(time.Second), DirectionInbound),
		},
		{
			name:     "outside time window",
			requests: []StoredEvent{customerConfigRequest("late-request", at)},
			response: customerEmptyResponse("late-response", at.Add(3*time.Minute), DirectionOutbound),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newRelationalStoreTestDB(t)
			store, err := NewRelationalStore(db)
			require.NoError(t, err)
			require.NoError(t, store.InsertBatch(context.Background(), tt.requests))
			require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{tt.response}))
			assertCustomerResponseBusiness(t, db, tt.response.EventID, BusinessUnknown, "未知业务")
		})
	}
}

func TestRelationalStoreFiltersMessagesAndSessionsByBusinessCode(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	legacyRegister := testRelationalEvent("legacy-register", at, "device-a", "call-register", "REGISTER", 0)
	legacyRegister.BusinessCode, legacyRegister.BusinessType = BusinessUnknown, "未知业务"
	legacyRegister.Direction = DirectionInbound
	legacyRegister.LocalAddr, legacyRegister.RemoteAddr = "platform:5060", "device:15060"
	configSession := testRelationalEvent("config-session", at.Add(time.Second), "device-a", "call-config", "MESSAGE", 0)
	configSession.BusinessCode, configSession.BusinessType, configSession.BusinessConfidence = BusinessCode("config_download"), "配置查询", "high"
	keepaliveSession := testRelationalEvent("keepalive-session", at.Add(2*time.Second), "device-a", "call-keepalive", "MESSAGE", 0)
	keepaliveSession.BusinessCode, keepaliveSession.BusinessType, keepaliveSession.BusinessConfidence = BusinessKeepalive, "心跳保持", "high"
	require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{legacyRegister, configSession, keepaliveSession}))

	page, err := store.ListMessages(t.Context(), MessageFilter{
		From: at.Add(-time.Minute), To: at.Add(time.Minute), BusinessCode: BusinessRegister, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "legacy-register", page.Items[0].EventID)

	sessions, err := store.ListSessions(t.Context(), SessionFilter{
		From: at.Add(-time.Minute), To: at.Add(time.Minute), BusinessCode: BusinessCode("config_download"), Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "call-config", sessions[0].CallID)

	stats, err := store.GetSessionStats(t.Context(), SessionFilter{
		From: at.Add(-time.Minute), To: at.Add(time.Minute), BusinessCode: BusinessCode("config_download"),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Total)
}

func TestRelationalStoreListsEmptyMethodTransportEventsByBusinessCode(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	events := []StoredEvent{
		{
			EventID: "transport-keepalive", OccurredAt: at, Direction: DirectionInbound, Transport: "TCP",
			LocalAddr: "platform:5060", RemoteAddr: "device:15060", DeviceID: "device-a",
			BusinessCode: BusinessCode("transport_keepalive"), BusinessType: "传输保活", BusinessConfidence: "high",
			Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte("transport"), Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("f", 64)},
		},
		{
			EventID: "stun-binding", OccurredAt: at.Add(time.Second), Direction: DirectionInbound, Transport: "UDP",
			LocalAddr: "platform:5060", RemoteAddr: "device:15060", DeviceID: "device-a",
			BusinessCode: BusinessCode("stun_binding"), BusinessType: "STUN探测", BusinessConfidence: "high",
			Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte("stun"), Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("a", 64)},
		},
	}
	require.NoError(t, store.InsertBatch(t.Context(), events))
	for _, tt := range []struct {
		code  BusinessCode
		id    string
		label string
	}{
		{BusinessCode("transport_keepalive"), "transport-keepalive", "传输保活"},
		{BusinessCode("stun_binding"), "stun-binding", "STUN探测"},
	} {
		page, err := store.ListMessages(t.Context(), MessageFilter{
			From: at.Add(-time.Minute), To: at.Add(time.Minute), BusinessCode: tt.code, Limit: 10,
		})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.Equal(t, tt.id, page.Items[0].EventID)
		require.Empty(t, page.Items[0].Method)
		require.Equal(t, tt.code, page.Items[0].BusinessCode)
		require.Equal(t, tt.label, page.Items[0].BusinessType)
	}
}

func TestRelationalStorePagesPastNonmatchingBusinessSessions(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	events := make([]StoredEvent, 202)
	for i := 0; i < 201; i++ {
		event := testRelationalEvent(fmt.Sprintf("newer-keepalive-%03d", i), at.Add(time.Duration(i+1)*time.Second), "device-a", fmt.Sprintf("newer-call-%03d", i), "MESSAGE", 0)
		event.BusinessCode, event.BusinessType, event.BusinessConfidence = BusinessKeepalive, "心跳保持", "high"
		events[i] = event
	}
	older := testRelationalEvent("older-config", at, "device-a", "older-call", "MESSAGE", 0)
	older.BusinessCode, older.BusinessType, older.BusinessConfidence = BusinessCode("config_download"), "配置查询", "high"
	events[201] = older
	require.NoError(t, store.InsertBatch(t.Context(), events))

	sessions, err := store.ListSessions(t.Context(), SessionFilter{
		From: at.Add(-time.Minute), To: at.Add(5 * time.Minute), BusinessCode: BusinessCode("config_download"), Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "older-call", sessions[0].CallID)
}

func TestRelationalStoreInsertBatchKeepsTransactionLookupBoundedToBatchKeys(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	requests := make([]StoredEvent, 80)
	responses := make([]StoredEvent, 80)
	for i := range requests {
		requests[i] = customerConfigRequest(fmt.Sprintf("request-%03d", i), at.Add(time.Duration(i)*time.Millisecond))
		requests[i].CallID = fmt.Sprintf("call-%03d", i)
		responses[i] = customerEmptyResponse(fmt.Sprintf("response-%03d", i), at.Add(time.Second), DirectionOutbound)
		responses[i].CallID = requests[i].CallID
	}
	require.NoError(t, store.InsertBatch(t.Context(), requests))
	require.NoError(t, store.InsertBatch(t.Context(), responses))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbSipTraceMessage{}).Where("business_code = ?", "config_download").Count(&count).Error)
	require.EqualValues(t, 160, count)
}

func validSTUNBindingRequest() []byte {
	return []byte{0, 1, 0, 0, 0x21, 0x12, 0xa4, 0x42, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
}

func customerConfigRequest(id string, at time.Time) StoredEvent {
	return StoredEvent{
		EventID: id, OccurredAt: at, Direction: DirectionInbound, Transport: "UDP",
		LocalAddr: "platform:5060", RemoteAddr: "device:15060", DeviceID: "device-a",
		Method: "MESSAGE", CallID: "customer-config-call", CSeq: 9, CSeqMethod: "MESSAGE",
		BusinessCode: BusinessCode("config_download"), BusinessType: "配置查询", BusinessConfidence: "high",
		Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte(id), Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("d", 64)},
	}
}

func customerKeepaliveRequest(id string, at time.Time) StoredEvent {
	event := customerConfigRequest(id, at)
	event.BusinessCode, event.BusinessType, event.BusinessConfidence = BusinessKeepalive, "心跳保持", "high"
	return event
}

func customerEmptyResponse(id string, at time.Time, direction Direction) StoredEvent {
	return StoredEvent{
		EventID: id, OccurredAt: at, Direction: direction, Transport: "UDP",
		LocalAddr: "platform:5060", RemoteAddr: "device:15060", DeviceID: "device-a",
		Method: "MESSAGE", StatusCode: 200, CallID: "customer-config-call", CSeq: 9, CSeqMethod: "MESSAGE",
		BusinessCode: BusinessUnknown, BusinessType: "未知业务", BusinessConfidence: "none", ResponseBodyEmpty: true,
		Payload: EncryptedPayload{Nonce: []byte{1}, Ciphertext: []byte(id), Algorithm: EncryptionAES256GCM, KeyVersion: "v1", DigestSHA256: strings.Repeat("e", 64)},
	}
}

func assertCustomerResponseBusiness(t *testing.T, db *gorm.DB, eventID string, code BusinessCode, label string) {
	t.Helper()
	var row gbmodels.GbSipTraceMessage
	require.NoError(t, db.First(&row, "event_id = ?", eventID).Error)
	require.Equal(t, string(code), row.BusinessCode)
	require.Equal(t, label, row.BusinessType)
}

func TestTransportEventsWithoutCallIDRemainSeparateAndFilterable(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	events := []StoredEvent{testRelationalEvent("transport-event-a", at, "device-a", "", "", 0), testRelationalEvent("transport-event-b", at.Add(time.Second), "device-a", "", "", 0)}
	events[0].BusinessCode, events[0].BusinessType = BusinessTransportKeepalive, "传输保活"
	events[1].BusinessCode, events[1].BusinessType = BusinessSTUNBinding, "STUN探测"
	require.NoError(t, store.InsertBatch(t.Context(), events))
	sessions, err := store.ListSessions(t.Context(), SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), Limit: 10})
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	filtered, err := store.ListSessions(t.Context(), SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute), BusinessCode: BusinessTransportKeepalive, Limit: 1})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.EqualValues(t, 1, filtered[0].MessageCount)
}

func TestSessionStatsHandlesThousandMixedCandidates(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	at := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	events := make([]StoredEvent, 1100)
	for i := range events {
		callID := fmt.Sprintf("call-%04d", i)
		method := "MESSAGE"
		if i%2 == 0 {
			callID, method = "", ""
		}
		events[i] = testRelationalEvent(fmt.Sprintf("event-%04d", i), at.Add(time.Duration(i)*time.Millisecond), "device-a", callID, method, 0)
	}
	for start := 0; start < len(events); start += 100 {
		require.NoError(t, store.InsertBatch(t.Context(), events[start:start+100]))
	}
	stats, err := store.GetSessionStats(t.Context(), SessionFilter{From: at.Add(-time.Minute), To: at.Add(time.Minute)})
	require.NoError(t, err)
	require.EqualValues(t, 1000, stats.Total)
	require.True(t, stats.Truncated)
}

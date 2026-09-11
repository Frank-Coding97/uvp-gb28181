package trace

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The fixture is shared by contract tests so the API's UTC and cursor shape
// is exercised without duplicating JSON literals in each test.
func traceContractFixture() (MessageSummary, MessageDetail, SessionSummary, HealthSnapshot) {
	at := time.Date(2026, 7, 20, 10, 11, 12, 123000000, time.UTC)
	summary := MessageSummary{
		EventID: "d2719f86-0d47-4e7e-9c02-5e7d191e5a1c", OccurredAt: at,
		Direction: DirectionInbound, Transport: "TCP", DeviceID: "34020000001320000001",
		Method: "MESSAGE", StatusCode: 0, CallID: "call-42", CSeq: 1, CSeqMethod: "MESSAGE",
	}
	detail := MessageDetail{MessageSummary: summary, Payload: "MESSAGE sip:test SIP/2.0", Sensitive: false}
	session := SessionSummary{
		Day: at.Truncate(24 * time.Hour), DeviceID: summary.DeviceID, CallID: summary.CallID,
		FirstAt: at, LastAt: at, MessageCount: 1, InboundCount: 1,
		Methods: []string{"MESSAGE"}, SessionDerivedState: SessionDerivedState{OriginalAvailable: true, OriginalExpiresAt: at.Add(RawMessageRetention)},
	}
	health := HealthSnapshot{State: HealthReady, QueueDepth: 2, QueueCapacity: 128}
	return summary, detail, session, health
}

func TestTraceContractFixturesUseRFC3339AndStableCursor(t *testing.T) {
	summary, detail, session, health := traceContractFixture()
	encoded := EncodeMessageCursor(MessageCursor{OccurredAt: summary.OccurredAt, EventID: summary.EventID})
	decoded, err := DecodeMessageCursor(encoded)
	require.NoError(t, err)
	require.Equal(t, summary.OccurredAt, decoded.OccurredAt)
	require.Equal(t, summary.EventID, decoded.EventID)

	data, err := json.Marshal(struct {
		Summary MessageSummary `json:"summary"`
		Detail  MessageDetail  `json:"detail"`
		Session SessionSummary `json:"session"`
		Health  HealthSnapshot `json:"health"`
	}{summary, detail, session, health})
	require.NoError(t, err)
	require.Contains(t, string(data), "2026-07-20T10:11:12.123Z")
	require.NotContains(t, string(data), "offset")
}

type fixtureQueryRepository struct{}

func (fixtureQueryRepository) ListMessages(context.Context, MessageFilter) (MessagePage, error) {
	summary, _, _, _ := traceContractFixture()
	return MessagePage{Items: []MessageSummary{summary}}, nil
}
func (fixtureQueryRepository) GetMessage(context.Context, string) (StoredMessage, error) {
	return StoredMessage{}, ErrTraceMessageNotFound
}
func (fixtureQueryRepository) ListSessions(context.Context, SessionFilter) ([]SessionSummary, error) {
	_, _, session, _ := traceContractFixture()
	return []SessionSummary{session}, nil
}

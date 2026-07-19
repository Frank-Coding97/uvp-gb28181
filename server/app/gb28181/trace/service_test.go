package trace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type queryRepositoryStub struct{ message StoredMessage }

func (r queryRepositoryStub) ListMessages(context.Context, MessageFilter) (MessagePage, error) {
	return MessagePage{Items: []MessageSummary{}}, nil
}
func (r queryRepositoryStub) GetMessage(context.Context, string) (StoredMessage, error) {
	return r.message, nil
}
func (r queryRepositoryStub) ListSessions(context.Context, SessionFilter) ([]SessionSummary, error) {
	return []SessionSummary{}, nil
}

func TestQueryServiceDecryptsAndRedactsMessageByDefault(t *testing.T) {
	cipher, err := NewCipher([]byte("0123456789abcdef0123456789abcdef"), "v1")
	require.NoError(t, err)
	raw := []byte("REGISTER sip:test SIP/2.0\r\nAuthorization: Digest username=34020000001320000001,response=secret\r\n\r\n")
	payload, err := cipher.Encrypt(raw)
	require.NoError(t, err)
	service := NewQueryService(
		func() HealthSnapshot { return HealthSnapshot{State: HealthReady} },
		queryRepositoryStub{message: StoredMessage{MessageSummary: MessageSummary{EventID: "event-1"}, Payload: payload}},
		cipher,
	)

	detail, err := service.GetMessage(context.Background(), "event-1", false, DisclosureContext{})
	require.NoError(t, err)
	require.Contains(t, detail.Payload, "Authorization: [REDACTED]")
	require.NotContains(t, detail.Payload, "response=secret")
	require.False(t, detail.Sensitive)

	detail, err = service.GetMessage(context.Background(), "event-1", true, DisclosureContext{
		IsAdmin: true, UserID: "7", Purpose: "incident-42",
	})
	require.NoError(t, err)
	require.Equal(t, string(raw), detail.Payload)
	require.True(t, detail.Sensitive)
}

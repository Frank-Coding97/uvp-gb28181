package playauth

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientFeedbackTokenBindsUserLifecycleAndAllowedEvents(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	signer, err := NewSigner(bytes.Repeat([]byte{0x42}, 32), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	grant, err := signer.IssueClientFeedback(ClientFeedbackBinding{
		UserID: 7, DeviceID: "D1", ChannelID: "C1", LifecycleID: "life-1",
		AllowedEvents: []string{"first_frame", "player_error"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, grant.Token)
	require.Equal(t, now.Add(DefaultClientFeedbackTTL), grant.ExpiresAt)

	claims, err := signer.VerifyClientFeedback(grant.Token, 7, "life-1", "first_frame")
	require.NoError(t, err)
	require.Equal(t, "D1", claims.DeviceID)
	require.Equal(t, "C1", claims.ChannelID)
	require.ErrorIs(t, mustVerifyFeedback(signer, grant.Token, 8, "life-1", "first_frame"), ErrClientFeedbackUnavailable)
	require.ErrorIs(t, mustVerifyFeedback(signer, grant.Token, 7, "life-2", "first_frame"), ErrClientFeedbackUnavailable)
	require.ErrorIs(t, mustVerifyFeedback(signer, grant.Token, 7, "life-1", "unexpected"), ErrClientFeedbackUnavailable)
}

func TestClientFeedbackTokenRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	signer, err := NewSigner(bytes.Repeat([]byte{0x24}, 32), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	grant, err := signer.IssueClientFeedback(ClientFeedbackBinding{
		UserID: 7, DeviceID: "D1", ChannelID: "C1", LifecycleID: "life-1",
		AllowedEvents: []string{"first_frame"},
	})
	require.NoError(t, err)
	require.ErrorIs(t, mustVerifyFeedback(signer, grant.Token+"x", 7, "life-1", "first_frame"), ErrClientFeedbackUnavailable)

	now = grant.ExpiresAt
	require.ErrorIs(t, mustVerifyFeedback(signer, grant.Token, 7, "life-1", "first_frame"), ErrClientFeedbackUnavailable)
}

func mustVerifyFeedback(signer *Signer, token string, userID uint, lifecycleID, event string) error {
	_, err := signer.VerifyClientFeedback(token, userID, lifecycleID, event)
	return err
}

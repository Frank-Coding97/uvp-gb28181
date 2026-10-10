package recording

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCapabilitySignerTTLAndVerification(t *testing.T) {
	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("k", 32)), "recording-v1")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }

	tenMinutes := 10 * 60.0
	play, err := signer.Issue("41", 7, CapabilityModePlay, &tenMinutes)
	require.NoError(t, err)
	require.Equal(t, now.Add(40*time.Minute), play.ExpiresAt)
	claims, err := signer.Verify(play.Token, "41", CapabilityModePlay)
	require.NoError(t, err)
	require.Equal(t, uint(7), claims.UserID)
	require.Equal(t, "41", claims.FileID)
	require.NotEmpty(t, claims.JTI)

	unknown, err := signer.Issue("41", 7, CapabilityModePlay, nil)
	require.NoError(t, err)
	require.Equal(t, now.Add(90*time.Minute), unknown.ExpiresAt)
	long := 3 * 60 * 60.0
	capped, err := signer.Issue("41", 7, CapabilityModePlay, &long)
	require.NoError(t, err)
	require.Equal(t, now.Add(2*time.Hour), capped.ExpiresAt)
	download, err := signer.Issue("41", 7, CapabilityModeDownload, nil)
	require.NoError(t, err)
	require.Equal(t, now.Add(15*time.Minute), download.ExpiresAt)
}

func TestDeriveCapabilityKeyIsPurposeSpecificAndDeterministic(t *testing.T) {
	root := []byte(strings.Repeat("r", 32))
	first, err := DeriveCapabilityKey(root)
	require.NoError(t, err)
	require.Len(t, first, 32)

	second, err := DeriveCapabilityKey(root)
	require.NoError(t, err)
	require.Equal(t, first, second)

	other, err := DeriveCapabilityKey([]byte(strings.Repeat("s", 32)))
	require.NoError(t, err)
	require.NotEqual(t, first, other)
}

func TestDeriveCapabilityKeyRejectsEmptyRoot(t *testing.T) {
	_, err := DeriveCapabilityKey(nil)
	require.ErrorIs(t, err, ErrCapabilityKey)
}

func TestCapabilitySignerRejectsTamperingExpiryAndWeakKeys(t *testing.T) {
	_, err := NewCapabilitySigner([]byte("short"), "recording-v1")
	require.ErrorIs(t, err, ErrCapabilityKey)

	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	signer, err := NewCapabilitySigner([]byte(strings.Repeat("s", 32)), "recording-v1")
	require.NoError(t, err)
	signer.now = func() time.Time { return now }
	grant, err := signer.Issue("55", 9, CapabilityModeDownload, nil)
	require.NoError(t, err)

	parts := strings.Split(grant.Token, ".")
	require.Len(t, parts, 2)
	tampered := parts[0][:len(parts[0])-1] + "A." + parts[1]
	_, err = signer.Verify(tampered, "55", CapabilityModeDownload)
	require.ErrorIs(t, err, ErrCapabilityInvalid)
	_, err = signer.Verify(grant.Token, "56", CapabilityModeDownload)
	require.ErrorIs(t, err, ErrCapabilityInvalid)
	_, err = signer.Verify(grant.Token, "55", CapabilityModePlay)
	require.ErrorIs(t, err, ErrCapabilityInvalid)

	signer.now = func() time.Time { return grant.ExpiresAt }
	_, err = signer.Verify(grant.Token, "55", CapabilityModeDownload)
	require.ErrorIs(t, err, ErrCapabilityExpired)
	require.NotContains(t, err.Error(), grant.Token)
}

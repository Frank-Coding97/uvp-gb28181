package security

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNonceSourceDomainSeparationAndLegacyLength(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("source-secret"), time.Minute, clock)

	sourceNonce, err := m.IssueForSource("198.51.100.10")
	require.NoError(t, err)
	require.Len(t, sourceNonce, 54)
	require.NoError(t, m.VerifySource(sourceNonce, "198.51.100.10"))
	require.ErrorIs(t, m.Validate(sourceNonce, "00000001"), ErrNonceInvalid)
	require.ErrorIs(t, m.ValidateForSourceTransaction(sourceNonce, "00000001", "tx", "198.51.100.11"), ErrNonceInvalid)

	legacyNonce, err := m.Issue()
	require.NoError(t, err)
	require.ErrorIs(t, m.VerifySource(legacyNonce, "198.51.100.10"), ErrNonceInvalid)
	require.ErrorIs(t, m.ValidateForSourceTransaction(legacyNonce, "00000001", "tx", "198.51.100.10"), ErrNonceInvalid)
}

func TestNonceSourceNormalizesIPv6AndRejectsCrossIP(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("source-secret"), time.Minute, clock)

	nonce, err := m.IssueForSource("2001:0DB8:0000:0000:0000:0000:0000:0001")
	require.NoError(t, err)
	require.NoError(t, m.VerifySource(nonce, "2001:db8::1"))
	require.NoError(t, m.VerifySource(nonce, "2001:0db8:0:0:0:0:0:1"))
	require.ErrorIs(t, m.VerifySource(nonce, "2001:db8::2"), ErrNonceInvalid)
	require.ErrorIs(t, m.ValidateForSourceTransaction(nonce, "00000001", "tx", "2001:db8::2"), ErrNonceInvalid)
}

func TestNonceSourceRejectsTamperingAndNonCanonicalEncoding(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("source-secret"), time.Minute, clock)
	nonce, err := m.IssueForSource("198.51.100.10")
	require.NoError(t, err)

	tampered := []byte(nonce)
	if tampered[len(tampered)/2] == 'A' {
		tampered[len(tampered)/2] = 'B'
	} else {
		tampered[len(tampered)/2] = 'A'
	}
	require.ErrorIs(t, m.VerifySource(string(tampered), "198.51.100.10"), ErrNonceInvalid)

	alternate := alternateNonceEncoding(t, nonce)
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	require.NoError(t, err)
	alternateRaw, err := base64.RawURLEncoding.DecodeString(alternate)
	require.NoError(t, err)
	require.Equal(t, raw, alternateRaw)
	require.ErrorIs(t, m.VerifySource(alternate, "198.51.100.10"), ErrNonceInvalid)
}

func TestNonceSourceVerifyDoesNotConsumeOrRenew(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("source-secret"), time.Minute, clock)
	nonce, err := m.IssueForSource("198.51.100.10")
	require.NoError(t, err)

	clock.now = clock.now.Add(30 * time.Second)
	require.NoError(t, m.VerifySource(nonce, "198.51.100.10"))
	require.Empty(t, m.used)
	require.NoError(t, m.ValidateForSourceTransaction(nonce, "00000001", "tx-a", "198.51.100.10"))
	require.NoError(t, m.VerifySource(nonce, "198.51.100.10"))
	require.NoError(t, m.ValidateForSourceTransaction(nonce, "00000001", "tx-a", "198.51.100.10"))
	require.ErrorIs(t, m.ValidateForSourceTransaction(nonce, "00000001", "tx-b", "198.51.100.10"), ErrNonceReplay)

	clock.now = time.Unix(160, 0)
	require.ErrorIs(t, m.VerifySource(nonce, "198.51.100.10"), ErrNonceExpired)
}

func TestNonceSourceSetTTLRotatesSecretAndAppliesNewTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	m := NewNonceManager([]byte("source-secret"), time.Minute, clock)
	oldNonce, err := m.IssueForSource("198.51.100.10")
	require.NoError(t, err)

	m.SetTTL(5 * time.Minute)
	require.ErrorIs(t, m.VerifySource(oldNonce, "198.51.100.10"), ErrNonceInvalid)

	newNonce, err := m.IssueForSource("198.51.100.10")
	require.NoError(t, err)
	clock.now = time.Unix(100, 0).Add(5*time.Minute - time.Nanosecond)
	require.NoError(t, m.VerifySource(newNonce, "198.51.100.10"))
	clock.now = time.Unix(100, 0).Add(5 * time.Minute)
	require.ErrorIs(t, m.VerifySource(newNonce, "198.51.100.10"), ErrNonceExpired)
}

func alternateNonceEncoding(t *testing.T, nonce string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, nonce[len(nonce)-1])
	require.NotEqual(t, -1, index)
	return nonce[:len(nonce)-1] + string(alphabet[index^1])
}

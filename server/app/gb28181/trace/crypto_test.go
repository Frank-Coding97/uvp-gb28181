package trace

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCipherRoundTripPreservesExactSIPBytes(t *testing.T) {
	cipher, err := NewCipher([]byte(strings.Repeat("k", 32)), "test-v1")
	require.NoError(t, err)
	raw := []byte("MESSAGE sip:test SIP/2.0\r\nAuthorization: Digest secret\r\nContent-Length: 4\r\n\r\nbody")

	payload, err := cipher.Encrypt(raw)
	require.NoError(t, err)
	require.Equal(t, EncryptionAES256GCM, payload.Algorithm)
	require.Equal(t, "test-v1", payload.KeyVersion)
	require.NotEmpty(t, payload.Nonce)
	require.NotEqual(t, raw, payload.Ciphertext)
	require.False(t, bytes.Contains(payload.Ciphertext, []byte("Digest secret")))
	require.Len(t, payload.DigestSHA256, 64)

	decrypted, err := cipher.Decrypt(payload)
	require.NoError(t, err)
	require.Equal(t, raw, decrypted)
}

func TestWriterStoreBoundaryContainsOnlyEncryptedPayload(t *testing.T) {
	store := &recordingStore{}
	cipher := testPayloadCipher()
	module := NewModule(testTraceConfig(4, 1, 5), store, cipher)
	raw := []byte("Authorization: never-store-this-plaintext")
	module.WriteObserver(testWriteProps(), raw)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 10*time.Millisecond)
	stored := store.snapshot()[0]
	require.False(t, bytes.Contains(stored.Payload.Ciphertext, []byte("never-store-this-plaintext")))
	decrypted, err := cipher.Decrypt(stored.Payload)
	require.NoError(t, err)
	require.Equal(t, raw, decrypted)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestEncryptionFailureDropsEventWithoutWritingPlaintext(t *testing.T) {
	store := &recordingStore{}
	module := NewModule(testTraceConfig(4, 1, 5), store, failingCipher{err: errors.New("cipher unavailable")})
	raw := []byte("Authorization: do-not-leak")
	module.WriteObserver(testWriteProps(), raw)

	require.Eventually(t, func() bool { return module.Health().Dropped == 1 }, time.Second, 10*time.Millisecond)
	health := module.Health()
	require.Equal(t, HealthDegraded, health.State)
	require.NotContains(t, health.LastError, "do-not-leak")
	require.Empty(t, store.snapshot())
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestCipherRejectsInvalidKeyAndTamperedPayloadWithoutPlaintextError(t *testing.T) {
	_, err := NewCipher([]byte("short"), "v1")
	require.ErrorIs(t, err, ErrInvalidEncryptionKey)

	cipher, err := NewCipher([]byte(strings.Repeat("k", 32)), "v1")
	require.NoError(t, err)
	raw := []byte("Authorization: super-secret")
	payload, err := cipher.Encrypt(raw)
	require.NoError(t, err)
	payload.Ciphertext[0] ^= 0xff
	_, err = cipher.Decrypt(payload)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "super-secret")
}

func TestLoadCipherFromEnvironment(t *testing.T) {
	t.Setenv("UVP_TRACE_TEST_KEY", strings.Repeat("e", 32))
	cipher, err := LoadCipherFromEnv("UVP_TRACE_TEST_KEY", "env-v1")
	require.NoError(t, err)
	require.NotNil(t, cipher)

	_, err = LoadCipherFromEnv("UVP_TRACE_MISSING_KEY", "env-v1")
	require.ErrorIs(t, err, ErrInvalidEncryptionKey)
}

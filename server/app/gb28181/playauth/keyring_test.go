package playauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func versionedBinding() Binding {
	return Binding{
		DeviceID:        "37010301021320000014",
		ChannelID:       "37010301021320000001",
		App:             "rtp",
		Stream:          "37010301021320000014_37010301021320000001",
		MediaServerID:   "node-a",
		MediaGeneration: 42,
	}
}

func TestPrepareBindUsesActiveKeyAndPreviousOnlyVerifies(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	active := KeyMaterial{Secret: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	previous := KeyMaterial{Secret: []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}
	keyring, err := NewKeyring(active, &previous, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := keyring.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Nonce == "" || prepared.AuthorizationGeneration == "" || !prepared.ExpiresAt.Equal(now.Add(DefaultTTL)) {
		t.Fatalf("prepared=%+v", prepared)
	}
	grant, err := keyring.Bind(prepared, versionedBinding())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := keyring.Verify(grant.Token, versionedBinding())
	if err != nil {
		t.Fatal(err)
	}
	if claims.KeyID != KeyID(active.Secret) || claims.MediaGeneration != 42 || claims.AuthorizationGeneration != prepared.AuthorizationGeneration {
		t.Fatalf("claims=%+v", claims)
	}

	oldSigner, err := NewSigner(previous.Secret, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	oldGrant, err := oldSigner.IssueDirect(versionedBinding())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Verify(oldGrant.Token, versionedBinding()); err != nil {
		t.Fatalf("previous key token rejected: %v", err)
	}
	activeOnly, _ := NewSigner(active.Secret, WithNow(func() time.Time { return now }))
	if _, err := activeOnly.Verify(oldGrant.Token, versionedBinding()); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("removed previous key error=%v", err)
	}
}

func TestKeyringRejectsDuplicateAndUnknownKeyID(t *testing.T) {
	key := KeyMaterial{Secret: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	if _, err := NewKeyring(key, &key); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("duplicate key error=%v", err)
	}
	signer, _ := NewSigner(key.Secret)
	grant, _ := signer.IssueDirect(versionedBinding())
	parts := strings.Split(grant.Token, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var claims map[string]interface{}
	_ = json.Unmarshal(payload, &claims)
	claims["kid"] = "unknown"
	payload, _ = json.Marshal(claims)
	parts[0] = base64.RawURLEncoding.EncodeToString(payload)
	if _, err := signer.Verify(strings.Join(parts, "."), versionedBinding()); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("unknown kid error=%v", err)
	}
}

func TestIPBindingUsesOpaqueDigestAndNormalizesMappedIPv4(t *testing.T) {
	signer, _ := NewSigner([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	binding := versionedBinding()
	binding.BindClientIP = true
	binding.ClientIP = "::ffff:203.0.113.9"
	grant, err := signer.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grant.Token, "203.0.113.9") {
		t.Fatal("token leaked readable client IP")
	}
	equivalent := binding
	equivalent.ClientIP = "203.0.113.9"
	if _, err := signer.Verify(grant.Token, equivalent); err != nil {
		t.Fatalf("normalized IP rejected: %v", err)
	}
	other := binding
	other.ClientIP = "203.0.113.10"
	if _, err := signer.Verify(grant.Token, other); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("different IP error=%v", err)
	}
}

func TestPrepareFailsWhenEntropyUnavailable(t *testing.T) {
	signer, err := NewSigner([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), WithRandomReader(failingReader{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Prepare(); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("prepare error=%v", err)
	}
}

func TestMediaGenerationIsBound(t *testing.T) {
	signer, _ := NewSigner([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	binding := versionedBinding()
	grant, _ := signer.IssueDirect(binding)
	binding.MediaGeneration++
	if _, err := signer.Verify(grant.Token, binding); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("generation mismatch error=%v", err)
	}
}

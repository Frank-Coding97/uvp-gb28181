package playauth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playurl"
)

const testRootSecret = "0123456789abcdef0123456789abcdef"

func TestNewSignerRejectsWeakRootKey(t *testing.T) {
	if _, err := NewSigner([]byte("short-root-key")); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("error=%v, want ErrKeyInvalid", err)
	}
}

func TestDirectTokenRoundTripAndBinding(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "37010301021320000014_37010301021320000001",
		MediaServerID: "node-a",
	}

	grant, err := signer.IssueDirect(binding)
	if err != nil {
		t.Fatalf("issue direct token: %v", err)
	}
	if grant.Token == "" || !grant.ExpiresAt.Equal(now.Add(DefaultTTL)) {
		t.Fatalf("grant=%+v", grant)
	}
	claims, err := signer.Verify(grant.Token, binding)
	if err != nil {
		t.Fatalf("verify direct token: %v", err)
	}
	if claims.Mode != ModeDirect || claims.DeviceID != binding.DeviceID || claims.ChannelID != binding.ChannelID || claims.Nonce == "" {
		t.Fatalf("claims=%+v", claims)
	}

	for name, mutate := range map[string]func(Binding) Binding{
		"device":  func(v Binding) Binding { v.DeviceID = "37010301021320000015"; return v },
		"channel": func(v Binding) Binding { v.ChannelID = "37010301021320000006"; return v },
		"app":     func(v Binding) Binding { v.App = "live"; return v },
		"stream":  func(v Binding) Binding { v.Stream += "x"; return v },
		"node":    func(v Binding) Binding { v.MediaServerID = "node-b"; return v },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := signer.Verify(grant.Token, mutate(binding)); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("verify error=%v, want ErrTokenInvalid", err)
			}
		})
	}
}

func TestDirectTokenRejectsTamperingExpiryAndMalformedValues(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithTTL(time.Minute), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "37010301021320000014_37010301021320000001", MediaServerID: "node-a",
	}
	grant, err := signer.IssueDirect(binding)
	if err != nil {
		t.Fatal(err)
	}

	dot := strings.IndexByte(grant.Token, '.')
	if dot < 0 || dot+1 >= len(grant.Token) {
		t.Fatalf("unexpected token format: %q", grant.Token)
	}
	tamperedByte := byte('A')
	if grant.Token[dot+1] == tamperedByte {
		tamperedByte = 'B'
	}
	tampered := grant.Token[:dot+1] + string(tamperedByte) + grant.Token[dot+2:]
	for _, token := range []string{"", "not-a-token", grant.Token + ".extra", tampered} {
		if _, err := signer.Verify(token, binding); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("token %q error=%v, want ErrTokenInvalid", token, err)
		}
	}

	now = now.Add(time.Minute + time.Second)
	if _, err := signer.Verify(grant.Token, binding); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired error=%v, want ErrTokenExpired", err)
	}
}

func TestSignerSetTTLAppliesToNewTokensOnly(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	first, err := signer.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.SetTTL(5 * time.Minute); err != nil {
		t.Fatal(err)
	}
	second, err := signer.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if !first.ExpiresAt.Equal(now.Add(DefaultTTL)) {
		t.Fatalf("first expiry=%s", first.ExpiresAt)
	}
	if !second.ExpiresAt.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("second expiry=%s", second.ExpiresAt)
	}
	if err := signer.SetTTL(0); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("error=%v, want ErrTokenInvalid", err)
	}
}

func TestIssueDirectRejectsInvalidIdentity(t *testing.T) {
	signer, err := NewSigner([]byte(testRootSecret))
	if err != nil {
		t.Fatal(err)
	}
	valid := Binding{
		DeviceID: "37010301021320000014", ChannelID: "37010301021320000001",
		App: "rtp", Stream: "37010301021320000014_37010301021320000001", MediaServerID: "node-a",
	}
	for name, mutate := range map[string]func(Binding) Binding{
		"device":  func(v Binding) Binding { v.DeviceID = "device"; return v },
		"channel": func(v Binding) Binding { v.ChannelID = ""; return v },
		"app":     func(v Binding) Binding { v.App = ""; return v },
		"stream":  func(v Binding) Binding { v.Stream = ""; return v },
		"node":    func(v Binding) Binding { v.MediaServerID = ""; return v },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := signer.IssueDirect(mutate(valid)); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("issue error=%v, want ErrTokenInvalid", err)
			}
		})
	}
}

func TestCallbackCapabilityIsNodeBoundAndDomainSeparated(t *testing.T) {
	capability, err := CallbackCapability("zlm-api-secret", "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if capability == "" || strings.Contains(capability, "zlm-api-secret") {
		t.Fatalf("capability must be opaque: %q", capability)
	}
	if !VerifyCallbackCapability("zlm-api-secret", "node-a", capability) {
		t.Fatal("matching capability was rejected")
	}
	if VerifyCallbackCapability("zlm-api-secret", "node-b", capability) || VerifyCallbackCapability("other-secret", "node-a", capability) {
		t.Fatal("capability was reusable across node identity or secret")
	}
	if _, err := CallbackCapability("", "node-a"); !errors.Is(err, ErrCapabilityInvalid) {
		t.Fatalf("empty secret error=%v", err)
	}
}

func TestDecorateURLsRejectsMissingOrInvalidPlaybackURL(t *testing.T) {
	if _, err := DecorateURLs(playurl.URLs{}, "token"); !errors.Is(err, ErrURLInvalid) {
		t.Fatalf("empty URLs error=%v, want ErrURLInvalid", err)
	}
	invalid := "/rtp/stream.live.flv"
	if _, err := DecorateURLs(playurl.URLs{HTTPFLV: &invalid}, "token"); !errors.Is(err, ErrURLInvalid) {
		t.Fatalf("relative URL error=%v, want ErrURLInvalid", err)
	}
}

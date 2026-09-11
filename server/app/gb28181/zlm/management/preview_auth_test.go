package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

const previewTestKey = "0123456789abcdef0123456789abcdef"

func previewTestBinding() PreviewBinding {
	return PreviewBinding{
		NodeUUID: "node-a",
		VHost:    "__defaultVhost__",
		Schema:   "rtsp",
		App:      "camera",
		Stream:   "live-1",
	}
}

func TestManagementPreviewSignerBindsIdentityUserAndIP(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewPreviewSigner([]byte(previewTestKey),
		WithPreviewNow(func() time.Time { return now }),
		WithPreviewRandomReader(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32))),
	)
	if err != nil {
		t.Fatal(err)
	}

	binding := previewTestBinding()
	binding.BindClientIP = true
	binding.ClientIP = "::ffff:203.0.113.9"
	grant, err := signer.IssueForUser(17, binding)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Token == "" || grant.Claims.Audience != PreviewAudience || grant.Claims.UserID != 17 {
		t.Fatalf("grant=%+v", grant)
	}
	if grant.Claims.ExpiresAt-grant.Claims.IssuedAt != int64(DefaultPreviewTTL/time.Second) {
		t.Fatalf("unexpected ttl: %+v", grant.Claims)
	}
	if len(grant.Claims.JTI) < 22 || grant.JTIHash == "" || grant.JTIHash == grant.Claims.JTI {
		t.Fatalf("jti must be random and audit-hashed: %+v", grant)
	}
	naked := sha256.Sum256([]byte(grant.Claims.JTI))
	if grant.JTIHash == hex.EncodeToString(naked[:]) {
		t.Fatal("jti audit hash must be domain separated")
	}

	verified, err := signer.Verify(grant.Token, binding)
	if err != nil {
		t.Fatal(err)
	}
	if verified.UserID != 17 || verified.NodeUUID != binding.NodeUUID || verified.VHost != binding.VHost {
		t.Fatalf("verified=%+v", verified)
	}

	equivalentIP := binding
	equivalentIP.ClientIP = "203.0.113.9"
	if _, err := signer.Verify(grant.Token, equivalentIP); err != nil {
		t.Fatalf("mapped IPv4 should verify: %v", err)
	}
}

func TestManagementPreviewSignerRejectsIdentityExpiryKeyAndIPChanges(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	signer, err := NewPreviewSigner([]byte(previewTestKey), WithPreviewNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	binding := previewTestBinding()
	binding.BindClientIP = true
	binding.ClientIP = "203.0.113.9"
	grant, err := signer.IssueForUser(17, binding)
	if err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func(PreviewBinding) PreviewBinding{
		"node":   func(value PreviewBinding) PreviewBinding { value.NodeUUID = "node-b"; return value },
		"vhost":  func(value PreviewBinding) PreviewBinding { value.VHost = "other-vhost"; return value },
		"schema": func(value PreviewBinding) PreviewBinding { value.Schema = "fmp4"; return value },
		"app":    func(value PreviewBinding) PreviewBinding { value.App = "other-app"; return value },
		"stream": func(value PreviewBinding) PreviewBinding { value.Stream = "other-stream"; return value },
		"ip":     func(value PreviewBinding) PreviewBinding { value.ClientIP = "203.0.113.10"; return value },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := signer.Verify(grant.Token, mutate(binding)); !errors.Is(err, ErrPreviewTokenInvalid) {
				t.Fatalf("error=%v, want invalid preview token", err)
			}
		})
	}

	lateSigner, err := NewPreviewSigner([]byte(previewTestKey), WithPreviewNow(func() time.Time { return now.Add(DefaultPreviewTTL + time.Second) }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lateSigner.Verify(grant.Token, binding); !errors.Is(err, ErrPreviewTokenExpired) {
		t.Fatalf("expired error=%v", err)
	}

	otherSigner, err := NewPreviewSigner([]byte("abcdef0123456789abcdef0123456789"), WithPreviewNow(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherSigner.Verify(grant.Token, binding); !errors.Is(err, ErrPreviewTokenInvalid) {
		t.Fatalf("wrong key error=%v", err)
	}

	for _, malformed := range []string{"", "not-a-token", grant.Token + ".extra", strings.Replace(grant.Token, ".", "!", 1)} {
		if _, err := signer.Verify(malformed, binding); !errors.Is(err, ErrPreviewTokenInvalid) {
			t.Fatalf("malformed=%q error=%v", malformed, err)
		}
	}
}

func TestManagementPreviewSignerIssueForUserCannotBeOverriddenByBinding(t *testing.T) {
	signer, err := NewPreviewSigner([]byte(previewTestKey))
	if err != nil {
		t.Fatal(err)
	}
	binding := previewTestBinding()
	binding.UserID = 999
	grant, err := signer.IssueForUser(17, binding)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Claims.UserID != 17 {
		t.Fatalf("issuer user was overridden: %+v", grant.Claims)
	}
}

func TestManagementPreviewSignerRejectsReusedSecretsAndInvalidTTL(t *testing.T) {
	key := []byte(previewTestKey)
	for name, option := range map[string]PreviewSignerOption{
		"jwt":  WithPreviewRejectedSecrets(previewTestKey),
		"zlm":  WithPreviewRejectedSecrets("other", previewTestKey),
		"node": WithPreviewRejectedSecrets("node-a", previewTestKey),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewPreviewSigner(key, option)
			if !errors.Is(err, ErrPreviewKeyInvalid) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), previewTestKey) {
				t.Fatalf("key leaked in error: %v", err)
			}
		})
	}
	if _, err := NewPreviewSignerWithPolicy(key, PreviewKeyPolicy{
		JWTRootSecret: previewTestKey,
	}); !errors.Is(err, ErrPreviewKeyInvalid) {
		t.Fatalf("policy key reuse error=%v", err)
	}
	for _, ttl := range []time.Duration{59 * time.Second, 121 * time.Second} {
		_, err := NewPreviewSigner(key, WithPreviewTTL(ttl))
		if !errors.Is(err, ErrPreviewTTLInvalid) {
			t.Fatalf("ttl=%s error=%v", ttl, err)
		}
	}
	if _, err := NewPreviewSigner([]byte("too-short")); !errors.Is(err, ErrPreviewKeyInvalid) {
		t.Fatalf("short key error=%v", err)
	}
}

func TestManagementPreviewClassifierContractUsesFullMediaIdentity(t *testing.T) {
	classifier := PreviewClassifierFunc(func(_ context.Context, resource PreviewResource) (PreviewResourceClass, error) {
		if resource.NodeUUID != "node-a" || resource.MediaIdentity() != (MediaIdentity{
			Schema: "rtsp", Vhost: "vhost-a", App: "camera", Stream: "stream-a",
		}) {
			t.Fatalf("resource=%+v", resource)
		}
		return PreviewResourceNonGBPreviewable, nil
	})
	class, err := classifier.Classify(context.Background(), PreviewResource{
		NodeUUID: "node-a", VHost: "vhost-a", Schema: "rtsp", App: "camera", Stream: "stream-a",
	})
	if err != nil || class != PreviewResourceNonGBPreviewable {
		t.Fatalf("class=%q err=%v", class, err)
	}
}

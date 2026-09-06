package playauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openAPITokenBinding() OpenAPIBinding {
	return OpenAPIBinding{
		GrantID: "a725da9c-ad51-45e3-9c19-8dcbe2491d92", ClientID: 10,
		ClientEpoch: 2, Scope: "play:live:apply", ScopeEpoch: 3, DeviceEpoch: 4,
		DeviceID: "34020000002000000001", ChannelID: "34020000001320000001",
		NodeUUID: "node-a", BootNonce: "000102030405060708090a0b0c0d0e0f",
		Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "test-stream",
		MediaGeneration: 99, Protocol: "https-flv",
	}
}

func TestOpenAPITokenBindingAndDomain(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }), WithTTL(time.Hour))
	require.NoError(t, err)
	binding := openAPITokenBinding()
	grant, err := signer.IssueOpenAPI(binding)
	require.NoError(t, err)
	require.Equal(t, now.Add(120*time.Second), grant.ExpiresAt, "external TTL does not inherit mutable backend TTL")
	claims, err := signer.VerifyOpenAPI(grant.Token, binding)
	require.NoError(t, err)
	require.Equal(t, binding, claims.OpenAPIBinding)
	require.Equal(t, 3, claims.Version)
	require.Equal(t, "gb28181-openapi-play", claims.Audience)
	backend := Binding{DeviceID: binding.DeviceID, ChannelID: binding.ChannelID, DeviceEpoch: binding.DeviceEpoch, App: binding.App, Stream: binding.Stream, MediaServerID: binding.NodeUUID, MediaGeneration: binding.MediaGeneration}
	_, err = signer.Verify(grant.Token, backend)
	require.Error(t, err)
	old, err := signer.IssueDirect(backend)
	require.NoError(t, err)
	_, err = signer.AuthenticateOpenAPI(old.Token)
	require.Error(t, err, "backend v2 cannot acquire external identity")
	for name, change := range map[string]func(*OpenAPIBinding){
		"grant":        func(b *OpenAPIBinding) { b.GrantID = "bd921956-cf56-4656-a574-15f948257dac" },
		"client":       func(b *OpenAPIBinding) { b.ClientID++ },
		"client epoch": func(b *OpenAPIBinding) { b.ClientEpoch++ },
		"scope":        func(b *OpenAPIBinding) { b.Scope = "device:list" },
		"scope epoch":  func(b *OpenAPIBinding) { b.ScopeEpoch++ },
		"device epoch": func(b *OpenAPIBinding) { b.DeviceEpoch++ },
		"device":       func(b *OpenAPIBinding) { b.DeviceID = "34020000002000000002" },
		"channel":      func(b *OpenAPIBinding) { b.ChannelID = "34020000001320000002" },
		"node":         func(b *OpenAPIBinding) { b.NodeUUID = "node-b" },
		"boot":         func(b *OpenAPIBinding) { b.BootNonce = "100102030405060708090a0b0c0d0e0f" },
		"schema":       func(b *OpenAPIBinding) { b.Schema = "rtsp" },
		"vhost":        func(b *OpenAPIBinding) { b.VHost = "other" },
		"app":          func(b *OpenAPIBinding) { b.App = "other" },
		"stream":       func(b *OpenAPIBinding) { b.Stream = "other" },
		"generation":   func(b *OpenAPIBinding) { b.MediaGeneration++ },
		"protocol":     func(b *OpenAPIBinding) { b.Protocol = "wss-flv" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := binding
			change(&changed)
			_, err := signer.VerifyOpenAPI(grant.Token, changed)
			require.Error(t, err)
		})
	}
	now = now.Add(120 * time.Second)
	_, err = signer.VerifyOpenAPI(grant.Token, binding)
	require.ErrorIs(t, err, ErrTokenExpired)
	_, err = signer.AuthenticateOpenAPI(grant.Token)
	require.NoError(t, err, "signature-only authentication permits persistent bound-viewer TTL policy; it is not authorization")
}

func TestOpenAPITokenStrictEnvelopeAndMalformedBindings(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	grant, err := signer.IssueOpenAPI(openAPITokenBinding())
	require.NoError(t, err)
	parts := strings.Split(grant.Token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	for _, broken := range []string{"", grant.Token + ".extra", strings.Repeat("a", 8193), parts[0] + ".AA", parts[0] + "=." + parts[1]} {
		_, err = signer.AuthenticateOpenAPI(broken)
		require.Error(t, err)
	}
	// Even a correctly MACed internal malformed payload must not be accepted:
	// no duplicate/case-shadowed/unknown fields or ambiguous JSON encodings.
	for _, bad := range []string{
		strings.Replace(string(payload), `"v":3`, `"v":3,"v":3`, 1),
		strings.Replace(string(payload), `"v":3`, `"V":3`, 1),
		strings.Replace(string(payload), `"v":3`, `"v":3,"extra":1`, 1),
		strings.Replace(string(payload), `"aud":"gb28181-openapi-play"`, `"aud":"gb28181-play"`, 1),
	} {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(bad))
		token := encoded + "." + base64.RawURLEncoding.EncodeToString(signature(signer.keys[signer.activeID].openapi, []byte(encoded)))
		_, err = signer.AuthenticateOpenAPI(token)
		require.Error(t, err)
	}
	for _, mutate := range []func(*OpenAPIBinding){
		func(b *OpenAPIBinding) { b.ClientID = 0 }, func(b *OpenAPIBinding) { b.DeviceEpoch = 0 },
		func(b *OpenAPIBinding) { b.BootNonce = strings.Repeat("A", 32) },
		func(b *OpenAPIBinding) { b.GrantID = "not-a-uuid" }, func(b *OpenAPIBinding) { b.Protocol = "hls" },
		func(b *OpenAPIBinding) { b.Stream = "bad\nstream" }, func(b *OpenAPIBinding) { b.MediaGeneration = 0 },
	} {
		binding := openAPITokenBinding()
		mutate(&binding)
		_, err = signer.IssueOpenAPI(binding)
		require.Error(t, err)
	}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	for _, forbidden := range []string{"ak", "sk", "secret", "user", "jwt"} {
		require.NotContains(t, decoded, forbidden)
	}
}

package trace

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactSIPCredentialsPreservesReadableStructure(t *testing.T) {
	raw := []byte("REGISTER sip:test SIP/2.0\r\n" +
		"Authorization: Digest username=\"device\", response=\"secret-response\"\r\n" +
		"Proxy-Authorization: Digest proxy-secret\r\n" +
		"Authentication-Info: nextnonce=\"secret-nonce\"\r\n" +
		"Call-ID: keep-this-call-id\r\n" +
		"Content-Type: Application/MANSCDP+xml\r\n\r\n" +
		"<Notify>\n<Password>xml-secret</Password>\n<DevicePassword code=\"1\">device-secret</DevicePassword>\n</Notify>")

	redacted := RedactSIP(raw)
	require.Contains(t, string(redacted), "REGISTER sip:test SIP/2.0")
	require.Contains(t, string(redacted), "Authorization: [REDACTED]")
	require.Contains(t, string(redacted), "Proxy-Authorization: [REDACTED]")
	require.Contains(t, string(redacted), "Authentication-Info: [REDACTED]")
	require.Contains(t, string(redacted), "Call-ID: keep-this-call-id")
	require.Contains(t, string(redacted), "<Password>[REDACTED]</Password>")
	require.Contains(t, string(redacted), "<DevicePassword code=\"1\">[REDACTED]</DevicePassword>")
	require.NotContains(t, string(redacted), "secret-response")
	require.NotContains(t, string(redacted), "proxy-secret")
	require.NotContains(t, string(redacted), "secret-nonce")
	require.NotContains(t, string(redacted), "xml-secret")
	require.NotContains(t, string(redacted), "device-secret")
}

func TestRenderForDisplayRequiresAdminAndAuditContextForSensitiveBytes(t *testing.T) {
	raw := []byte("Authorization: Digest secret\r\n\r\n")

	defaultView, err := RenderForDisplay(raw, false, DisclosureContext{})
	require.NoError(t, err)
	require.NotContains(t, string(defaultView), "Digest secret")

	_, err = RenderForDisplay(raw, true, DisclosureContext{UserID: "1", Purpose: "incident"})
	require.ErrorIs(t, err, ErrSensitiveAccessDenied)
	_, err = RenderForDisplay(raw, true, DisclosureContext{IsAdmin: true})
	require.ErrorIs(t, err, ErrAuditContextRequired)

	sensitive, err := RenderForDisplay(raw, true, DisclosureContext{IsAdmin: true, UserID: "1", Purpose: "incident"})
	require.NoError(t, err)
	require.Equal(t, raw, sensitive)
	require.False(t, errors.Is(err, ErrSensitiveAccessDenied))
}

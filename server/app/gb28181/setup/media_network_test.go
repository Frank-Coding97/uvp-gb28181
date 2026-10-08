package setup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaNetworkDefaultsPersistAndClear(t *testing.T) {
	svc := NewSIPConfigService(newConfigTestDB(t))
	password := "K9#nT2xQ"
	req := validSaveRequest(&password)
	req.HookIP = "192.168.1.20"
	req.StreamIP = "stream.example.com"
	_, err := svc.Save(context.Background(), req)
	require.NoError(t, err)
	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, req.HookIP, got.HookIP)
	require.Equal(t, req.StreamIP, got.StreamIP)
	req.HookIP, req.StreamIP, req.Password = "", "", nil
	_, err = svc.Save(context.Background(), req)
	require.NoError(t, err)
	got, err = svc.Get(context.Background())
	require.NoError(t, err)
	require.Empty(t, got.HookIP)
	require.Empty(t, got.StreamIP)
}

func TestMediaNetworkDefaultsValidation(t *testing.T) {
	for _, tc := range []struct{ name, hook, stream, invalid string }{
		{"optional", "", "", ""},
		{"ip", "192.168.1.20", "203.0.113.8", ""},
		{"ipv6", "2001:db8::1", "2001:db8::2", ""},
		{"domain", "", "video.example.com", ""},
		{"absolute domain", "", "video.example.com.", ""},
		{"multicast", "224.0.0.1", "", "hookIp"},
		{"numeric invalid", "", "999.1.1.1", "streamIp"},
		{"hook domain", "hook.example.com", "", "hookIp"},
		{"wildcard", "0.0.0.0", "", "hookIp"},
		{"stream wildcard", "", "0.0.0.0", "streamIp"},
		{"url", "", "https://video.example.com", "streamIp"},
		{"port", "", "video.example.com:8080", "streamIp"},
		{"bad domain", "", "bad_host.example.com", "streamIp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validSaveRequest(nil)
			req.HookIP, req.StreamIP = tc.hook, tc.stream
			err := ValidateSIPConfigRequest(req, true)
			if tc.invalid == "" {
				require.NoError(t, err)
				return
			}
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, validation.Fields, tc.invalid)
		})
	}
}

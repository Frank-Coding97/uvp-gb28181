package setup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSIPConfigRequest_ValidLANWildcard(t *testing.T) {
    password := "Sec12345Aa!!"
    request := validSaveRequest(&password)
    request.AdvertiseIP = ""
    err := ValidateSIPConfigRequest(request, false)
    require.NoError(t, err)
}

func TestValidateSIPConfigRequest_EmptyAdvertiseOnlyAllowedForLANWildcard(t *testing.T) {
    password := "Sec12345Aa!!"

    concreteLAN := validSaveRequest(&password)
    concreteLAN.ListenIP = "192.168.1.10"
    concreteLAN.AdvertiseIP = ""
    require.Error(t, ValidateSIPConfigRequest(concreteLAN, false))

    public := validSaveRequest(&password)
    public.DeploymentMode = DeploymentPublic
    public.AdvertiseIP = ""
    require.Error(t, ValidateSIPConfigRequest(public, false))
}

func TestValidateSIPConfigRequest_FieldErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SaveSIPConfigRequest)
		field  string
	}{
		{"invalid mode", func(r *SaveSIPConfigRequest) { r.DeploymentMode = "cloud" }, "deploymentMode"},
		{"invalid listen", func(r *SaveSIPConfigRequest) { r.ListenIP = "http://1.2.3.4" }, "listenIp"},
		{"wildcard advertise", func(r *SaveSIPConfigRequest) { r.AdvertiseIP = wildcardIPv4 }, "advertiseIp"},
		{"loopback advertise", func(r *SaveSIPConfigRequest) { r.AdvertiseIP = "127.0.0.1" }, "advertiseIp"},
		{"ipv6 advertise", func(r *SaveSIPConfigRequest) { r.AdvertiseIP = "2001:db8::1" }, "advertiseIp"},
		{"port zero", func(r *SaveSIPConfigRequest) { r.Port = 0 }, "port"},
		{"port overflow", func(r *SaveSIPConfigRequest) { r.Port = 65536 }, "port"},
		{"short id", func(r *SaveSIPConfigRequest) { r.ServerID = "340200" }, "serverId"},
		{"non digit id", func(r *SaveSIPConfigRequest) { r.ServerID = "3402000000200000000x" }, "serverId"},
		{"short domain", func(r *SaveSIPConfigRequest) { r.Domain = "340200" }, "domain"},
		{"non digit domain", func(r *SaveSIPConfigRequest) { r.Domain = "340200000x" }, "domain"},
		{"short password", func(r *SaveSIPConfigRequest) { p := "12345"; r.Password = &p }, "password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			password := "Sec12345Aa!!"
			req := validSaveRequest(&password)
			tt.mutate(&req)
			err := ValidateSIPConfigRequest(req, false)
			var validationErr *ValidationError
			require.ErrorAs(t, err, &validationErr)
			require.Contains(t, validationErr.Fields, tt.field)
		})
	}
}

func TestValidateSIPConfigRequest_PasswordEditingSemantics(t *testing.T) {
	req := validSaveRequest(nil)
	require.Error(t, ValidateSIPConfigRequest(req, false))
	require.NoError(t, ValidateSIPConfigRequest(req, true))

	empty := ""
	req.Password = &empty
	err := ValidateSIPConfigRequest(req, true)
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Contains(t, validationErr.Fields, "password")
}

func TestPasswordStrength(t *testing.T) {
	weak := []struct {
		name     string
		password string
	}{
		{"too short", "Aa1!Aa1"},
		{"only lower", "abcdefghijkl"},
		{"only digits", "123456789012"},
		{"upper+lower no digit no special", "AbcdefghijklAbc"},
		{"common weak 12345678", "12345678"},
		{"common weak password", "password"},
		{"common weak admin123", "admin123"},
		{"sequential ascending", "0123456789ab"},
	}
	for _, tt := range weak {
		t.Run("reject "+tt.name, func(t *testing.T) {
			require.NotEmpty(t, checkPasswordStrength(tt.password), "expected %q to be rejected", tt.password)
		})
	}

	strong := []string{
		"Sec12345Aa!!",
		"MyP@ssw0rdX1",
		"K9#nT2xQvL5m",
		"S3cur3-Sip-Key!",
	}
	for _, pw := range strong {
		t.Run("accept "+pw, func(t *testing.T) {
			require.Empty(t, checkPasswordStrength(pw), "expected %q to be accepted", pw)
		})
	}
}

func TestValidateSIPConfigRequest_PortBoundaries(t *testing.T) {
	password := "Sec12345Aa!!"
	for _, port := range []int{1, 65535} {
		req := validSaveRequest(&password)
		req.Port = port
		require.NoError(t, ValidateSIPConfigRequest(req, false))
	}
}

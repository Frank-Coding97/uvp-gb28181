package setup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSIPConfigRequest_ValidLANWildcard(t *testing.T) {
	password := "Secret123"
	err := ValidateSIPConfigRequest(validSaveRequest(&password), false)
	require.NoError(t, err)
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
			password := "Secret123"
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

func TestValidateSIPConfigRequest_PortBoundaries(t *testing.T) {
	password := "Secret123"
	for _, port := range []int{1, 65535} {
		req := validSaveRequest(&password)
		req.Port = port
		require.NoError(t, ValidateSIPConfigRequest(req, false))
	}
}

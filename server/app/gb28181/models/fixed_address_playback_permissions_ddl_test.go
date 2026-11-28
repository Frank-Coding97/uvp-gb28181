package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixedAddressPlaybackPermissionMigrations(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	for _, name := range []string{
		"2026-08-10-fixed-address-playback-permissions.sql",
		"2026-08-10-fixed-address-playback-permissions-postgresql.sql",
		"2026-08-10-fixed-address-playback-permissions-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "/api/gb28181/sip/service-config/fixed-address-playback")
		require.Contains(t, text, "gb28181:sip:config:view")
		require.Contains(t, text, "gb28181:sip:config:update")
		require.Contains(t, text, "not exists")
	}
}

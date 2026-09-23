package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultPlaybackProtocolPermissionMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, filename := range []string{
		"2026-08-10-default-playback-protocol-permissions.sql",
		"2026-08-10-default-playback-protocol-permissions-postgresql.sql",
		"2026-08-10-default-playback-protocol-permissions-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/api/gb28181/sip/service-config/default-playback-protocol",
				"/api/sysdictitem/getbydictcode/:dictcode",
				"'get'",
				"'put'",
				"gb28181:sip:config:view",
				"gb28181:sip:config:update",
				"sys_api",
				"sys_menu_api",
				"sys_casbin_rule",
				"role_1",
			} {
				require.Contains(t, sql, token)
			}
		})
	}
}

func TestDefaultPlaybackProtocolDictionaryMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, filename := range []string{
		"2026-08-10-default-playback-protocol-dict.sql",
		"2026-08-10-default-playback-protocol-dict-postgresql.sql",
		"2026-08-10-default-playback-protocol-dict-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"gb28181_playback_protocol",
				"sys_dict",
				"sys_dict_item",
				"ws-flv",
				"http-flv",
				"hls",
				"webrtc",
				"not exists",
			} {
				require.Contains(t, sql, token)
			}
		})
	}
}

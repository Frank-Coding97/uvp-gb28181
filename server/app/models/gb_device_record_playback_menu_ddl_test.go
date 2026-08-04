package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceRecordPlaybackMenuMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	filenames := []string{
		"2026-08-04-device-record-playback-menu.sql",
		"2026-08-04-device-record-playback-menu-postgresql.sql",
		"2026-08-04-device-record-playback-menu-sqlserver.sql",
	}
	for _, filename := range filenames {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/gb28181/device-mgmt",
				"/gb28181/device-mgmt/index",
				"/gb28181/device-record-playback/:channelid",
				"gb28181-device-record-playback",
				"gb28181/device-record-playback/index",
				"sys_role_menu",
				"is_full",
				"hide",
			} {
				require.Contains(t, sql, token)
			}
			require.NotContains(t, sql, "140350", "migration must derive the GB menu parent from the device list route")
		})
	}
}

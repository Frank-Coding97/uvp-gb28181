package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelFavoriteMigrationContracts(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, name := range []string{"2026-08-19-channel-favorites.sql", "2026-08-19-channel-favorites-postgresql.sql", "2026-08-19-channel-favorites-sqlserver.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		for _, token := range []string{"gb_channel_favorite_group", "gb_channel_favorite_item", "owner_user_id", "device_code", "channel_code", "uk_gb_channel_favorite_group_owner_name", "uk_gb_channel_favorite_item_code"} {
			require.Contains(t, text, token, name)
		}
	}
	for _, name := range []string{"2026-08-19-channel-favorites-down.sql", "2026-08-19-channel-favorites-postgresql-down.sql", "2026-08-19-channel-favorites-sqlserver-down.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_channel_favorite_item")
		require.Contains(t, text, "gb_channel_favorite_group")
		require.True(t, strings.Contains(text, "signal") || strings.Contains(text, "raise exception") || strings.Contains(text, "throw"), name)
	}
}

package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceSecurityRequiresCompletedCleanupBeforeNewMedia(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	require.NoError(t, f.db.Exec(`CREATE TABLE gb_device (
		device_id TEXT, access_epoch INTEGER, cleanup_completed_epoch INTEGER,
		legacy_revoked_before DATETIME NULL, deleted_at DATETIME NULL)`).Error)
	cutoff := time.Unix(f.clock.Unix(), 0).UTC()
	require.NoError(t, f.db.Exec(`INSERT INTO gb_device
		(device_id,access_epoch,cleanup_completed_epoch,legacy_revoked_before) VALUES (?,2,1,?)`, transferDevice, cutoff).Error)
	store := NewDeviceSecurityStore(f.db)
	ctx := context.Background()
	require.Error(t, store.AuthorizeEpoch(ctx, transferDevice, 2), "current epoch alone cannot prove cleanup completion")
	require.Error(t, store.AuthorizeLegacy(ctx, transferDevice, cutoff.Unix()), "legacy cutoff equality cannot bypass a pending device barrier")
	require.ErrorIs(t, store.AuthorizeEpoch(ctx, transferDevice, 1), ErrTokenRevoked)
	require.ErrorIs(t, store.AuthorizeLegacy(ctx, transferDevice, cutoff.Unix()-1), ErrTokenRevoked)
	require.Error(t, NewDeviceSecurityStore(f.db).AuthorizeEpoch(ctx, transferDevice, 2), "rebuilding the authority must preserve the pending barrier")
	// This fixture models a trusted cleanup acknowledgement, not real media
	// cleanup. The device coordinator will own the production completion CAS.
	require.NoError(t, f.db.Exec("UPDATE gb_device SET cleanup_completed_epoch=2").Error)
	require.NoError(t, store.AuthorizeEpoch(ctx, transferDevice, 2))
	require.NoError(t, store.AuthorizeLegacy(ctx, transferDevice, cutoff.Unix()))
}

func TestDeviceSecurityRejectsMissingOrInvalidCleanupWatermark(t *testing.T) {
	for _, scenario := range []string{"missing", "null", "zero", "negative", "ahead"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOpenAPIRevocationFixture(t)
			require.NoError(t, f.db.Exec(`CREATE TABLE gb_device (
				device_id TEXT, access_epoch INTEGER, legacy_revoked_before DATETIME NULL, deleted_at DATETIME NULL)`).Error)
			require.NoError(t, f.db.Exec("INSERT INTO gb_device(device_id,access_epoch) VALUES (?,1)", transferDevice).Error)
			if scenario != "missing" {
				require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NULL").Error)
				values := map[string]any{"null": nil, "zero": 0, "negative": -1, "ahead": 2}
				require.NoError(t, f.db.Exec("UPDATE gb_device SET cleanup_completed_epoch=?", values[scenario]).Error)
			}
			store := NewDeviceSecurityStore(f.db)
			require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrDeviceSecurityUnavailable)
			require.ErrorIs(t, store.AuthorizeLegacy(context.Background(), transferDevice, f.clock.Unix()), ErrDeviceSecurityUnavailable)
		})
	}
}

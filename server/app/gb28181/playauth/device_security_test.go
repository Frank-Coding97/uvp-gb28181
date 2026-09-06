package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceSecurityLegacyCutoffCoversSameSecondAndOnlyTargetDevice(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, f)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1").Error)
	other := "34020000001320000002"
	require.NoError(t, f.db.Exec("INSERT INTO gb_device(id, device_id, owner_dept_id, access_epoch) VALUES (2, ?, 20, 1)", other).Error)
	store := NewDeviceSecurityStore(f.db)
	issued := f.clock.Unix()
	require.NoError(t, store.AuthorizeLegacy(context.Background(), transferDevice, issued))
	cutoff := time.Unix(issued+1, 0).UTC()
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, legacy_revoked_before=? WHERE id=1", cutoff).Error)
	require.ErrorIs(t, store.AuthorizeLegacy(context.Background(), transferDevice, issued), ErrTokenRevoked)
	require.NoError(t, store.AuthorizeLegacy(context.Background(), other, issued))
	// Equality retains the historical iat < cutoff rule; new code will stop
	// issuing legacy tokens. Pending cleanup still blocks current authority.
	require.ErrorIs(t, store.AuthorizeLegacy(context.Background(), transferDevice, issued+1), ErrDeviceSecurityUnavailable)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET cleanup_completed_epoch=2 WHERE id=1").Error)
	require.NoError(t, store.AuthorizeLegacy(context.Background(), transferDevice, issued+1))
	require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrTokenRevoked)
	require.NoError(t, store.AuthorizeEpoch(context.Background(), transferDevice, 2))
	require.NoError(t, store.AuthorizeEpoch(context.Background(), other, 1))
}

func TestDeviceSecurityReloadsAfterTransferAndReturn(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, f)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1").Error)
	store := NewDeviceSecurityStore(f.db)
	require.NoError(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=3 WHERE id=1").Error)
	require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrTokenRevoked)
	require.ErrorIs(t, NewDeviceSecurityStore(f.db).AuthorizeEpoch(context.Background(), transferDevice, 2), ErrTokenRevoked)
	require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 3), ErrDeviceSecurityUnavailable)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET cleanup_completed_epoch=3 WHERE id=1").Error)
	require.NoError(t, store.AuthorizeEpoch(context.Background(), transferDevice, 3))
}

func TestDeviceSecurityFailsClosedOnMissingOrInvalidPersistentState(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
	}{
		{name: "missing column", statement: ""},
		{name: "missing device", statement: "DELETE FROM gb_device"},
		{name: "deleted device", statement: "UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP"},
		{name: "invalid epoch", statement: "UPDATE gb_device SET access_epoch=0"},
		{name: "db failure", statement: "DROP TABLE gb_device"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOpenAPIRevocationFixture(t)
			prepareTransferDevice(t, f)
			if test.name != "missing column" {
				require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
				require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1").Error)
				require.NoError(t, f.db.Exec(test.statement).Error)
			}
			store := NewDeviceSecurityStore(f.db)
			require.ErrorIs(t, store.AuthorizeLegacy(context.Background(), transferDevice, f.clock.Unix()), ErrDeviceSecurityUnavailable)
			require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrDeviceSecurityUnavailable)
		})
	}
}

func TestDeviceSecurityRequiresContextAndValidInputs(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	store := NewDeviceSecurityStore(f.db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []struct {
		device string
		epoch  int64
	}{{"invalid", 1}, {transferDevice, 0}, {transferDevice, -1}} {
		require.Error(t, store.AuthorizeEpoch(context.Background(), input.device, input.epoch))
	}
	require.Error(t, store.AuthorizeLegacy(context.Background(), transferDevice, 0))
	require.Error(t, store.AuthorizeEpoch(ctx, transferDevice, 1))
	require.Error(t, store.AuthorizeEpoch(nil, transferDevice, 1))
	require.Error(t, NewDeviceSecurityStore(nil).AuthorizeEpoch(context.Background(), transferDevice, 1))
	var absent *DeviceSecurityStore
	require.Error(t, absent.AuthorizeEpoch(context.Background(), transferDevice, 1))
}

func TestDeviceSecurityRejectsAmbiguousOrMalformedAuthorityRows(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	require.NoError(t, f.db.Exec(`CREATE TABLE gb_device (device_id TEXT, access_epoch INTEGER NULL, cleanup_completed_epoch INTEGER NOT NULL DEFAULT 1, legacy_revoked_before DATETIME NULL, deleted_at DATETIME NULL)`).Error)
	store := NewDeviceSecurityStore(f.db)
	require.NoError(t, f.db.Exec("INSERT INTO gb_device(device_id) VALUES (?)", transferDevice).Error)
	require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrDeviceSecurityUnavailable)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=1, legacy_revoked_before=?", f.clock).Error)
	require.ErrorIs(t, store.AuthorizeLegacy(context.Background(), transferDevice, f.clock.Unix()), ErrDeviceSecurityUnavailable)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET legacy_revoked_before=NULL").Error)
	require.NoError(t, f.db.Exec("INSERT INTO gb_device(device_id,access_epoch) VALUES (?,1)", transferDevice).Error)
	require.ErrorIs(t, store.AuthorizeEpoch(context.Background(), transferDevice, 1), ErrDeviceSecurityUnavailable)
}

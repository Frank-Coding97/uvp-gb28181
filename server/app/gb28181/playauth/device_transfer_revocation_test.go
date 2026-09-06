package playauth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

const transferDevice = "34020000001320000001"

func prepareTransferDevice(t *testing.T, f *revocationFixture) {
	t.Helper()
	require.NoError(t, f.db.Exec(`CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id TEXT NOT NULL UNIQUE, owner_dept_id INTEGER NOT NULL, access_epoch INTEGER NOT NULL, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, f.db.Exec("INSERT INTO gb_device VALUES (1, ?, 10, 1, NULL)", transferDevice).Error)
}

func TestOpenAPIDeviceTransferRevokesOnlyOldTargetEpochs(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, f)
	otherDevice := "34020000001320000002"
	grants := make([]models.PlayGrant, 7)
	for i := range grants {
		grants[i] = revocationGrant(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), int64(i+1), openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	}
	grants[2].DeviceID = &otherDevice
	grants[3].DeviceEpoch = 2
	grants[4].State, grants[4].Reason = models.GrantStateRevoked, "client.disabled"
	grants[5].State = models.GrantStateFailed
	grants[6].Scope = "device:list"
	require.NoError(t, f.db.Create(&grants).Error)
	for i, grant := range grants {
		viewer := revocationViewer(grant.GrantID, int64(i+1), models.ViewerStateActive, f.clock)
		if i == 4 {
			viewer.State = models.ViewerStateRevokePending
		}
		if i == 5 {
			viewer.State = models.ViewerStateClosed
		}
		require.NoError(t, f.db.Create(&viewer).Error)
	}
	tombstone := requireGrant(t, f.db, grants[4].GrantID)
	retry := requireViewer(t, f.db, grants[4].GrantID)
	// The recorder must use the caller's transaction, never its own DB handle.
	store := NewOpenAPIRevocationStore(nil, func() time.Time { return f.clock })
	err := f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE gb_device SET owner_dept_id=20, access_epoch=2 WHERE id=1").Error; err != nil {
			return err
		}
		return store.RecordDeviceTransfer(context.Background(), tx, transferDevice, 2)
	})
	require.NoError(t, err)
	for _, grant := range grants[:2] {
		row := requireGrant(t, f.db, grant.GrantID)
		require.Equal(t, models.GrantStateRevoked, row.State)
		require.Equal(t, "device.transferred", row.Reason)
		viewer := requireViewer(t, f.db, grant.GrantID)
		require.Equal(t, models.ViewerStateRevokePending, viewer.State)
		require.Equal(t, f.clock, viewer.RetryAt.UTC())
	}
	for _, index := range []int{2, 3, 5, 6} {
		require.Equal(t, grants[index], requireGrant(t, f.db, grants[index].GrantID))
	}
	f.clock = f.clock.Add(time.Second)
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		return store.RecordDeviceTransfer(context.Background(), tx, transferDevice, 2)
	}))
	require.Equal(t, tombstone, requireGrant(t, f.db, grants[4].GrantID))
	require.Equal(t, retry, requireViewer(t, f.db, grants[4].GrantID))
}

func TestOpenAPIDeviceTransferFailureRollsBackOwnerEpochAndEarlierGrant(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, f)
	grant := revocationGrant("00000000-0000-4000-8000-000000000001", 1, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := revocationViewer(grant.GrantID, 1, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&viewer).Error)
	require.NoError(t, f.db.Exec(`CREATE TRIGGER transfer_viewer_failure BEFORE UPDATE ON gb_openapi_viewer BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`).Error)
	err := f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE gb_device SET owner_dept_id=20, access_epoch=2 WHERE id=1").Error; err != nil {
			return err
		}
		return f.store.RecordDeviceTransfer(context.Background(), tx, transferDevice, 2)
	})
	require.ErrorIs(t, err, ErrOpenAPIRevocationUnavailable)
	var device struct{ OwnerDeptID, AccessEpoch int64 }
	require.NoError(t, f.db.Table("gb_device").First(&device).Error)
	require.EqualValues(t, 10, device.OwnerDeptID)
	require.EqualValues(t, 1, device.AccessEpoch)
	require.Equal(t, grant, requireGrant(t, f.db, grant.GrantID))
	require.Equal(t, viewer, requireViewer(t, f.db, grant.GrantID))
}

func TestOpenAPIDeviceTransferRejectsStaleMissingAndInvalidAuthority(t *testing.T) {
	for _, test := range []struct {
		name, device string
		epoch        int64
		prepare      func(*gorm.DB)
	}{
		{name: "stale epoch", device: transferDevice, epoch: 2},
		{name: "missing device", device: "34020000001320000002", epoch: 2},
		{name: "no transfer", device: transferDevice, epoch: 1},
		{name: "malformed device", device: "device", epoch: 2},
		{name: "deleted device", device: transferDevice, epoch: 2, prepare: func(db *gorm.DB) {
			require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2, deleted_at=CURRENT_TIMESTAMP").Error)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOpenAPIRevocationFixture(t)
			prepareTransferDevice(t, f)
			if test.prepare != nil {
				test.prepare(f.db)
			}
			err := f.db.Transaction(func(tx *gorm.DB) error {
				return f.store.RecordDeviceTransfer(context.Background(), tx, test.device, test.epoch)
			})
			require.Error(t, err)
		})
	}
	f := newOpenAPIRevocationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, f.store.RecordDeviceTransfer(ctx, f.db, transferDevice, 2))
	require.Error(t, f.store.RecordDeviceTransfer(nil, f.db, transferDevice, 2))
	require.Error(t, f.store.RecordDeviceTransfer(context.Background(), nil, transferDevice, 2))
	var absent *OpenAPIRevocationStore
	require.Error(t, absent.RecordDeviceTransfer(context.Background(), f.db, transferDevice, 2))
}

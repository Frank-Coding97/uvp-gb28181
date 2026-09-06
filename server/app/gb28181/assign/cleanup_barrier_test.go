package assign

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIAssignmentRequiresValidCleanupWatermark(t *testing.T) {
	for _, scenario := range []string{"missing", "null", "zero", "negative", "ahead"} {
		t.Run(scenario, func(t *testing.T) {
			db := newAssignTestDB(t)
			device := seedAssignedDeviceWithCode(t, db, "34020000002000100021")
			if scenario == "missing" || scenario == "null" {
				require.NoError(t, db.Exec("ALTER TABLE gb_device DROP COLUMN cleanup_completed_epoch").Error)
				if scenario == "null" {
					require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER NULL").Error)
				}
			} else {
				values := map[string]int64{"zero": 0, "negative": -1, "ahead": 2}
				require.NoError(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch=? WHERE id=?", values[scenario], device.ID).Error)
			}
			service := NewService(db, validatorVisibleDept1, WithTransferClock(fixedTransferClock))
			receipt, err := service.AssignOneWithReceipt(context.Background(), device.ID, 2, nil, false)
			require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
			require.Equal(t, TransferReceipt{}, receipt)
			var state struct{ OwnerDeptID, AccessEpoch int64 }
			require.NoError(t, db.Table("gb_device").Select("owner_dept_id,access_epoch").Where("id=?", device.ID).Take(&state).Error)
			require.EqualValues(t, 1, state.OwnerDeptID)
			require.EqualValues(t, 1, state.AccessEpoch)
		})
	}
}

func TestOpenAPIAssignmentKeepsEarlierPendingCleanupAcrossAnotherTransfer(t *testing.T) {
	db := newAssignTestDB(t)
	device := seedAssignedDeviceWithCode(t, db, "34020000002000100021")
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=?", device.ID).Error)
	service := NewService(db, validatorVisibleDept1, WithTransferClock(fixedTransferClock))
	receipt, err := service.AssignOneWithReceipt(context.Background(), device.ID, 2, nil, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, receipt.OldEpoch)
	require.EqualValues(t, 3, receipt.NewEpoch)
	var state struct{ AccessEpoch, CleanupCompletedEpoch int64 }
	require.NoError(t, db.Table("gb_device").Select("access_epoch,cleanup_completed_epoch").Where("id=?", device.ID).Take(&state).Error)
	require.EqualValues(t, 3, state.AccessEpoch)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch, "a new transfer is not evidence that earlier cleanup completed")
}

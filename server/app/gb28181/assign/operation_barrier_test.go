package assign

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestOpenAPIAssignmentRequiresProcessBarrier(t *testing.T) {
	for _, version := range []string{"one", "batch-v2"} {
		t.Run(version, func(t *testing.T) {
			db := newAssignTestDB(t)
			device := seedAssignedDeviceWithCode(t, db, "34020000002000100021")
			service := NewService(db, validatorVisibleDept1)
			if version == "one" {
				receipt, err := service.AssignOneWithReceipt(context.Background(), device.ID, 2, nil, false)
				require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
				require.Equal(t, TransferReceipt{}, receipt)
			} else {
				result, err := service.AssignBatchV2(context.Background(), []AssignmentInput{{DeviceID: device.ID, ExpectedOwnerDeptID: 1}}, 2)
				require.NoError(t, err)
				require.Equal(t, 1, result.Summary.Failed)
				require.Zero(t, result.Summary.Changed)
			}
			var state struct{ OwnerDeptID, AccessEpoch int64 }
			require.NoError(t, db.Table("gb_device").Select("owner_dept_id, access_epoch").Where("id=?", device.ID).Take(&state).Error)
			require.EqualValues(t, 1, state.OwnerDeptID)
			require.EqualValues(t, 1, state.AccessEpoch)
		})
	}
}

func TestOpenAPIAssignmentCancelsOnlyCommittedTargetOperations(t *testing.T) {
	for _, version := range []string{"one", "batch-v2"} {
		for _, rollback := range []bool{false, true} {
			name := version + "/commit"
			if rollback {
				name = version + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				if !authoritytest.InProcess(t) {
					return
				}
				db := newAssignTestDB(t)
				device := seedAssignedDeviceWithCode(t, db, "34020000002000100021")
				other := seedAssignedDeviceWithCode(t, db, "34020000002000100022")
				barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authoritytest.Authority(t, db))
				require.NoError(t, err)
				lease, err := barrier.BeginEpoch(context.Background(), device.DeviceID, 1)
				require.NoError(t, err)
				defer lease.Release()
				otherLease, err := barrier.BeginEpoch(context.Background(), other.DeviceID, 1)
				require.NoError(t, err)
				defer otherLease.Release()
				recorder := &recordingTransferRecorder{}
				if rollback {
					recorder.err = errors.New("isolated fixture rollback")
				}
				service := NewService(db, validatorVisibleDept1, WithDeviceTransferBarrier(barrier), WithTransferRecorder(recorder))
				if version == "one" {
					_, err = service.AssignOneWithReceipt(context.Background(), device.ID, 2, nil, false)
					require.Equal(t, rollback, err != nil)
				} else {
					result, batchErr := service.AssignBatchV2(context.Background(), []AssignmentInput{{DeviceID: device.ID, ExpectedOwnerDeptID: 1}}, 2)
					require.NoError(t, batchErr)
					require.Equal(t, rollback, result.Summary.Failed == 1)
				}
				require.NoError(t, otherLease.Context().Err(), "another device must not be canceled")
				state := readTransferSecurity(t, db, device.ID)
				if rollback {
					require.NoError(t, lease.Context().Err())
					require.EqualValues(t, 1, state.AccessEpoch)
					return
				}
				require.EqualValues(t, 2, state.AccessEpoch)
				require.EqualValues(t, 2, state.OwnerDeptID)
				require.ErrorIs(t, lease.Context().Err(), context.Canceled)
				newLease, err := barrier.BeginEpoch(context.Background(), device.DeviceID, 2)
				require.Error(t, err, "committed assignment has not completed media cleanup")
				require.Nil(t, newLease)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
				defer cancel()
				require.Error(t, barrier.WaitBefore(ctx, device.ID, 2), "canceled operation is not yet released")
				lease.Release()
				require.NoError(t, barrier.WaitBefore(context.Background(), device.ID, 2))
				var completed int64
				require.NoError(t, db.Table("gb_device").Where("id=?", device.ID).Pluck("cleanup_completed_epoch", &completed).Error)
				require.EqualValues(t, 1, completed, "operation drain is not aggregate media cleanup")
			})
		}
	}
}

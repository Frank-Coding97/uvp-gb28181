package assign

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func reserveAssignmentIntent(t *testing.T, db *gorm.DB, pk uint, code string, n int, dispatch bool) string {
	t.Helper()
	id := playauth.DeviceOperationIntentIdentity{OperationID: fmt.Sprintf("%032x", n), DevicePK: int64(pk), DeviceCode: code, DeviceEpoch: 1, TargetScope: "device", TargetPK: int64(pk), TargetCode: code, Kind: "ptz"}
	store := playauth.NewDeviceOperationIntentStore(db)
	if dispatch {
		var err error
		store, err = playauth.NewAuthorizedDeviceOperationIntentStore(db, authoritytest.Authority(t, db))
		require.NoError(t, err)
	}
	_, err := store.Reserve(context.Background(), id)
	require.NoError(t, err)
	if dispatch {
		_, err = store.Dispatch(context.Background(), id, 1)
		require.NoError(t, err)
	}
	return id.OperationID
}

func TestAssignTransferCancelsOnlyOldReservedIntents(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db := newAssignTestDB(t)
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	device := seedAssignedDeviceWithCode(t, db, "34020000002000101001")
	other := seedAssignedDeviceWithCode(t, db, "34020000002000101002")
	reserved := reserveAssignmentIntent(t, db, device.ID, device.DeviceID, 1, false)
	dispatched := reserveAssignmentIntent(t, db, device.ID, device.DeviceID, 2, true)
	unrelated := reserveAssignmentIntent(t, db, other.ID, other.DeviceID, 3, false)
	svc := newTestAssignService(db, validatorVisibleDept1)
	_, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.NoError(t, err)
	for id, want := range map[string]string{reserved: playauth.IntentCancelled, dispatched: playauth.IntentDispatched, unrelated: playauth.IntentReserved} {
		var row playauth.DeviceOperationIntent
		require.NoError(t, db.Where("operation_id=?", id).Take(&row).Error)
		require.Equal(t, want, row.State)
		if id == reserved {
			require.Equal(t, int64(2), row.RowVersion)
			require.NotNil(t, row.CancelledAt)
			require.Nil(t, row.DispatchStartedAt)
		}
	}
	state, err := playauth.NewDeviceCleanupStore(db).Load(context.Background(), device.DeviceID)
	require.NoError(t, err)
	require.Equal(t, int64(2), state.AccessEpoch)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
}

func TestAssignTransferIntentCancellationRollsBack(t *testing.T) {
	db := newAssignTestDB(t)
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	device := seedAssignedDeviceWithCode(t, db, "34020000002000101003")
	id := reserveAssignmentIntent(t, db, device.ID, device.DeviceID, 1, false)
	failure := errors.New("fixture revocation fails after intent cancellation")
	svc := newTestAssignService(db, validatorVisibleDept1, WithTransferRecorder(&recordingTransferRecorder{err: failure}))
	_, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.ErrorIs(t, err, failure)
	var row playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id=?", id).Take(&row).Error)
	require.Equal(t, playauth.IntentReserved, row.State)
	require.Equal(t, int64(1), row.RowVersion)
	require.Nil(t, row.CancelledAt)
	state := readTransferSecurity(t, db, device.ID)
	require.Equal(t, int64(1), state.AccessEpoch)
	require.EqualValues(t, 1, state.OwnerDeptID)
}

func TestAssignTransferMissingIntentSchemaFailsClosed(t *testing.T) {
	db := newAssignTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&playauth.DeviceOperationIntent{}))
	device := seedAssignedDeviceWithCode(t, db, "34020000002000101004")
	svc := newTestAssignService(db, validatorVisibleDept1)
	_, err := svc.AssignOneWithReceipt(context.Background(), device.ID, 2, []uint{1}, true)
	require.ErrorIs(t, err, ErrAssignmentSecurityUnavailable)
	state := readTransferSecurity(t, db, device.ID)
	require.Equal(t, int64(1), state.AccessEpoch)
	require.EqualValues(t, 1, state.OwnerDeptID)
}

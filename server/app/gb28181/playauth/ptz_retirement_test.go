package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newRetirementStore(f *deviceCleanupFixture) *DeviceOperationIntentStore {
	return newDeviceOperationIntentStore(f.db, &intentCheckingAuthority{
		check:   func(*gorm.DB) error { return nil },
		retired: func(*gorm.DB, string) error { return nil },
	})
}

func reserveRetireableOperation(t *testing.T, store *DeviceOperationIntentStore, id DeviceOperationIntentIdentity, operationID string, sn int) (gbmodels.GbPTZOperation, gbmodels.GbPTZOperationAttempt) {
	t.Helper()
	op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
		OperationID: operationID, IdempotencyKey: operationID, DeviceID: 1, DeviceCode: id.DeviceCode,
		ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
		CmdType: "DeviceControl", Action: "guard_set", Status: gbmodels.PTZOperationQueued, MaxAttempts: 1, SN: sn,
	})
	require.NoError(t, err)
	attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now().Add(time.Hour))
	require.NoError(t, err)
	return op, attempt
}

const retiredForeignProcess = "ffffffffffffffffffffffffffffffff"

// A corrupt foreign-generation row must not starve the remaining rows: the
// scan skips it without fabricating a certificate, and other rows still retire.
func TestRecoverRetiredPTZSkipsCorruptRowWithoutBlockingStartup(t *testing.T) {
	f, _ := newIntentFixture(t)
	require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	store := newRetirementStore(f)
	ctx := context.Background()

	idA := intentIdentity(901)
	idA.Kind = "ptz"
	_, attemptA := reserveRetireableOperation(t, store, idA, "retire-ok", 1)
	idB := intentIdentity(902)
	idB.Kind = "ptz"
	opB, attemptB := reserveRetireableOperation(t, store, idB, "retire-corrupt", 2)
	// Simulate a damaged historical row: the operation hint cannot be resolved.
	require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET device_epoch=NULL WHERE id=?", opB.ID).Error)
	require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET owner_process_id=?, owner_run_id=? WHERE id IN (?,?)",
		retiredForeignProcess, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", attemptA.ID, attemptB.ID).Error)

	skipped, err := store.RecoverRetiredPTZ(ctx)
	require.NoError(t, err, "a corrupt row must not block publication")

	var afterA gbmodels.GbPTZOperationAttempt
	require.NoError(t, f.db.First(&afterA, attemptA.ID).Error)
	require.NotNil(t, afterA.RetiredByProcessID)
	require.NotNil(t, afterA.RetiredAt)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, afterA.Status)
	require.Equal(t, PTZOwnerProcessRetired, afterA.ErrorCode)
	var afterB gbmodels.GbPTZOperationAttempt
	require.NoError(t, f.db.First(&afterB, attemptB.ID).Error)
	require.Nil(t, afterB.RetiredByProcessID, "certificate must never be fabricated for a corrupt row")
	require.Nil(t, afterB.RetiredAt)
	require.NotEqual(t, PTZOwnerProcessRetired, afterB.ErrorCode)
	require.Equal(t, []uint{afterB.ID}, skipped)
}

// A stale caller snapshot must be rejected instead of quietly retiring a row
// that no longer matches what was observed.
func TestRetirePTZAttemptRejectsStaleSnapshot(t *testing.T) {
	f, _ := newIntentFixture(t)
	require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	store := newRetirementStore(f)
	ctx := context.Background()

	id := intentIdentity(903)
	id.Kind = "ptz"
	op, attempt := reserveRetireableOperation(t, store, id, "retire-stale", 3)
	require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET owner_process_id=?, owner_run_id=? WHERE id=?",
		retiredForeignProcess, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", attempt.ID).Error)
	var stale gbmodels.GbPTZOperationAttempt
	require.NoError(t, f.db.First(&stale, attempt.ID).Error)
	require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET sn=? WHERE id=?", op.SN+100, op.ID).Error)
	require.ErrorIs(t, store.RetirePTZAttempt(ctx, stale), ErrDeviceIntentConflict)

	var untouched gbmodels.GbPTZOperationAttempt
	require.NoError(t, f.db.First(&untouched, attempt.ID).Error)
	require.Nil(t, untouched.RetiredByProcessID, "a rejected stale snapshot must not leave a certificate behind")
}

// ReserveHomePositionQuery may derive a reconcile child from a late ACK only
// when the underlying attempt carries a complete retirement certificate.
func TestReserveHomePositionQueryRequiresRetirementCertificate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		certified  bool
		wantDenied bool
	}{
		{name: "complete-certificate-allows-derive", certified: true},
		{name: "missing-certificate-denied", wantDenied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			store := newRetirementStore(f)

			id := intentIdentity(904)
			id.Kind = "ptz"
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "hp-late", IdempotencyKey: "hp-late", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				CmdType: "DeviceControl", Action: "home_position", Status: gbmodels.PTZOperationQueued, MaxAttempts: 1, SN: 7,
			})
			require.NoError(t, err)
			attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now().Add(time.Hour))
			require.NoError(t, err)
			// The late ACK lands after the original process was retired and the
			// terminal transition has already applied.
			require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET status=?, error_code=?, owner_process_id=?, owner_run_id=? WHERE id=?",
				gbmodels.PTZOperationAttemptUnknown, PTZOwnerProcessRetired, retiredForeignProcess, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", attempt.ID).Error)
			require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET status=? WHERE id=?", gbmodels.PTZOperationAccepted, op.ID).Error)
			if tc.certified {
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET retired_by_process_id=?, retired_at=? WHERE id=?",
					"dddddddddddddddddddddddddddddddd", time.Now().In(time.Local).Truncate(time.Millisecond), attempt.ID).Error)
			}

			var errObs error
			var observation *PTZResponseObservation
			err = f.db.Transaction(func(tx *gorm.DB) error {
				var stored gbmodels.GbPTZOperation
				require.NoError(t, tx.First(&stored, op.ID).Error)
				observation, errObs = store.BeginPTZResponseObservation(tx, stored)
				require.NoError(t, errObs)
				return observation.ReserveHomePositionQuery(gbmodels.GbPTZOperation{
					OperationID: "hp-reconcile-child", CmdType: "HomePositionQuery", Action: "refresh_home_position",
					Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 1,
					QueueDeadlineAt: ptrTime(time.Now().Add(time.Minute)),
				})
			})
			if tc.wantDenied {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

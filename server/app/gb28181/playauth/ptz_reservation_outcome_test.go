package playauth

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZReservationObservationRejectsChangedCommand(t *testing.T) {
	for _, mutation := range []string{"action='changed'", "profile_version='2022'", "target_code='changed'", "response_required=0", "max_attempts=9", "sn=99"} {
		t.Run(mutation, func(t *testing.T) {
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}))
			id := intentIdentity(301)
			id.Kind = "ptz"
			deadline := time.Now().Add(time.Second)
			original := gbmodels.GbPTZOperation{OperationID: "immutable", IdempotencyKey: "immutable", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode, ProfileVersion: "2016", SN: 1,
				Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, QueueDeadlineAt: &deadline}
			op, err := store.ReservePTZOperation(context.Background(), id, original)
			require.NoError(t, err)
			ticket := &PTZReservationOutcome{store: store, identity: id, operation: original}
			require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET "+mutation+" WHERE id=?", op.ID).Error)
			_, _, err = ticket.resolve(context.Background(), false)
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		})
	}
}

func TestPTZAbandonedReservationCancelsParentAtomically(t *testing.T) {
	for _, name := range []string{"commit-and-repeat", "parent-write-fails", "operation-write-fails"} {
		t.Run(name, func(t *testing.T) {
			fail := name != "commit-and-repeat"
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}))
			id := intentIdentity(401)
			id.Kind = "ptz"
			original := gbmodels.GbPTZOperation{OperationID: "abandoned", IdempotencyKey: "abandoned", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				Status: gbmodels.PTZOperationQueued, MaxAttempts: 1}
			op, err := store.ReservePTZOperation(context.Background(), id, original)
			require.NoError(t, err)
			ticket := &PTZReservationOutcome{store: store, identity: id, operation: original}
			if name == "parent-write-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_parent_cancel BEFORE UPDATE ON gb_device_operation_intent BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			if name == "operation-write-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_operation_cancel BEFORE UPDATE ON gb_ptz_operation BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			err = ticket.Reconcile(context.Background())
			var parent DeviceOperationIntent
			require.NoError(t, f.db.First(&parent, "operation_id=?", id.OperationID).Error)
			require.NoError(t, f.db.First(&op, op.ID).Error)
			if fail {
				require.Error(t, err)
				require.Equal(t, IntentReserved, parent.State)
				require.Equal(t, gbmodels.PTZOperationQueued, op.Status)
			} else {
				require.NoError(t, err)
				require.Equal(t, IntentCancelled, parent.State)
				require.True(t, validIntentRow(parent))
				require.Equal(t, gbmodels.PTZOperationUnknown, op.Status)
				require.NoError(t, ticket.Reconcile(context.Background()))
			}
		})
	}
}

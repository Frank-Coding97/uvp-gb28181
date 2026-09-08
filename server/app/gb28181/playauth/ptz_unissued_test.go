package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZUnissuedRejectsChangedSnapshot(t *testing.T) {
	for _, mutation := range []string{"unchanged", "channel_id=12", "target_code='changed'", "sn=99", "attempt-sn", "attempt-lease"} {
		t.Run(mutation, func(t *testing.T) {
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			id := intentIdentity(601)
			id.Kind = "ptz"
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "snapshot", IdempotencyKey: "snapshot", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "alarm", TargetCode: "34020000001340000001",
				Status: gbmodels.PTZOperationQueued, MaxAttempts: 1, SN: 1})
			require.NoError(t, err)
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: true}
			fault := newIntentFixtureStore(faultDB)
			attempt, err := fault.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now())
			require.Zero(t, attempt.ID)
			var ticket *PTZUnissuedAttempt
			require.ErrorAs(t, err, &ticket)
			// This state-machine fixture injects only the original commit fault.
			ticket.store = store
			switch mutation {
			case "unchanged":
			case "attempt-sn":
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET sn=99").Error)
			case "attempt-lease":
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation_attempt SET lease_until=?", time.Now().Add(time.Hour)).Error)
			default:
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET "+mutation+" WHERE id=?", op.ID).Error)
			}
			err = ticket.Reconcile(context.Background())
			if mutation == "unchanged" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
		})
	}
}

func TestPTZUnissuedRollbackAllowsLaterCancellation(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "missing-advanced-attempt"}[advanced], func(t *testing.T) {
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			id := intentIdentity(501)
			id.Kind = "ptz"
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "rollback", IdempotencyKey: "rollback", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				Status: gbmodels.PTZOperationQueued, MaxAttempts: 1})
			require.NoError(t, err)
			require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_claim BEFORE INSERT ON gb_ptz_operation_attempt BEGIN SELECT RAISE(ABORT, 'injected rollback'); END`).Error)
			attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now())
			require.Zero(t, attempt.ID)
			var ticket *PTZUnissuedAttempt
			require.ErrorAs(t, err, &ticket)
			require.NoError(t, store.CancelReserved(context.Background(), id.OperationID, 1))
			require.NoError(t, f.db.Model(&op).Update("status", gbmodels.PTZOperationRejected).Error)
			if advanced {
				require.NoError(t, f.db.Model(&op).Update("attempt", 1).Error)
			}
			err = ticket.Reconcile(context.Background())
			if advanced {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			} else {
				require.NoError(t, err)
				require.NoError(t, ticket.Reconcile(context.Background()))
			}
		})
	}
}

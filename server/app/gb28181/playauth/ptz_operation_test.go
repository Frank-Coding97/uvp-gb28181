package playauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZClaimReadsStoredAttemptBeforeGrant(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "stored-time", true: "readback-fails"}[fail], func(t *testing.T) {
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			id := intentIdentity(801)
			id.Kind = "ptz"
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "readback", IdempotencyKey: "readback", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				Status: gbmodels.PTZOperationQueued, MaxAttempts: 1})
			require.NoError(t, err)
			if fail {
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("fail-attempt-read", func(tx *gorm.DB) {
					if _, readingAttempt := tx.Statement.Dest.(*gbmodels.GbPTZOperationAttempt); readingAttempt {
						tx.AddError(errors.New("injected read failure"))
					}
				}))
			} else {
				// Model a database-normalized representation. This tests the
				// readback boundary, not a native driver's timezone contract.
				require.NoError(t, f.db.Exec(`CREATE TRIGGER normalize_attempt AFTER INSERT ON gb_ptz_operation_attempt BEGIN UPDATE gb_ptz_operation_attempt SET started_at='2026-09-08 01:00:00', lease_until='2026-09-08 01:00:15' WHERE id=NEW.id; END`).Error)
			}
			attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now())
			if fail {
				require.Error(t, err)
				require.Zero(t, attempt.ID)
				var receipt *PTZUnissuedAttempt
				require.ErrorAs(t, err, &receipt, "readback failed only after the attempted insert")
				require.NoError(t, f.db.Callback().Query().Remove("fail-attempt-read"))
				var parent DeviceOperationIntent
				require.NoError(t, f.db.First(&parent, "operation_id=?", id.OperationID).Error)
				require.Equal(t, IntentReserved, parent.State)
			} else {
				require.NoError(t, err)
				var stored gbmodels.GbPTZOperationAttempt
				require.NoError(t, f.db.First(&stored, attempt.ID).Error)
				require.True(t, stored.StartedAt.Equal(attempt.StartedAt))
				require.True(t, stored.LeaseUntil.Equal(attempt.LeaseUntil))
			}
		})
	}
}

func TestPTZAlarmRetainsChannelAuthority(t *testing.T) {
	for _, name := range []string{"alarm", "device-wire-fallback", "changed-channel", "changed-device"} {
		t.Run(name, func(t *testing.T) {
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			id := intentIdentity(301)
			id.Kind = "ptz"
			wire := "34020000001340000001"
			if name == "device-wire-fallback" {
				wire = id.DeviceCode
			}
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "alarm-op", IdempotencyKey: "alarm-key", DeviceID: uint(id.DevicePK), DeviceCode: id.DeviceCode,
				ChannelID: uint(id.TargetPK), ChannelCode: id.TargetCode, TargetScope: "alarm", TargetCode: wire,
				Status: gbmodels.PTZOperationQueued, MaxAttempts: 1})
			require.NoError(t, err)
			require.Equal(t, wire, op.TargetCode)
			if name == "changed-channel" {
				require.NoError(t, f.db.Model(&op).Update("channel_id", op.ChannelID+1).Error)
			}
			if name == "changed-device" {
				require.NoError(t, f.db.Model(&op).Update("device_id", op.DeviceID+1).Error)
			}
			attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now())
			if name == "changed-channel" || name == "changed-device" {
				require.Error(t, err)
				require.Zero(t, attempt.ID)
			} else {
				require.NoError(t, err)
				require.NotZero(t, attempt.ID)
			}
		})
	}
}

func TestPTZClaimUsesDatabaseMillisecondPrecision(t *testing.T) {
	f, store := newIntentFixture(t)
	require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	id := intentIdentity(701)
	id.Kind = "ptz"
	op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
		OperationID: "precision", IdempotencyKey: "precision", DeviceID: 1, DeviceCode: id.DeviceCode,
		ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
		Status: gbmodels.PTZOperationQueued, MaxAttempts: 1})
	require.NoError(t, err)
	var parent DeviceOperationIntent
	require.NoError(t, f.db.First(&parent, "operation_id=?", id.OperationID).Error)
	require.Zero(t, parent.CreatedAt.Nanosecond()%int(time.Millisecond))
	now := time.Now().Add(time.Second).Truncate(time.Millisecond).Add(876543 * time.Nanosecond)
	attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, now)
	require.NoError(t, err)
	require.True(t, attempt.StartedAt.Equal(now.UTC().Truncate(time.Millisecond)))
	require.Zero(t, attempt.LeaseUntil.Nanosecond()%int(time.Millisecond))
	require.NoError(t, f.db.First(&parent, "operation_id=?", id.OperationID).Error)
	require.True(t, validIntentRow(parent))
}

func TestPTZReservationBindsOperationAtomically(t *testing.T) {
	for _, name := range []string{"commit", "operation-write-fails", "revoked", "no-authority", "wrong-channel"} {
		t.Run(name, func(t *testing.T) {
			fail := name != "commit"
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}))
			if name == "operation-write-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_ptz BEFORE INSERT ON gb_ptz_operation BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			id := intentIdentity(101)
			id.Kind = "ptz"
			if name == "revoked" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch = 2, cleanup_completed_epoch = 2 WHERE id = 1").Error)
			}
			if name == "no-authority" {
				store = NewDeviceOperationIntentStore(f.db)
			}
			deadline := time.Now().Add(5 * time.Second)
			op := gbmodels.GbPTZOperation{OperationID: "test-ptz", IdempotencyKey: "key", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, QueueDeadlineAt: &deadline}
			if name == "wrong-channel" {
				op.ChannelID = 12
			}
			got, err := store.ReservePTZOperation(context.Background(), id, op)
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, id.OperationID, *got.DeviceIntentID)
				require.Equal(t, id.DeviceEpoch, *got.DeviceEpoch)
				require.Zero(t, got.Attempt)
			}
			var parents, operations int64
			require.NoError(t, f.db.Model(&DeviceOperationIntent{}).Count(&parents).Error)
			require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Count(&operations).Error)
			want := int64(1)
			if fail {
				want = 0
			}
			require.Equal(t, want, parents)
			require.Equal(t, want, operations)
		})
	}
}

func TestPTZClaimCommitsParentAndAttemptTogether(t *testing.T) {
	for _, name := range []string{"commit", "attempt-write-fails", "revoked", "no-authority", "binding-changed"} {
		t.Run(name, func(t *testing.T) {
			fail := name != "commit"
			f, store := newIntentFixture(t)
			require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
			id := intentIdentity(201)
			id.Kind = "ptz"
			now := time.Now().UTC()
			deadline := now.Add(5 * time.Second)
			op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
				OperationID: "claim-ptz", IdempotencyKey: "claim", DeviceID: 1, DeviceCode: id.DeviceCode,
				ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
				Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, QueueDeadlineAt: &deadline})
			require.NoError(t, err)
			if name == "attempt-write-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_attempt BEFORE INSERT ON gb_ptz_operation_attempt BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			if name == "revoked" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
			}
			if name == "no-authority" {
				store = NewDeviceOperationIntentStore(f.db)
			}
			if name == "binding-changed" {
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET device_epoch=2 WHERE id=?", op.ID).Error)
			}
			attempt, err := store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, now)
			var parent DeviceOperationIntent
			require.NoError(t, f.db.First(&parent, "operation_id = ?", id.OperationID).Error)
			require.NoError(t, f.db.First(&op, op.ID).Error)
			if fail {
				require.Error(t, err)
				require.Zero(t, attempt.ID)
				require.Equal(t, IntentReserved, parent.State)
				require.Zero(t, op.Attempt)
			} else {
				require.NoError(t, err)
				require.NotZero(t, attempt.ID)
				require.Equal(t, IntentDispatched, parent.State)
				require.Equal(t, 1, op.Attempt)
				require.NotNil(t, attempt.OwnerProcessID)
				require.NotNil(t, attempt.OwnerRunID)
				_, err = store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, now)
				require.Error(t, err, "a committed attempt cannot be issued twice")
			}
		})
	}
}

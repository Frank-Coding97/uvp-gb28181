package ptz

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func TestSchedulerResultRequiresOriginalOwnerAndAtomicExit(t *testing.T) {
	for _, mode := range []string{"wrong-owner", "exit-write-fails", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newSchedulerFixture(t, 1)
			op := f.createOperation(t, 3)
			require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
			attempt := loadSchedulerAttempts(t, f.db, op.ID)[0]
			process, run := "11111111111111111111111111111111", "22222222222222222222222222222222"
			require.NoError(t, f.db.Model(&gbmodels.GbPTZOperationAttempt{}).Where("id=?", attempt.ID).
				Updates(map[string]any{"owner_process_id": process, "owner_run_id": run}).Error)
			attempt.OwnerProcessID, attempt.OwnerRunID = &process, &run
			if mode == "wrong-owner" {
				wrong := "33333333333333333333333333333333"
				attempt.OwnerRunID = &wrong
			}
			if mode == "exit-write-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_exit BEFORE UPDATE OF local_quiesced_at ON gb_ptz_operation_attempt BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			err := f.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil, f.clock.Now())
			got := loadSchedulerAttempts(t, f.db, op.ID)[0]
			if mode == "commit" {
				require.NoError(t, err)
				require.NotNil(t, got.LocalQuiescedAt)
				require.Equal(t, gbmodels.PTZOperationAttemptSent, got.Status)
			} else {
				require.Error(t, err)
				require.Nil(t, got.LocalQuiescedAt)
				require.Equal(t, gbmodels.PTZOperationAttemptDispatching, got.Status)
				require.Equal(t, gbmodels.PTZOperationQueued, loadSchedulerOperation(t, f.db, op.ID).Status)
			}
		})
	}
}

func TestSchedulerOneWayResultDoesNotWaitForApplicationResponse(t *testing.T) {
	for _, status := range []int{200, 403, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newSchedulerFixture(t, 1)
			op := f.createOperation(t, 1)
			require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
			attempt := loadSchedulerAttempts(t, f.db, op.ID)[0]
			require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Where("id=?", op.ID).UpdateColumn("response_required", false).Error)
			err := f.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{StatusCode: status, Attempted: true}, nil, f.clock.Now())
			require.NoError(t, err)
			got := loadSchedulerOperation(t, f.db, op.ID)
			want := gbmodels.PTZOperationSent
			if status == 403 {
				want = gbmodels.PTZOperationRejected
			}
			if status == 0 {
				want = gbmodels.PTZOperationUnknown
			}
			require.Equal(t, want, got.Status)
			require.Nil(t, got.DeadlineAt)
			require.Nil(t, got.NextAttemptAt)
		})
	}
}

func TestSchedulerRevocationPreservesPossibleSend(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 3)
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	op = loadSchedulerOperation(t, f.db, op.ID)
	require.NoError(t, f.scheduler.revokeOperation(context.Background(), op, f.clock.Now()))
	got := loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationUnknown, got.Status)
	require.Equal(t, "DEVICE_EPOCH_REVOKED", got.ErrorCode)
	require.Nil(t, got.NextAttemptAt)
	attempt := loadSchedulerAttempts(t, f.db, op.ID)[0]
	require.Equal(t, gbmodels.PTZOperationAttemptDispatching, attempt.Status, "revocation must not fabricate sender exit")
	require.Nil(t, attempt.LocalQuiescedAt)
}

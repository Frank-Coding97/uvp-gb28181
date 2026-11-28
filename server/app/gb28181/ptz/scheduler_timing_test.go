package ptz

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestSchedulerPreparationDoesNotConsumeResponseWindow(t *testing.T) {
	for _, maxAttempts := range []int{1, 3} {
		t.Run(fmt.Sprint(maxAttempts), func(t *testing.T) {
			responseWindow := time.Second
			if maxAttempts == 1 {
				responseWindow = 15 * time.Second
			}
			f := newSchedulerFixture(t, 1)
			op := f.createOperation(t, maxAttempts)
			t0 := f.clock.Now()
			// Simulate seven seconds in the claim/commit and three in target lookup.
			require.NoError(t, f.db.Callback().Create().After("gorm:create").Register("slow-claim", func(tx *gorm.DB) {
				if tx.Statement.Table == "gb_ptz_operation_attempt" {
					f.clock.Set(f.clock.Now().Add(7 * time.Second))
				}
			}))
			require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("slow-target", func(tx *gorm.DB) {
				if tx.Statement.Table == "gb_device" {
					f.clock.Set(f.clock.Now().Add(3 * time.Second))
				}
			}))
			require.NoError(t, f.scheduler.RunDue(t0))
			f.dispatcher.Drain()
			require.Len(t, f.sender.Calls(), 1)
			sent := t0.Add(10 * time.Second)
			require.True(t, f.sender.Calls()[0].deadline.Equal(sent.Add(responseWindow)), "response window starts at sender entry")
			got := loadSchedulerOperation(t, f.db, op.ID)
			require.Equal(t, gbmodels.PTZOperationSent, got.Status)
			require.True(t, got.DispatchStartedAt.Equal(sent))
			require.True(t, got.TransportDeadlineAt.Equal(sent.Add(15*time.Second)))
			if maxAttempts > 1 {
				require.True(t, got.NextAttemptAt.Equal(sent.Add(time.Second)))
			} else {
				require.Nil(t, got.NextAttemptAt)
			}
		})
	}
}

func TestSchedulerClaimsUseIndividualCurrentTime(t *testing.T) {
	f := newSchedulerFixture(t, 2)
	t0 := f.clock.Now()
	f.createOperation(t, 1)
	f.clock.Set(t0.Add(time.Millisecond))
	second := f.createOperation(t, 1)
	f.clock.Set(t0)
	require.NoError(t, f.db.Callback().Create().After("gorm:create").Register("slow-claims", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_ptz_operation_attempt" {
			f.clock.Set(f.clock.Now().Add(3 * time.Second))
		}
	}))
	require.NoError(t, f.scheduler.RunDue(t0))
	attempts := loadSchedulerAttempts(t, f.db, second.ID)
	require.Len(t, attempts, 1)
	require.True(t, attempts[0].StartedAt.Equal(t0.Add(3*time.Second)), "later candidate must not reuse batch timestamp")
	f.dispatcher.Drain()
}

func TestSchedulerExpiredPreparationNeverInvokesSender(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 3)
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	f.clock.Set(f.clock.Now().Add(16 * time.Second))
	f.dispatcher.Drain()
	require.Empty(t, f.sender.Calls())
	require.Equal(t, gbmodels.PTZOperationRejected, loadSchedulerOperation(t, f.db, op.ID).Status)
}

package playauth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceRTPCleanupWorkCallsNeedNewConfirmedDispatch(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	identity := rtpStepIdentity(1)
	identity.TCPMode = 0
	_, err := store.AddRTPResourceStep(ctx, id, 2, identity)
	require.NoError(t, err)
	_, original, err := store.DispatchRTPResourceWork(ctx, id, 3, identity.StepID)
	require.NoError(t, err)
	require.NoError(t, original.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	barrier := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	work, err := barrier.ReserveRTPCleanup(ctx, store, id, identity.StepID)
	require.NoError(t, err)
	_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		t.Fatal("unprepared call")
		return "", nil
	})
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	require.NoError(t, work.Prepare(ctx))
	duplicate, err := barrier.ReserveRTPCleanup(ctx, NewDeviceOperationIntentStore(f.db), id, identity.StepID)
	require.NoError(t, err)
	require.Same(t, work, duplicate)
	calls := 0
	for _, result := range []string{"rtp_ingress_drained", "close_pending"} {
		got, err := work.CloseIngress(ctx, func(_ context.Context, target DeviceRTPResourceIdentity) (string, error) {
			calls++
			require.Equal(t, identity, target)
			return result, nil
		})
		require.NoError(t, err)
		require.Equal(t, result, got)
	}
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	r := loaded.Steps[0].Recovery
	require.EqualValues(t, 2, r.CallSequence)
	require.Equal(t, "rtp_ingress_drained", r.IngressEvidence.Result)
	require.Equal(t, "close_pending", r.CurrentCall.Result)
	require.Equal(t, IntentDispatched, loaded.Intent.State)
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, barrier.WaitBefore(short, uint(id.DevicePK), 2), context.DeadlineExceeded)
	copy := *work
	require.NoError(t, copy.Quiesce(ctx), "copying an opaque handle shares the same strong owner and exit")
	require.NoError(t, barrier.WaitBefore(ctx, uint(id.DevicePK), 2))
	_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { calls++; return "close_pending", nil })
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	require.Equal(t, 2, calls)
	next, err := barrier.ReserveRTPCleanup(ctx, store, id, identity.StepID)
	require.NoError(t, err)
	require.NotSame(t, work, next)
	require.NoError(t, next.Prepare(ctx))
	require.NoError(t, next.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 2, loaded.Steps[0].Recovery.Generation)
	require.EqualValues(t, 2, loaded.Steps[0].Recovery.CallSequence)
	require.Equal(t, "rtp_ingress_drained", loaded.Steps[0].Recovery.IngressEvidence.Result)
}

func TestDeviceRTPCleanupWorkUnknownCommitNeverSends(t *testing.T) {
	for _, phase := range []string{"prepare", "dispatch", "outcome"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", phase, committed), func(t *testing.T) {
				f, store, id := newRTPStepFixture(t)
				ctx := context.Background()
				_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
				require.NoError(t, err)
				_, original, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
				require.NoError(t, err)
				require.NoError(t, original.Quiesce(ctx))
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				barrier := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
				work, err := barrier.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
				require.NoError(t, err)
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				faultStore := NewDeviceOperationIntentStore(faultDB)
				if phase == "prepare" {
					work.work.store = faultStore
					require.Error(t, work.Prepare(ctx))
				} else {
					require.NoError(t, work.Prepare(ctx))
				}
				if phase == "dispatch" {
					work.work.store = faultStore
				}
				calls := 0
				_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
					calls++
					if phase == "outcome" {
						work.work.store = faultStore
					}
					return "close_pending", nil
				})
				require.Error(t, err)
				expected := 0
				if phase == "outcome" {
					expected = 1
				}
				require.Equal(t, expected, calls)
				_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { calls++; return "close_pending", nil })
				require.Error(t, err)
				require.Equal(t, expected, calls, "unconfirmed final facts must block the next HTTP call")
				work.work.store = store
				require.NoError(t, work.Quiesce(ctx))
				require.NoError(t, barrier.WaitBefore(ctx, uint(id.DevicePK), 2))
				loaded, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				if phase == "dispatch" && committed {
					require.Equal(t, rtpCallNotInvoked, loaded.Steps[0].Recovery.CurrentCall.Outcome)
				}
				if phase == "outcome" {
					require.Equal(t, rtpCallObserved, loaded.Steps[0].Recovery.CurrentCall.Outcome)
				}
				require.Equal(t, IntentDispatched, loaded.Intent.State)
			})
		}
	}
}

func TestDeviceRTPCleanupWorkQuiesceJoinsRealCall(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, w.Prepare(ctx))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, e := w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
			close(entered)
			<-release
			return "", errors.New("lost HTTP reply")
		})
		done <- e
	}()
	<-entered
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, w.Quiesce(short), context.DeadlineExceeded)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].Recovery.LocalQuiescedAt)
	close(release)
	require.Error(t, <-done)
	require.NoError(t, w.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, rtpCallUnknown, loaded.Steps[0].Recovery.CurrentCall.Outcome)
	require.Nil(t, loaded.Steps[0].Recovery.ResourceEvidence)
}

func TestDeviceRTPCleanupWorkWaitsForOldLeaseAndRejectsOtherAuthority(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	ctx := context.Background()
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	lease, err := b.BeginEpoch(ctx, id.DeviceCode, id.DeviceEpoch)
	require.NoError(t, err)
	_, err = store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, w.Prepare(short), context.DeadlineExceeded)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].Recovery)
	lease.Release()
	require.NoError(t, w.Prepare(ctx))
	_, otherStore, _ := newRTPStepFixture(t)
	_, err = b.ReserveRTPCleanup(ctx, otherStore, id, rtpStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	require.NoError(t, w.Quiesce(ctx))
}

func TestDeviceRTPCleanupWorkSealDuringDispatchNeverInvokesHTTP(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, w.Prepare(ctx))
	const hook = "fixture:seal_rtp_recovery_dispatch"
	require.NoError(t, f.db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) { w.work.sealed.Store(true) }))
	t.Cleanup(func() { require.NoError(t, f.db.Callback().Update().Remove(hook)) })
	calls := 0
	_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { calls++; return "close_pending", nil })
	require.Error(t, err)
	require.Zero(t, calls, "sealing while SQL commits must still prevent the HTTP call")
	require.NoError(t, w.Quiesce(ctx))
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, rtpCallNotInvoked, loaded.Steps[0].Recovery.CurrentCall.Outcome)
}

func TestDeviceRTPCleanupWorkUnknownDispatchThenUnknownQuiesceCanRetry(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f, store, id := newRTPStepFixture(t)
			ctx := context.Background()
			_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
			require.NoError(t, err)
			_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
			require.NoError(t, err)
			require.NoError(t, old.Quiesce(ctx))
			require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
			w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
			require.NoError(t, err)
			require.NoError(t, w.Prepare(ctx))
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: false}
			w.work.store = NewDeviceOperationIntentStore(faultDB)
			_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
				t.Fatal("unknown commit sent HTTP")
				return "", nil
			})
			require.Error(t, err)
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
			require.Error(t, w.Quiesce(ctx))
			w.work.store = store
			require.NoError(t, w.Quiesce(ctx))
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			require.Nil(t, loaded.Steps[0].Recovery.CurrentCall)
			require.Zero(t, loaded.Steps[0].Recovery.CallSequence)
		})
	}
}

func TestDeviceRTPCleanupWorkRollingSlotKeepsStrongerEvidenceAndUnknownHistory(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, w.Prepare(ctx))
	_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "shutdown_scheduled", nil })
	require.NoError(t, err)
	_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		return "", errors.New("unknown HTTP response")
	})
	require.Error(t, err)
	for i := 0; i < 40; i++ {
		_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "runtime_mismatch", nil })
		require.NoError(t, err)
	}
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	r := loaded.Steps[0].Recovery
	require.EqualValues(t, 42, r.CallSequence)
	require.Equal(t, "shutdown_scheduled", r.ResourceEvidence.Result)
	require.EqualValues(t, 1, r.ResourceEvidence.Sequence)
	require.True(t, r.PriorObservationIncomplete)
	require.Equal(t, "runtime_mismatch", r.CurrentCall.Result)
	require.NoError(t, w.Quiesce(ctx))
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id = ?", id.OperationID).Scan(&raw).Error)
	require.Less(t, len(raw), 2400, "completed attempts are not retained in a growing array")
	next, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, next.Prepare(ctx))
	require.NoError(t, next.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.True(t, loaded.Steps[0].Recovery.PriorObservationIncomplete)
	require.Equal(t, "shutdown_scheduled", loaded.Steps[0].Recovery.ResourceEvidence.Result)
}

func TestDeviceRTPCleanupWorkRegistryBoundDoesNotEvictOnCancellation(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	owners := make([]*RTPRecoveryWork, 0, 64)
	for i := 0; i < 64; i++ {
		identity := id
		identity.OperationID = fmt.Sprintf("%032x", i+1)
		w, err := b.ReserveRTPCleanup(ctx, store, identity, rtpStepIdentity(1).StepID)
		require.NoError(t, err)
		owners = append(owners, w)
	}
	cancel()
	duplicate, err := b.ReserveRTPCleanup(context.Background(), store, owners[0].work.id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.Same(t, owners[0], duplicate, "existing work is retained, not replaced by a fresh context")
	id.OperationID = fmt.Sprintf("%032x", 65)
	_, err = b.ReserveRTPCleanup(context.Background(), store, id, rtpStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	for _, w := range owners {
		require.NoError(t, w.Quiesce(context.Background()))
	}
	w, err := b.ReserveRTPCleanup(context.Background(), store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, w.Quiesce(context.Background()))
}

func TestDeviceRTPCleanupWorkConcurrentReservationAndPrepareAreSingleOwner(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	results := make(chan *RTPRecoveryWork, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, e := b.ReserveRTPCleanup(ctx, NewDeviceOperationIntentStore(f.db), id, rtpStepIdentity(1).StepID)
			results <- w
			if e == nil {
				e = w.Prepare(ctx)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	w := <-results
	for other := range results {
		require.Same(t, w, other)
	}
	passed := 0
	for e := range errs {
		if e == nil {
			passed++
		} else {
			require.ErrorIs(t, e, ErrDeviceIntentConflict)
		}
	}
	require.Equal(t, 1, passed)
	require.NoError(t, w.Quiesce(ctx))
}

func TestDeviceRTPCleanupWorkRejectsTakeoverAndSequenceOverflow(t *testing.T) {
	for _, kind := range []string{"active-original", "active-recovery", "other-process", "sequence-overflow", "generation-overflow", "current-epoch"} {
		t.Run(kind, func(t *testing.T) {
			f, store, id := newRTPStepFixture(t)
			ctx := context.Background()
			_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
			require.NoError(t, err)
			_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
			require.NoError(t, err)
			if kind != "active-original" {
				require.NoError(t, old.Quiesce(ctx))
			}
			if kind != "current-epoch" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			}
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			if kind != "active-original" && kind != "current-epoch" {
				processID, err := sipCleanupProcessID()
				require.NoError(t, err)
				_, err = store.mutateRTPFacts(ctx, id, loaded.Intent.RowVersion, authorizeRTPCleanupDevice, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
					r := &DeviceRTPRecovery{Version: 1, Generation: 2, OwnerProcessID: processID, OwnerRunID: strings.Repeat("a", 32), ReservedAt: now, LocalQuiescedAt: &now}
					switch kind {
					case "active-recovery":
						r.LocalQuiescedAt = nil
					case "other-process":
						r.OwnerProcessID = strings.Repeat("d", 32)
					case "sequence-overflow":
						r.CallSequence = math.MaxInt64
					case "generation-overflow":
						r.Generation = math.MaxInt64
					}
					out.Steps[0].Recovery = r
					return true, nil
				})
				require.NoError(t, err)
			}
			b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
			w, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
			require.NoError(t, err)
			err = w.Prepare(ctx)
			if kind == "sequence-overflow" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			_, err = w.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
				t.Fatal("denied recovery performed HTTP")
				return "", nil
			})
			require.Error(t, err)
			require.NoError(t, w.Quiesce(ctx))
		})
	}
}

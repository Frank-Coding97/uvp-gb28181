package playauth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type rtpCleanupResolverFunc func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error)

func (f rtpCleanupResolverFunc) ResolveRTPCleanup(ctx context.Context, id DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
	return f(ctx, id)
}

type rtpCleanupRuntimeFixtureControl struct {
	resource, ingress func(context.Context) (string, error)
	release           func()
}

func (r *rtpCleanupRuntimeFixtureControl) CloseResource(ctx context.Context) (string, error) {
	return r.resource(ctx)
}
func (r *rtpCleanupRuntimeFixtureControl) CloseIngress(ctx context.Context) (string, error) {
	return r.ingress(ctx)
}
func (r *rtpCleanupRuntimeFixtureControl) Release() { r.release() }

func rtpCleanupRuntimeFixture(t *testing.T) (*gorm.DB, *DeviceOperationBarrier, *DeviceOperationIntentStore, DeviceOperationIntentIdentity, *RTPRecoveryWork) {
	t.Helper()
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	identity := rtpStepIdentity(1)
	identity.TCPMode = 0
	_, err := store.AddRTPResourceStep(ctx, id, 2, identity)
	require.NoError(t, err)
	_, old, err := store.DispatchRTPResourceWork(ctx, id, 3, identity.StepID)
	require.NoError(t, err)
	require.NoError(t, old.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	w, err := b.ReserveRTPCleanup(ctx, store, id, identity.StepID)
	require.NoError(t, err)
	return f.db, b, store, id, w
}

func TestDeviceRTPCleanupRuntimeRunRequiresPrepareAndRunsOnce(t *testing.T) {
	_, _, store, id, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	var resolved, resource, ingress, released atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{
		resource: func(ctx context.Context) (string, error) {
			resource.Add(1)
			out, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, rtpCleanupResource, out.Steps[0].Recovery.CurrentCall.Action)
			require.Empty(t, out.Steps[0].Recovery.CurrentCall.Outcome)
			return "close_pending", nil
		},
		ingress: func(ctx context.Context) (string, error) { ingress.Add(1); return "rtp_ingress_drained", nil },
		release: func() { released.Add(1) },
	}
	resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
		resolved.Add(1)
		return runtime, nil
	})
	require.Error(t, w.Run(ctx, resolver))
	require.Zero(t, resolved.Load())
	require.NoError(t, w.Prepare(ctx))
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- w.Run(ctx, resolver) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, resolved.Load())
	require.EqualValues(t, 1, resource.Load())
	require.EqualValues(t, 1, ingress.Load())
	require.Zero(t, released.Load())
	require.NoError(t, w.Quiesce(ctx))
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, released.Load())
	require.Error(t, w.Run(ctx, resolver))
	out, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, IntentDispatched, out.Intent.State)
}

func TestDeviceRTPCleanupRuntimeResolveIsJoinedAndLateRuntimeReleased(t *testing.T) {
	_, b, store, id, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	entered, cancelled, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var freed atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{resource: func(context.Context) (string, error) { t.Error("late resolver started resource HTTP"); return "", nil }, ingress: func(context.Context) (string, error) { t.Error("late resolver started ingress HTTP"); return "", nil }, release: func() { freed.Add(1) }}
	go func() {
		done <- w.Run(ctx, rtpCleanupResolverFunc(func(ctx context.Context, _ DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return runtime, nil
		}))
	}()
	<-entered
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, w.Quiesce(short), context.DeadlineExceeded)
	<-cancelled
	require.Zero(t, freed.Load())
	out, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, out.Steps[0].Recovery.LocalQuiescedAt)
	short2, cancel2 := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel2()
	require.ErrorIs(t, b.WaitBefore(short2, uint(id.DevicePK), 2), context.DeadlineExceeded)
	close(release)
	require.Error(t, <-done)
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
}

func TestDeviceRTPCleanupRuntimeOutcomeFailureDoesNotRepeatResource(t *testing.T) {
	db, _, store, id, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	var resource, ingress, freed atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{resource: func(context.Context) (string, error) { resource.Add(1); return "close_pending", nil }, ingress: func(context.Context) (string, error) { ingress.Add(1); return "rtp_ingress_drained", nil }, release: func() { freed.Add(1) }}
	resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) { return runtime, nil })
	require.NoError(t, db.Exec(`CREATE TRIGGER deny_rtp_cleanup_outcome BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.currentCall.outcome') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture final facts unavailable'); END`).Error)
	require.Error(t, w.Run(ctx, resolver))
	require.EqualValues(t, 1, resource.Load())
	require.Zero(t, ingress.Load())
	require.Zero(t, freed.Load())
	require.Error(t, w.Run(ctx, resolver))
	require.EqualValues(t, 1, resource.Load())
	require.Zero(t, ingress.Load())
	require.NoError(t, db.Exec("DROP TRIGGER deny_rtp_cleanup_outcome").Error)
	require.NoError(t, w.Run(ctx, resolver))
	require.EqualValues(t, 1, resource.Load())
	require.EqualValues(t, 1, ingress.Load())
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
	out, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 2, out.Steps[0].Recovery.CallSequence)
}

func TestDeviceRTPCleanupRuntimeReleaseBeforeFinalSQLAndNoReacquire(t *testing.T) {
	db, b, store, id, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	var resolved, freed atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{resource: func(context.Context) (string, error) { return "close_pending", nil }, ingress: func(context.Context) (string, error) { return "close_pending", nil }, release: func() {
		freed.Add(1)
		out, err := store.LoadRTPResourceSteps(ctx, id)
		require.NoError(t, err)
		require.Nil(t, out.Steps[0].Recovery.LocalQuiescedAt, "owner exit may not precede runtime release")
	}}
	resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
		resolved.Add(1)
		return runtime, nil
	})
	require.NoError(t, w.Run(ctx, resolver))
	require.NoError(t, db.Exec(`CREATE TRIGGER deny_rtp_runtime_exit BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.localQuiescedAt') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture local exit unavailable'); END`).Error)
	require.Error(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, b.WaitBefore(short, uint(id.DevicePK), 2), context.DeadlineExceeded)
	require.Error(t, w.Run(ctx, resolver))
	require.EqualValues(t, 1, resolved.Load())
	require.NoError(t, db.Exec("DROP TRIGGER deny_rtp_runtime_exit").Error)
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
}

func TestDeviceRTPCleanupRuntimeResolverFailureCannotRetryWithinOwner(t *testing.T) {
	_, _, _, _, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	var count atomic.Int32
	resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
		count.Add(1)
		return nil, errors.New("untrusted runtime")
	})
	require.Error(t, w.Run(ctx, resolver))
	require.Error(t, w.Run(ctx, resolver))
	require.EqualValues(t, 1, count.Load())
	require.NoError(t, w.Quiesce(ctx))
}

func TestDeviceRTPCleanupRuntimeHTTPThatIgnoresCancelMustActuallyReturn(t *testing.T) {
	_, b, store, id, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var freed, ingress atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{resource: func(context.Context) (string, error) { close(entered); <-release; return "close_pending", nil }, ingress: func(context.Context) (string, error) { ingress.Add(1); return "close_pending", nil }, release: func() { freed.Add(1) }}
	go func() {
		done <- w.Run(ctx, rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) { return runtime, nil }))
	}()
	<-entered
	short, cancel := context.WithTimeout(ctx, 15*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, w.Quiesce(short), context.DeadlineExceeded)
	require.Zero(t, freed.Load())
	out, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, out.Steps[0].Recovery.LocalQuiescedAt)
	require.Nil(t, out.Steps[0].Recovery.CurrentCall.LocalQuiescedAt)
	close(release)
	require.Error(t, <-done)
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
	require.Zero(t, ingress.Load())
	require.NoError(t, b.WaitBefore(ctx, uint(id.DevicePK), 2))
	out, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, rtpCallObserved, out.Steps[0].Recovery.CurrentCall.Outcome)
}

func TestDeviceRTPCleanupRuntimeNonconformingResolverStillRetainsPartialRuntime(t *testing.T) {
	_, _, _, _, w := rtpCleanupRuntimeFixture(t)
	ctx := context.Background()
	require.NoError(t, w.Prepare(ctx))
	var freed atomic.Int32
	runtime := &rtpCleanupRuntimeFixtureControl{resource: func(context.Context) (string, error) { t.Fatal("partial resolver granted a call"); return "", nil }, release: func() { freed.Add(1) }}
	resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
		return runtime, errors.New("partial initialization")
	})
	require.Error(t, w.Run(ctx, resolver))
	require.Zero(t, freed.Load())
	require.NoError(t, w.Quiesce(ctx))
	require.EqualValues(t, 1, freed.Load())
}

func TestDeviceRTPCleanupRuntimeDispositionSeparatesSQLAndResolverFailure(t *testing.T) {
	for _, kind := range []string{"resolver", "resource", "ingress", "sql"} {
		t.Run(kind, func(t *testing.T) {
			db, _, _, _, w := rtpCleanupRuntimeFixture(t)
			ctx := context.Background()
			require.NoError(t, w.Prepare(ctx))
			runtime := &rtpCleanupRuntimeFixtureControl{
				resource: func(context.Context) (string, error) {
					if kind == "resource" {
						return "", errors.New("resource response lost")
					}
					return "close_pending", nil
				},
				ingress: func(context.Context) (string, error) {
					if kind == "ingress" {
						return "", errors.New("ingress response lost")
					}
					return "rtp_ingress_drained", nil
				},
				release: func() {},
			}
			resolver := rtpCleanupResolverFunc(func(context.Context, DeviceRTPResourceIdentity) (RTPCleanupRuntime, error) {
				if kind == "resolver" {
					return nil, errors.New("untrusted resolver")
				}
				return runtime, nil
			})
			if kind == "sql" {
				require.NoError(t, db.Exec(`CREATE TRIGGER disposition_sql BEFORE UPDATE ON gb_device_operation_intent WHEN json_extract(NEW.rtp_steps_json,'$.steps[0].cleanup.currentCall.outcome') IS NOT NULL BEGIN SELECT RAISE(FAIL,'fixture SQL failure'); END`).Error)
			}
			require.Error(t, w.Run(ctx, resolver))
			d, err := w.BatchDisposition(ctx)
			require.NoError(t, err)
			if kind == "resolver" || kind == "ingress" {
				require.Equal(t, RTPRecoveryReadyToQuiesce, d)
			} else {
				require.Equal(t, RTPRecoveryRetrySameOwner, d)
				if kind == "sql" {
					require.NoError(t, db.Exec("DROP TRIGGER disposition_sql").Error)
				}
				require.NoError(t, w.Run(ctx, resolver))
				d, err = w.BatchDisposition(ctx)
				require.NoError(t, err)
				require.Equal(t, RTPRecoveryReadyToQuiesce, d)
			}
			require.NoError(t, w.Quiesce(ctx))
		})
	}
}

package playauth

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const operationBarrierDevice = transferDevice

func newDeviceOperationBarrierFixture(t *testing.T) (*revocationFixture, *DeviceOperationBarrier) {
	t.Helper()
	fixture := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, fixture)
	require.NoError(t, fixture.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER DEFAULT 1").Error)
	require.NoError(t, fixture.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	store := NewDeviceSecurityStore(fixture.db)
	return fixture, newDeviceOperationBarrier(store, intentFixtureAuthority{})
}

func setOperationBarrierState(t *testing.T, fixture *revocationFixture, updates map[string]any) {
	t.Helper()
	require.NoError(t, fixture.db.Table("gb_device").Where("id = ?", 1).Updates(updates).Error)
}

func TestDeviceOperationBarrierBeginEpochRequiresStableOriginalEpoch(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "pending", value: int64(0)},
		{name: "null", value: nil},
		{name: "ahead", value: int64(2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, barrier := newDeviceOperationBarrierFixture(t)
			setOperationBarrierState(t, fixture, map[string]any{"cleanup_completed_epoch": test.value})
			_, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
			require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
		})
	}

	fixture, barrier := newDeviceOperationBarrierFixture(t)
	lease, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), lease.OperationEpoch())
	lease.Release()

	setOperationBarrierState(t, fixture, map[string]any{"access_epoch": 2, "cleanup_completed_epoch": 1})
	_, err = barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
	_, err = barrier.BeginEpoch(context.Background(), operationBarrierDevice, 2)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable)

	setOperationBarrierState(t, fixture, map[string]any{"cleanup_completed_epoch": 2})
	lease, err = barrier.BeginEpoch(context.Background(), operationBarrierDevice, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), lease.OperationEpoch())
	lease.Release()
	var row struct{ AccessEpoch, CleanupCompletedEpoch int64 }
	require.NoError(t, fixture.db.Table("gb_device").Select("access_epoch, cleanup_completed_epoch").Where("id = 1").Scan(&row).Error)
	require.Equal(t, int64(2), row.AccessEpoch)
	require.Equal(t, int64(2), row.CleanupCompletedEpoch)
}

func TestDeviceOperationBarrierAuthorizeEpochIsSideEffectFreePreflight(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	require.NoError(t, barrier.AuthorizeEpoch(context.Background(), operationBarrierDevice, 1))

	setOperationBarrierState(t, fixture, map[string]any{"cleanup_completed_epoch": int64(0)})
	require.ErrorIs(t, barrier.AuthorizeEpoch(context.Background(), operationBarrierDevice, 1), ErrDeviceOperationUnavailable)
	setOperationBarrierState(t, fixture, map[string]any{"cleanup_completed_epoch": int64(1)})
	setOperationBarrierState(t, fixture, map[string]any{"access_epoch": int64(2)})
	require.ErrorIs(t, barrier.AuthorizeEpoch(context.Background(), operationBarrierDevice, 1), ErrTokenRevoked)

	barrier.mu.Lock()
	require.Empty(t, barrier.lanes, "preflight must not acquire a PK lane")
	barrier.mu.Unlock()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, barrier.AuthorizeEpoch(canceled, operationBarrierDevice, 1), context.Canceled)
	require.ErrorIs(t, barrier.AuthorizeEpoch(nil, operationBarrierDevice, 1), ErrDeviceOperationUnavailable)
	require.ErrorIs(t, (*DeviceOperationBarrier)(nil).AuthorizeEpoch(context.Background(), operationBarrierDevice, 1), ErrDeviceOperationUnavailable)
}

func TestDeviceOperationBarrierTracksContinuousEpochsWithoutUpgrade(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	for epoch := int64(1); epoch <= 3; epoch++ {
		lease, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, epoch)
		require.NoError(t, err)
		require.Equal(t, epoch, lease.OperationEpoch())
		lease.Release()
		if epoch == 3 {
			continue
		}

		next := epoch + 1
		setOperationBarrierState(t, fixture, map[string]any{"access_epoch": next, "cleanup_completed_epoch": epoch})
		_, err = barrier.BeginEpoch(context.Background(), operationBarrierDevice, epoch)
		require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
		_, err = barrier.BeginEpoch(context.Background(), operationBarrierDevice, next)
		require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
		setOperationBarrierState(t, fixture, map[string]any{"cleanup_completed_epoch": next})
	}
}

func TestAuthorizationServiceBeginQueuedOperationUsesTrustedV2IATAndV4Epoch(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	service, queued := newOperationBarrierAuthorizationService(t, fixture, barrier, 0, "operation-v2")
	lease, err := service.BeginQueuedOperation(context.Background(), queued)
	require.NoError(t, err)
	require.Equal(t, int64(1), lease.OperationEpoch())
	lease.Release()

	setOperationBarrierState(t, fixture, map[string]any{"legacy_revoked_before": fixture.clock.Truncate(time.Second).Add(time.Second)})
	_, err = service.BeginQueuedOperation(context.Background(), queued)
	require.ErrorIs(t, err, ErrTokenRevoked)

	setOperationBarrierState(t, fixture, map[string]any{"legacy_revoked_before": nil})
	setOperationBarrierState(t, fixture, map[string]any{"access_epoch": 2, "cleanup_completed_epoch": 2})
	service, queued = newOperationBarrierAuthorizationService(t, fixture, barrier, 1, "operation-v4-old")
	_, err = service.BeginQueuedOperation(context.Background(), queued)
	require.ErrorIs(t, err, ErrTokenRevoked)

	service, queued = newOperationBarrierAuthorizationService(t, fixture, barrier, 2, "operation-v4-current")
	lease, err = service.BeginQueuedOperation(context.Background(), queued)
	require.NoError(t, err)
	require.Equal(t, int64(2), lease.OperationEpoch())
	lease.Release()
}

func TestAuthorizationServiceBeginQueuedOperationRechecksRegistryAfterAdmission(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *AuthorizationRegistry, string)
		err    error
	}{
		{
			name: "terminal",
			mutate: func(t *testing.T, registry *AuthorizationRegistry, generation string) {
				require.NoError(t, registry.BindAuthorization(generation, 77))
				require.Equal(t, 1, registry.TerminateMediaGeneration(77))
			},
			err: ErrAuthorizationTerminal,
		},
		{
			name: "replacement",
			mutate: func(_ *testing.T, registry *AuthorizationRegistry, generation string) {
				registry.mu.Lock()
				record := registry.records[generation]
				record.nonce = "replacement"
				registry.records[generation] = record
				registry.mu.Unlock()
			},
			err: ErrAuthorizationClaimsMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, barrier := newDeviceOperationBarrierFixture(t)
			service, queued := newOperationBarrierAuthorizationService(t, fixture, barrier, 1, "post-admission-"+test.name)
			guard, err := barrier.LockTransfer(context.Background(), 1)
			require.NoError(t, err)

			resolved := make(chan struct{})
			var resolveOnce sync.Once
			callbackName := "device_operation_barrier_test_signal_resolve_" + test.name
			require.NoError(t, fixture.db.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
				if db.Statement.Table == "gb_device" && len(db.Statement.Selects) == 1 && db.Statement.Selects[0] == "id" {
					resolveOnce.Do(func() { close(resolved) })
				}
			}))
			t.Cleanup(func() { _ = fixture.db.Callback().Query().Remove(callbackName) })

			type outcome struct {
				lease DeviceOperationLease
				err   error
			}
			result := make(chan outcome, 1)
			go func() {
				lease, err := service.BeginQueuedOperation(context.Background(), queued)
				result <- outcome{lease: lease, err: err}
			}()
			select {
			case <-resolved:
			case <-time.After(time.Second):
				t.Fatal("queued operation did not reach PK resolution")
			}
			test.mutate(t, service.registry, queued.AuthorizationGeneration)
			guard.Release()

			got := <-result
			require.ErrorIs(t, got.err, test.err)
			require.Nil(t, got.lease)
			barrier.mu.Lock()
			require.Empty(t, barrier.lanes, "post-admission rejection must release the owner lease")
			barrier.mu.Unlock()
		})
	}
}

func TestDeviceOperationBarrierCommitCancelsOldLeasesButReleaseDoesNot(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	first, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)

	rollback, err := barrier.LockTransfer(context.Background(), 1)
	require.NoError(t, err)
	rollback.Release()
	require.NoError(t, first.Context().Err(), "rollback must not cancel an active operation")

	guard, err := barrier.LockTransfer(context.Background(), 1)
	require.NoError(t, err)
	blocked, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err = barrier.BeginEpoch(blocked, operationBarrierDevice, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	guard.Commit(2)
	require.ErrorIs(t, context.Cause(first.Context()), ErrDeviceOperationRevoked)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer waitCancel()
	require.ErrorIs(t, barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	guard.Release()

	first.Release()
	first.Release()
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2))
	setOperationBarrierState(t, fixture, map[string]any{"access_epoch": 2, "cleanup_completed_epoch": 2})
	second, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 2)
	require.NoError(t, err)
	second.Release()
}

func TestDeviceOperationBarrierCallerCancelDoesNotCancelAnotherLeaseAndReclaimsLane(t *testing.T) {
	_, barrier := newDeviceOperationBarrierFixture(t)
	firstCtx, firstCancel := context.WithCancel(context.Background())
	first, err := barrier.BeginEpoch(firstCtx, operationBarrierDevice, 1)
	require.NoError(t, err)
	firstCancel()
	require.ErrorIs(t, first.Context().Err(), context.Canceled)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	require.ErrorIs(t, barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	waitCancel()

	second, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)
	first.Release()
	second.Release()

	barrier.mu.Lock()
	_, exists := barrier.lanes[1]
	barrier.mu.Unlock()
	require.False(t, exists, "released leases must allow the PK lane to be reclaimed")
}

func TestDeviceOperationBarrierAdmissionGateDoesNotHoldLaneMutexDuringSQL(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var signalOnce sync.Once
	callbackName := "device_operation_barrier_test_block_resolve"
	require.NoError(t, fixture.db.Callback().Query().Before("gorm:query").Register(callbackName, func(db *gorm.DB) {
		if db.Statement.Table != "gb_device" || len(db.Statement.Selects) != 1 || !strings.Contains(db.Statement.Selects[0], "cleanup_completed_epoch") {
			return
		}
		signalOnce.Do(func() { close(queryStarted) })
		select {
		case <-releaseQuery:
		case <-db.Statement.Context.Done():
		}
	}))
	t.Cleanup(func() { _ = fixture.db.Callback().Query().Remove(callbackName) })

	type outcome struct {
		lease DeviceOperationLease
		err   error
	}
	firstResult := make(chan outcome, 1)
	go func() {
		lease, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
		firstResult <- outcome{lease: lease, err: err}
	}()
	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("first admission did not reach the controlled SQL block")
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	secondResult := make(chan error, 1)
	go func() {
		guard, err := barrier.LockTransfer(secondCtx, 1)
		if guard != nil {
			guard.Release()
		}
		secondResult <- err
	}()

	var secondErr error
	timely := true
	select {
	case secondErr = <-secondResult:
	case <-time.After(250 * time.Millisecond):
		timely = false
	}
	secondCancel()
	close(releaseQuery)
	first := <-firstResult
	if first.lease != nil {
		first.lease.Release()
	}
	if !timely {
		if secondErr == nil {
			secondErr = context.DeadlineExceeded
		}
		t.Fatalf("LockTransfer remained blocked by admission SQL, err=%v", secondErr)
	}
	require.ErrorIs(t, secondErr, context.DeadlineExceeded)
}

func TestDeviceOperationBarrierConcurrentLeaseReleaseIsIdempotent(t *testing.T) {
	_, barrier := newDeviceOperationBarrierFixture(t)
	const workers = 8
	leases := make([]DeviceOperationLease, workers)
	for i := range leases {
		lease, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
		require.NoError(t, err)
		leases[i] = lease
	}
	var wg sync.WaitGroup
	for _, lease := range leases {
		wg.Add(1)
		go func(lease DeviceOperationLease) {
			defer wg.Done()
			lease.Release()
			lease.Release()
		}(lease)
	}
	wg.Wait()
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2))
	barrier.mu.Lock()
	_, exists := barrier.lanes[1]
	barrier.mu.Unlock()
	require.False(t, exists)
}

func newOperationBarrierAuthorizationService(t *testing.T, fixture *revocationFixture, barrier *DeviceOperationBarrier, epoch int64, generation string) (*AuthorizationService, QueuedAuthorization) {
	t.Helper()
	now := fixture.clock
	signer, err := NewSigner([]byte(testRootSecret), WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	registry := NewAuthorizationRegistry(WithAuthorizationRegistryNow(func() time.Time { return now }))
	store := NewDeviceSecurityStore(fixture.db)
	service := NewAuthorizationService(signer, registry, WithDeviceSecurityAuthority(store), WithDeviceOperationBarrier(barrier))
	binding := Binding{
		DeviceID: operationBarrierDevice, ChannelID: "34020000001310000001", DeviceEpoch: epoch,
		App: "rtp", Stream: "34020000001320000001_34020000001310000001", MediaServerID: "node-a",
	}
	prepared := Prepared{IssuedAt: now, ExpiresAt: now.Add(2 * time.Minute), Nonce: "nonce-" + generation, AuthorizationGeneration: generation}
	require.NoError(t, registry.Register(prepared, binding))
	return service, QueuedAuthorization{
		AuthorizationGeneration: generation, DeviceID: binding.DeviceID, ChannelID: binding.ChannelID,
		DeviceEpoch: epoch, App: binding.App, Stream: binding.Stream, MediaServerID: binding.MediaServerID,
	}
}

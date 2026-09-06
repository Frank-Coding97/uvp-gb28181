package playauth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const operationBarrierDevice = transferDevice

func newDeviceOperationBarrierFixture(t *testing.T) (*revocationFixture, *DeviceOperationBarrier) {
	t.Helper()
	fixture := newOpenAPIRevocationFixture(t)
	prepareTransferDevice(t, fixture)
	require.NoError(t, fixture.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch INTEGER DEFAULT 1").Error)
	require.NoError(t, fixture.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	store := NewDeviceSecurityStore(fixture.db)
	return fixture, NewDeviceOperationBarrier(store)
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
	require.NoError(t, first.Context().Err())

	second, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)
	first.Release()
	second.Release()

	barrier.mu.Lock()
	_, exists := barrier.lanes[1]
	barrier.mu.Unlock()
	require.False(t, exists, "released leases must allow the PK lane to be reclaimed")
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

package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceOperationChildCannotReleaseParent(t *testing.T) {
	_, barrier := newDeviceOperationBarrierFixture(t)
	parent, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)
	t.Cleanup(parent.Release)
	id := DeviceOperationIntentIdentity{DevicePK: 1, DeviceCode: operationBarrierDevice, DeviceEpoch: 1}
	child, err := barrier.BorrowChildLease(context.Background(), parent, id)
	require.NoError(t, err)
	require.Equal(t, int64(1), child.OperationEpoch())
	child.Release()
	require.ErrorIs(t, child.Context().Err(), context.Canceled)
	require.NoError(t, parent.Context().Err())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, barrier.WaitBefore(ctx, 1, 2), context.DeadlineExceeded)
	parent.Release()
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2))
}

func TestDeviceOperationChildRejectsForeignOrEndedParent(t *testing.T) {
	fixture, barrier := newDeviceOperationBarrierFixture(t)
	parent, err := barrier.BeginEpoch(context.Background(), operationBarrierDevice, 1)
	require.NoError(t, err)
	t.Cleanup(parent.Release)
	id := DeviceOperationIntentIdentity{DevicePK: 1, DeviceCode: operationBarrierDevice, DeviceEpoch: 1}
	foreign := NewDeviceOperationBarrier(NewDeviceSecurityStore(fixture.db))
	_, err = foreign.BorrowChildLease(context.Background(), parent, id)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
	for _, wrong := range []DeviceOperationIntentIdentity{
		{DevicePK: 2, DeviceCode: id.DeviceCode, DeviceEpoch: 1},
		{DevicePK: 1, DeviceCode: "34020000002000009999", DeviceEpoch: 1},
		{DevicePK: 1, DeviceCode: id.DeviceCode, DeviceEpoch: 2},
	} {
		_, err = barrier.BorrowChildLease(context.Background(), parent, wrong)
		require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
	}
	child, err := barrier.BorrowChildLease(context.Background(), parent, id)
	require.NoError(t, err)
	t.Cleanup(child.Release)
	_, err = barrier.BorrowChildLease(context.Background(), child, id)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable, "a borrowed child cannot become a second parent")
	guard, err := barrier.LockTransfer(context.Background(), 1)
	require.NoError(t, err)
	guard.Commit(2)
	guard.Release()
	require.Eventually(t, func() bool { return child.Context().Err() != nil }, time.Second, time.Millisecond)
	_, err = barrier.BorrowChildLease(context.Background(), parent, id)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
	parent.Release()
	_, err = barrier.BorrowChildLease(context.Background(), parent, id)
	require.ErrorIs(t, err, ErrDeviceOperationUnavailable)
}

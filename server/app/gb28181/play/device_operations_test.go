package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type coordinatorOperationLease struct {
	ctx     context.Context
	release func()
}

func TestOpenAPIPlayConfiguredNilOperationBarrierFailsClosed(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, false, false)
	z := &mockZLM{openErr: errors.New("must not reach media")}
	s, _, _ := newFixedSvc(t, z, &mockInviter{}, onlineDevice(), aChannel())
	WithDeviceOperationBarrier(nil)(s)
	result, err := s.EnsureLive(context.Background(), Request{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1})
	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.Nil(t, result)
	require.Zero(t, z.openCalls.Load())
}

func (l *coordinatorOperationLease) Context() context.Context { return l.ctx }
func (l *coordinatorOperationLease) OperationEpoch() int64    { return 1 }
func (l *coordinatorOperationLease) Release()                 { l.release() }

func TestOpenAPICoordinatorDeviceBarrierDeniesBeforeStart(t *testing.T) {
	var calls atomic.Int32
	c := NewCoordinator(func(context.Context, Request) (*Result, error) {
		calls.Add(1)
		return &Result{StreamID: "must-not-start"}, nil
	})
	c.beginOperation = func(context.Context, Request) (playauth.DeviceOperationLease, error) {
		return nil, playauth.ErrDeviceSecurityUnavailable
	}
	result, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel"))
	require.ErrorIs(t, err, playauth.ErrDeviceSecurityUnavailable)
	require.Nil(t, result)
	require.Zero(t, calls.Load())
}

func TestOpenAPIEnsureLiveRechecksDeviceAfterLateStartWithoutStoppingSharedGeneration(t *testing.T) {
	db := newDeviceOperationSQLFixture(t)
	var stops atomic.Int32
	s := &Service{operationBarrier: playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))}
	s.liveCoordinator = NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
		return &Result{StreamID: "shared-late-result", Generation: 1}, nil
	}, func(context.Context, *Result) error { stops.Add(1); return nil })
	result, err := s.EnsureLive(context.Background(), Request{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1})
	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.Nil(t, result)
	require.Zero(t, stops.Load(), "caller denial does not own shared-generation cleanup")
}

func TestOpenAPICoordinatorDeviceCancelSurvivesCallerIsolationUntilCompensation(t *testing.T) {
	started := make(chan context.Context, 1)
	canceled := make(chan struct{})
	compensated := make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(compensated) }) }
	defer finish()
	released := make(chan bool, 1)
	var begins atomic.Int32
	var deviceCancel context.CancelFunc
	c := NewCoordinator(func(ctx context.Context, _ Request) (*Result, error) {
		started <- ctx
		<-ctx.Done()
		close(canceled)
		<-compensated // Model a non-instantaneous network failure compensation.
		return nil, ctx.Err()
	})
	c.beginOperation = func(ctx context.Context, _ Request) (playauth.DeviceOperationLease, error) {
		begins.Add(1)
		leaseCtx, cancel := context.WithCancel(ctx)
		deviceCancel = cancel
		return &coordinatorOperationLease{ctx: leaseCtx, release: func() {
			c.mu.Lock()
			_, exists := c.entries[coordinatorKey{deviceID: "device", channelID: "channel"}]
			c.mu.Unlock()
			released <- !exists // Release must follow entry publication, not just cancellation.
		}}, nil
	}
	ownerCtx, ownerCancel := context.WithTimeout(context.Background(), time.Second)
	defer ownerCancel()
	ownerDone := make(chan error, 1)
	go func() {
		_, err := c.EnsureLive(ownerCtx, coordinatorRequest("device", "channel"))
		ownerDone <- err
	}()
	var startCtx context.Context
	select {
	case startCtx = <-started:
	case <-time.After(time.Second):
		t.Fatal("owner did not start")
	}
	ownerCancel()
	require.NoError(t, startCtx.Err(), "owner caller cancellation must not terminate shared start")
	waiterCtx, waiterCancel := context.WithCancel(context.Background())
	waiterCancel()
	_, err := c.EnsureLive(waiterCtx, coordinatorRequest("device", "channel"))
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, begins.Load(), "waiter must not own a second operation lease")
	require.NotNil(t, deviceCancel)
	deviceCancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("device cancellation was lost")
	}
	select {
	case <-released:
		t.Fatal("operation released before compensation")
	default:
	}
	finish()
	require.True(t, <-released, "operation released before publishing coordinator terminal state")
	require.True(t, errors.Is(<-ownerDone, context.Canceled))
}

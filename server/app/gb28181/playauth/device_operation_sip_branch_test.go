package playauth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sipKnownBranch() DeviceSIPKnownBranchIdentity {
	i := sipStepIdentity(1)
	return DeviceSIPKnownBranchIdentity{InviteStepID: i.StepID, CallID: i.CallID, LocalTag: i.LocalTag,
		RemoteTag: "remote-one", CSeq: i.CSeq, StatusCode: 200, RemoteTarget: "sip:device@127.0.0.1:5062",
		RouteSet: []string{"sip:proxy-one.example:5060;lr", "sip:proxy-two.example:5060;lr;transport=tcp"}}
}

func dispatchedSIPBranchFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity) {
	t.Helper()
	f, store, id := newSIPStepFixture(t)
	_, err := store.AddSIPInviteStep(context.Background(), id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	_, err = store.DispatchSIPInviteStep(context.Background(), id, 3, sipStepIdentity(1).StepID)
	require.NoError(t, err)
	return f, store, id
}

func TestDeviceSIPKnownBranchPersistentAndACKCAS(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx, identity := context.Background(), sipKnownBranch()
	observed, err := store.ObserveSIPKnownBranch(ctx, id, 4, identity)
	require.NoError(t, err)
	require.EqualValues(t, 5, observed.Intent.RowVersion)
	require.Equal(t, identity, observed.Steps[0].KnownBranch.Identity)
	require.Equal(t, SIPStepPrepared, observed.Steps[0].KnownBranch.ACKState)
	duplicate, err := store.ObserveSIPKnownBranch(ctx, id, 5, identity)
	require.NoError(t, err)
	require.Equal(t, observed, duplicate)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, observed, loaded)
	for _, value := range []any{loaded, loaded.Steps[0].KnownBranch, identity} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(encoded))
	}
	var wg sync.WaitGroup
	var winners atomic.Int32
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchSIPKnownBranchACK(ctx, id, 5, identity)
			if err == nil {
				winners.Add(1)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	require.EqualValues(t, 1, winners.Load())
	loaded, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, SIPStepMayHaveDispatched, loaded.Steps[0].KnownBranch.ACKState)
	require.NotNil(t, loaded.Steps[0].KnownBranch.ACKDispatchStartedAt)
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 6, identity)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "load does not restore ACK permission")
	_, err = store.ObserveSIPKnownBranch(ctx, id, 6, identity)
	require.NoError(t, err, "observing after ACK remains observation only")
}

func TestDeviceSIPKnownBranchRejectsReplacementAndWrongInvite(t *testing.T) {
	_, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	_, err := store.ObserveSIPKnownBranch(ctx, id, 4, sipKnownBranch())
	require.NoError(t, err)
	for _, change := range []func(*DeviceSIPKnownBranchIdentity){
		func(i *DeviceSIPKnownBranchIdentity) { i.InviteStepID = sipStepIdentity(2).StepID },
		func(i *DeviceSIPKnownBranchIdentity) { i.CallID += "-other" },
		func(i *DeviceSIPKnownBranchIdentity) { i.LocalTag += "-other" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTag += "-fork" },
		func(i *DeviceSIPKnownBranchIdentity) { i.CSeq++ },
		func(i *DeviceSIPKnownBranchIdentity) { i.StatusCode = 202 },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTarget += ";transport=tcp" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0], i.RouteSet[1] = i.RouteSet[1], i.RouteSet[0] },
	} {
		i := sipKnownBranch()
		change(&i)
		_, err = store.ObserveSIPKnownBranch(ctx, id, 5, i)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
		_, err = store.DispatchSIPKnownBranchACK(ctx, id, 5, i)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	_, fresh, parent := newSIPStepFixture(t)
	_, err = fresh.AddSIPInviteStep(ctx, parent, 2, sipStepIdentity(1))
	require.NoError(t, err)
	_, err = fresh.ObserveSIPKnownBranch(ctx, parent, 3, sipKnownBranch())
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "prepared INVITE is not a dispatched transaction")
}

func TestDeviceSIPKnownBranchLateObservationCannotReauthorize(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	observed, err := store.ObserveSIPKnownBranch(ctx, id, 4, sipKnownBranch())
	require.NoError(t, err, "retain late response facts after transfer")
	require.EqualValues(t, 1, observed.Intent.DeviceEpoch)
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 5, sipKnownBranch())
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
}

func TestDeviceSIPKnownBranchCommitUnknownReturnsEmpty(t *testing.T) {
	for _, operation := range []string{"observe", "ack"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", operation, committed), func(t *testing.T) {
				f, store, id := dispatchedSIPBranchFixture(t)
				ctx, version := context.Background(), int64(4)
				if operation == "ack" {
					_, err := store.ObserveSIPKnownBranch(ctx, id, version, sipKnownBranch())
					require.NoError(t, err)
					version++
				}
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				fault := newIntentFixtureStore(faultDB)
				mutate := fault.ObserveSIPKnownBranch
				if operation == "ack" {
					mutate = fault.DispatchSIPKnownBranchACK
				}
				out, err := mutate(ctx, id, version, sipKnownBranch())
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out.Intent.OperationID)
				require.Empty(t, out.Steps)
				loaded, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				if committed && operation == "ack" {
					require.Equal(t, SIPStepMayHaveDispatched, loaded.Steps[0].KnownBranch.ACKState)
					_, err = store.DispatchSIPKnownBranchACK(ctx, id, version+1, sipKnownBranch())
					require.ErrorIs(t, err, ErrDeviceIntentConflict)
				}
			})
		}
	}
}

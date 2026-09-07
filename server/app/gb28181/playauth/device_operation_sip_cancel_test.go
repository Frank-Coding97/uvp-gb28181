package playauth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sipCancelIdentity() DeviceSIPCancelIdentity {
	i := DeviceSIPCancelIdentity(sipStepIdentity(1))
	i.ContentType, i.BodyLength, i.BodySHA256 = "", 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	return i
}

func TestDeviceSIPCancelPersistentFixedIdentityAndUniqueDispatch(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx, identity := context.Background(), sipCancelIdentity()
	prepared, err := store.PrepareSIPCancel(ctx, id, 4, identity)
	require.NoError(t, err)
	require.EqualValues(t, 5, prepared.Intent.RowVersion)
	require.Equal(t, identity, prepared.Steps[0].Cancel.Identity)
	require.Equal(t, SIPStepPrepared, prepared.Steps[0].Cancel.State)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, prepared, loaded)
	duplicate, err := store.PrepareSIPCancel(ctx, id, 5, identity)
	require.NoError(t, err)
	require.Equal(t, prepared, duplicate)
	for _, value := range []any{loaded.Steps[0].Cancel, identity} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body))
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchSIPCancel(ctx, id, 5, identity)
			if err == nil {
				wins.Add(1)
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
	require.EqualValues(t, 1, wins.Load())
	loaded, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, SIPStepMayHaveDispatched, loaded.Steps[0].Cancel.State)
	require.NotNil(t, loaded.Steps[0].Cancel.DispatchStartedAt)
	_, err = store.DispatchSIPCancel(ctx, id, 6, identity)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "readback cannot restore dispatch permission")
}

func TestDeviceSIPCancelRejectsChangedOriginalMaterial(t *testing.T) {
	_, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	for _, change := range []func(*DeviceSIPCancelIdentity){
		func(i *DeviceSIPCancelIdentity) { i.StepID = sipStepIdentity(2).StepID },
		func(i *DeviceSIPCancelIdentity) { i.CallID += "-other" },
		func(i *DeviceSIPCancelIdentity) { i.CSeq++ },
		func(i *DeviceSIPCancelIdentity) { i.LocalTag += "-other" },
		func(i *DeviceSIPCancelIdentity) { i.RequestURI = "sip:other@127.0.0.1" },
		func(i *DeviceSIPCancelIdentity) { i.ToURI = "sip:other@127.0.0.1" },
		func(i *DeviceSIPCancelIdentity) { i.FromURI = "sip:other@127.0.0.1" },
		func(i *DeviceSIPCancelIdentity) { i.ContactURI = "sip:other@127.0.0.1:5060" },
		func(i *DeviceSIPCancelIdentity) { i.Destination = "127.0.0.1:9999" },
		func(i *DeviceSIPCancelIdentity) { i.Branch += "-other" },
		func(i *DeviceSIPCancelIdentity) { i.ViaPort++ },
		func(i *DeviceSIPCancelIdentity) { i.ViaHost = "127.0.0.2" },
		func(i *DeviceSIPCancelIdentity) { i.ViaTransport = "TCP" },
		func(i *DeviceSIPCancelIdentity) { i.RPortPresent = true },
		func(i *DeviceSIPCancelIdentity) { i.RPortValue = "5060" },
		func(i *DeviceSIPCancelIdentity) { i.MaxForwards++ },
		func(i *DeviceSIPCancelIdentity) { i.Transport = "TCP" },
		func(i *DeviceSIPCancelIdentity) { i.ContentType = "application/sdp" },
		func(i *DeviceSIPCancelIdentity) { i.BodyLength = 1 },
		func(i *DeviceSIPCancelIdentity) { i.BodySHA256 = sipStepIdentity(1).BodySHA256 },
	} {
		identity := sipCancelIdentity()
		change(&identity)
		_, err := store.PrepareSIPCancel(ctx, id, 4, identity)
		require.Error(t, err)
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].Cancel)
	_, err = store.DispatchSIPCancel(ctx, id, 4, sipCancelIdentity())
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "no prepared CANCEL")
}

func TestDeviceSIPCancelCommitUnknownReturnsNoPermit(t *testing.T) {
	for _, operation := range []string{"prepare", "dispatch"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", operation, committed), func(t *testing.T) {
				f, store, id := dispatchedSIPBranchFixture(t)
				ctx, version := context.Background(), int64(4)
				if operation == "dispatch" {
					_, err := store.PrepareSIPCancel(ctx, id, version, sipCancelIdentity())
					require.NoError(t, err)
					version++
				}
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				fault := NewDeviceOperationIntentStore(faultDB)
				mutate := fault.PrepareSIPCancel
				if operation == "dispatch" {
					mutate = fault.DispatchSIPCancel
				}
				out, err := mutate(ctx, id, version, sipCancelIdentity())
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out.Intent.OperationID)
				require.Empty(t, out.Steps)
				if committed && operation == "dispatch" {
					_, err = store.DispatchSIPCancel(ctx, id, version+1, sipCancelIdentity())
					require.ErrorIs(t, err, ErrDeviceIntentConflict)
				}
			})
		}
	}
}

func TestDeviceSIPCancelCleanupGate(t *testing.T) {
	for _, dispatch := range []bool{false, true} {
		for _, tc := range []struct {
			name              string
			access, completed int64
			change            func(*DeviceOperationIntentIdentity)
			allowed           bool
		}{
			{name: "fresh", access: 1, completed: 1, allowed: true},
			{name: "transfer", access: 2, completed: 1, allowed: true},
			{name: "repeated-transfer", access: 3, completed: 1, allowed: true},
			{name: "covered", access: 3, completed: 2},
			{name: "caught-up", access: 2, completed: 2},
			{name: "wrong-pk", access: 2, completed: 1, change: func(id *DeviceOperationIntentIdentity) { id.DevicePK++ }},
			{name: "wrong-code", access: 2, completed: 1, change: func(id *DeviceOperationIntentIdentity) { id.DeviceCode = "34020000001320000009" }},
			{name: "future-intent", access: 1, completed: 1, change: func(id *DeviceOperationIntentIdentity) { id.DeviceEpoch = 2 }},
			{name: "changed-target", access: 2, completed: 1, change: func(id *DeviceOperationIntentIdentity) { id.TargetPK++ }},
		} {
			t.Run(fmt.Sprintf("dispatch=%t/%s", dispatch, tc.name), func(t *testing.T) {
				f, store, id := dispatchedSIPBranchFixture(t)
				ctx, version := context.Background(), int64(4)
				if dispatch {
					_, err := store.PrepareSIPCancel(ctx, id, version, sipCancelIdentity())
					require.NoError(t, err)
					version++
				}
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=?, cleanup_completed_epoch=? WHERE id=1", tc.access, tc.completed).Error)
				original := id
				if tc.change != nil {
					tc.change(&id)
				}
				mutate := store.PrepareSIPCancel
				if dispatch {
					mutate = store.DispatchSIPCancel
				}
				out, err := mutate(ctx, id, version, sipCancelIdentity())
				if tc.allowed {
					require.NoError(t, err)
					require.EqualValues(t, version+1, out.Intent.RowVersion)
				} else {
					require.Error(t, err)
					require.Empty(t, out)
					loaded, err := store.LoadSIPInviteSteps(ctx, original)
					require.NoError(t, err)
					require.EqualValues(t, version, loaded.Intent.RowVersion)
				}
				state, err := NewDeviceCleanupStore(f.db).Load(ctx, original.DeviceCode)
				require.NoError(t, err)
				require.EqualValues(t, tc.completed, state.CleanupCompletedEpoch, "CANCEL never completes cleanup")
			})
		}
	}
}

func TestDeviceSIPCancelOldEpochCannotRestoreBusiness(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err := store.PrepareSIPCancel(ctx, id, 4, sipCancelIdentity())
	require.NoError(t, err)
	// Reconstructing a store permits an explicit prepared-only cleanup CAS, not
	// re-authorization or automatic dispatch from a loaded state.
	restarted := NewDeviceOperationIntentStore(f.db)
	_, err = restarted.DispatchSIPCancel(ctx, id, 5, sipCancelIdentity())
	require.NoError(t, err)
	_, err = restarted.DispatchSIPCancel(ctx, id, 6, sipCancelIdentity())
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	_, err = store.AddSIPInviteStep(ctx, id, 6, sipStepIdentity(2))
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	_, err = store.DispatchSIPInviteStep(ctx, id, 6, sipStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	_, err = store.ObserveSIPKnownBranch(ctx, id, 6, sipKnownBranch())
	require.NoError(t, err, "a successful CANCEL CAS does not erase a late 2xx")
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 7, sipKnownBranch())
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	loaded, err := restarted.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].KnownBranch)
	require.Equal(t, SIPStepMayHaveDispatched, loaded.Steps[0].Cancel.State)
}

func TestDeviceSIPCancelKnownBranchPreventsNewCancel(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			_, store, id := dispatchedSIPBranchFixture(t)
			ctx, version := context.Background(), int64(4)
			if prepared {
				_, err := store.PrepareSIPCancel(ctx, id, version, sipCancelIdentity())
				require.NoError(t, err)
				version++
			}
			_, err := store.ObserveSIPKnownBranch(ctx, id, version, sipKnownBranch())
			require.NoError(t, err)
			version++
			_, err = store.DispatchSIPCancel(ctx, id, version, sipCancelIdentity())
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
			_, err = store.PrepareSIPCancel(ctx, id, version, sipCancelIdentity())
			if prepared {
				require.NoError(t, err, "existing material is observation, not a permit")
			} else {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
		})
	}
}

func TestDeviceSIPCancelPreparedInviteCannotCancel(t *testing.T) {
	_, store, id := newSIPStepFixture(t)
	_, err := store.AddSIPInviteStep(context.Background(), id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	_, err = store.PrepareSIPCancel(context.Background(), id, 3, sipCancelIdentity())
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
}

func TestDeviceSIPCancelRejectsCorruptStorage(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	_, err := store.PrepareSIPCancel(ctx, id, 4, sipCancelIdentity())
	require.NoError(t, err)
	_, err = store.DispatchSIPCancel(ctx, id, 5, sipCancelIdentity())
	require.NoError(t, err)
	_, err = store.ObserveSIPKnownBranch(ctx, id, 6, sipKnownBranch())
	require.NoError(t, err)
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	for _, change := range []func(*sipInviteStepWire){
		func(s *sipInviteStepWire) { s.Cancel.Version = 2 },
		func(s *sipInviteStepWire) { s.Cancel.Action = "bye" },
		func(s *sipInviteStepWire) { s.Cancel.State = "closed" },
		func(s *sipInviteStepWire) { s.Cancel.RowVersion = 1 },
		func(s *sipInviteStepWire) { s.Cancel.DispatchStartedAt = nil },
		func(s *sipInviteStepWire) { s.Cancel.Identity.CSeq++ },
		func(s *sipInviteStepWire) { s.Cancel.Identity.BodyLength = 1 },
		func(s *sipInviteStepWire) { s.Cancel.PreparedAt = s.PreparedAt.Add(-time.Second) },
		func(s *sipInviteStepWire) { s.Cancel.PreparedAt = s.Cancel.PreparedAt.Add(time.Nanosecond) },
		func(s *sipInviteStepWire) {
			s.Cancel.PreparedAt = s.Cancel.PreparedAt.In(time.FixedZone("invalid", 3600))
		},
		func(s *sipInviteStepWire) { s.Cancel.PreparedAt = s.KnownBranch.ObservedAt.Add(time.Microsecond) },
		func(s *sipInviteStepWire) {
			v := s.KnownBranch.ObservedAt.Add(time.Microsecond)
			s.Cancel.DispatchStartedAt = &v
		},
		func(s *sipInviteStepWire) { s.State = SIPStepPrepared; s.RowVersion = 1; s.DispatchStartedAt = nil },
	} {
		var wire sipInviteStepsWire
		require.NoError(t, json.Unmarshal([]byte(raw), &wire))
		change(&wire.Steps[0])
		body, err := json.Marshal(wire)
		require.NoError(t, err)
		require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(body)).Error)
		out, err := store.LoadSIPInviteSteps(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		require.Empty(t, out)
	}
}

func TestDeviceSIPCancelByteBudgetPreservesEvidence(t *testing.T) {
	_, store, id := newSIPStepFixture(t)
	ctx, version := context.Background(), int64(2)
	uri := "sip:" + strings.Repeat("u", 256) + "@" + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".example.com:5060"
	var last DeviceSIPInviteSteps
	for n := 1; n <= 16; n++ {
		i := sipStepIdentity(n)
		i.RequestURI, i.FromURI, i.ToURI, i.ContactURI = uri, uri, uri, uri
		out, err := store.AddSIPInviteStep(ctx, id, version, i)
		if err != nil {
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			break
		}
		version = out.Intent.RowVersion
		last, err = store.DispatchSIPInviteStep(ctx, id, version, i.StepID)
		require.NoError(t, err)
		version = last.Intent.RowVersion
	}
	require.NotEmpty(t, last.Steps)
	cancel := DeviceSIPCancelIdentity(last.Steps[0].Identity)
	cancel.ContentType, cancel.BodyLength, cancel.BodySHA256 = "", 0, sipEmptyBodySHA256
	out, err := store.PrepareSIPCancel(ctx, id, version, cancel)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable, "full ledger must not evict evidence for compensation")
	require.Empty(t, out)
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, last, loaded)
}

func TestDeviceSIPCancelTransferAndResponseRace(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	_, err := store.PrepareSIPCancel(ctx, id, 4, sipCancelIdentity())
	require.NoError(t, err)
	start, results := make(chan struct{}), make(chan error, 22)
	for n := 0; n < 22; n++ {
		go func(n int) {
			<-start
			var err error
			switch n {
			case 20:
				err = f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error
			case 21:
				_, err = store.ObserveSIPKnownBranch(ctx, id, 5, sipKnownBranch())
			default:
				_, err = store.DispatchSIPCancel(ctx, id, 5, sipCancelIdentity())
			}
			results <- err
		}(n)
	}
	close(start)
	wins := 0
	for n := 0; n < 22; n++ {
		if err := <-results; err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		}
	}
	require.Equal(t, 2, wins, "transfer plus exactly one parent CAS winner")
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	if loaded.Steps[0].KnownBranch == nil {
		loaded, err = store.ObserveSIPKnownBranch(ctx, id, loaded.Intent.RowVersion, sipKnownBranch())
		require.NoError(t, err, "CANCEL winning never prevents late response persistence")
	}
	_, err = store.DispatchSIPCancel(ctx, id, loaded.Intent.RowVersion, sipCancelIdentity())
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
}

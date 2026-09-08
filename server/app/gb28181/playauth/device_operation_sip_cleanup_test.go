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

func sipCleanupIdentity(n int) DeviceSIPCleanupAttemptIdentity {
	ack := sipStepIdentity(1)
	ack.RequestURI, ack.Destination = "sip:device@127.0.0.1:5062", "127.0.0.1:5062"
	ack.ContentType, ack.BodyLength, ack.BodySHA256 = "", 0, sipEmptyBodySHA256
	ack.Branch = fmt.Sprintf("z9hG4bK-cleanup-ack-%d", n)
	bye := ack
	bye.CSeq += uint32(n)
	bye.Branch = fmt.Sprintf("z9hG4bK-cleanup-bye-%d", n)
	return DeviceSIPCleanupAttemptIdentity{AttemptID: fmt.Sprintf("%032x", n+100),
		ACK: DeviceSIPCleanupRequestIdentity{Request: ack, RemoteTag: "remote-one", Routes: []string{}},
		BYE: DeviceSIPCleanupRequestIdentity{Request: bye, RemoteTag: "remote-one", Routes: []string{}}}
}

func sipCleanupFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity) {
	t.Helper()
	f, store, id := dispatchedSIPBranchFixture(t)
	branch := sipKnownBranch()
	branch.RouteSet = []string{}
	_, err := store.ObserveSIPKnownBranch(context.Background(), id, 4, branch)
	require.NoError(t, err)
	return f, store, id
}

func TestDeviceSIPCleanupStagesPreserveBusinessAuthority(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx, identity := context.Background(), sipCleanupIdentity(1)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	prepared, err := store.PrepareSIPBranchCleanup(ctx, id, 5, identity)
	require.NoError(t, err)
	a := prepared.Steps[0].KnownBranch.CleanupAttempts[0]
	require.Equal(t, SIPCleanupPrepared, a.State)
	require.NotEmpty(t, a.OwnerRunID)
	_, err = store.DispatchSIPCleanupBYE(ctx, id, 6, identity.AttemptID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "no BYE before the dedicated cleanup ACK stage")
	_, err = store.DispatchSIPCleanupACK(ctx, id, 6, identity.AttemptID)
	require.NoError(t, err)
	_, err = store.DispatchSIPCleanupACK(ctx, id, 7, identity.AttemptID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	// The later concrete owner must retain successful ACK Write before invoking
	// this store CAS. The store itself performs no network or result inference.
	_, err = store.DispatchSIPCleanupBYE(ctx, id, 7, identity.AttemptID)
	require.NoError(t, err)
	response := DeviceSIPCleanupBYEResponse{AttemptID: identity.AttemptID, CallID: identity.BYE.Request.CallID,
		CSeq: identity.BYE.Request.CSeq, LocalTag: identity.BYE.Request.LocalTag, RemoteTag: identity.BYE.RemoteTag, StatusCode: 200}
	out, err := store.ObserveSIPCleanupBYE(ctx, id, 8, response)
	require.NoError(t, err)
	require.Equal(t, SIPCleanupBYEObserved, out.Steps[0].KnownBranch.CleanupAttempts[0].State)
	require.Equal(t, SIPStepPrepared, out.Steps[0].KnownBranch.ACKState, "original ACK was not reauthorized")
	require.Equal(t, IntentDispatched, out.Intent.State)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
}

func TestDeviceSIPCleanupConcurrentStagesAndQuiescedRetry(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx, identity := context.Background(), sipCleanupIdentity(1)
	_, err := store.PrepareSIPBranchCleanup(ctx, id, 5, identity)
	require.NoError(t, err)
	duplicate, err := store.PrepareSIPBranchCleanup(ctx, id, 6, identity)
	require.NoError(t, err)
	require.EqualValues(t, 6, duplicate.Intent.RowVersion)
	for stage, mutate := range []func(context.Context, DeviceOperationIntentIdentity, int64, string) (DeviceSIPInviteSteps, error){store.DispatchSIPCleanupACK, store.DispatchSIPCleanupBYE} {
		var wg sync.WaitGroup
		var wins atomic.Int32
		errs := make(chan error, 20)
		for n := 0; n < 20; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := mutate(ctx, id, int64(6+stage), identity.AttemptID)
				if err == nil {
					wins.Add(1)
				} else {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		require.EqualValues(t, 1, wins.Load())
		for err := range errs {
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		}
	}
	_, err = newIntentFixtureStore(f.db).PrepareSIPBranchCleanup(ctx, id, 8, sipCleanupIdentity(2))
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "new store is not a new process or a quiesced transaction")
	_, err = store.ObserveSIPCleanupQuiesced(ctx, id, 8, identity.AttemptID)
	require.NoError(t, err)
	_, err = store.PrepareSIPBranchCleanup(ctx, id, 9, sipCleanupIdentity(2))
	require.NoError(t, err)
	_, err = store.DispatchSIPCleanupBYE(ctx, id, 10, identity.AttemptID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "old consumed attempt never restarts")
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	a := loaded.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, a, 2)
	require.Equal(t, a[0].OwnerRunID, a[1].OwnerRunID)
	require.Equal(t, a[0].Identity.BYE.Request.CSeq+1, a[1].Identity.BYE.Request.CSeq)
	require.NotEqual(t, a[0].Identity.BYE.Request.Branch, a[1].Identity.BYE.Request.Branch)
}

func TestDeviceSIPCleanupCommitUnknownReturnsNoPermission(t *testing.T) {
	for _, operation := range []string{"prepare", "ack", "bye", "response", "quiesced"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", operation, committed), func(t *testing.T) {
				f, store, id := sipCleanupFixture(t)
				ctx, identity, version := context.Background(), sipCleanupIdentity(1), int64(5)
				if operation != "prepare" {
					_, err := store.PrepareSIPBranchCleanup(ctx, id, version, identity)
					require.NoError(t, err)
					version++
				}
				if operation == "bye" || operation == "response" {
					_, err := store.DispatchSIPCleanupACK(ctx, id, version, identity.AttemptID)
					require.NoError(t, err)
					version++
				}
				if operation == "response" {
					_, err := store.DispatchSIPCleanupBYE(ctx, id, version, identity.AttemptID)
					require.NoError(t, err)
					version++
				}
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				fault := newIntentFixtureStore(faultDB)
				var out DeviceSIPInviteSteps
				var err error
				switch operation {
				case "prepare":
					out, err = fault.PrepareSIPBranchCleanup(ctx, id, version, identity)
				case "ack":
					out, err = fault.DispatchSIPCleanupACK(ctx, id, version, identity.AttemptID)
				case "bye":
					out, err = fault.DispatchSIPCleanupBYE(ctx, id, version, identity.AttemptID)
				case "response":
					out, err = fault.ObserveSIPCleanupBYE(ctx, id, version, DeviceSIPCleanupBYEResponse{identity.AttemptID, identity.BYE.Request.CallID, identity.BYE.Request.CSeq, identity.BYE.Request.LocalTag, identity.BYE.RemoteTag, 200})
				case "quiesced":
					out, err = fault.ObserveSIPCleanupQuiesced(ctx, id, version, identity.AttemptID)
				}
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out)
				loaded, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				want := version
				if committed {
					want++
				}
				require.Equal(t, want, loaded.Intent.RowVersion)
				if committed && (operation == "ack" || operation == "bye") {
					mutate := store.DispatchSIPCleanupACK
					if operation == "bye" {
						mutate = store.DispatchSIPCleanupBYE
					}
					_, err = mutate(ctx, id, want, identity.AttemptID)
					require.ErrorIs(t, err, ErrDeviceIntentConflict)
				}
			})
		}
	}
}

func TestDeviceSIPCleanupGateAndFixedIdentity(t *testing.T) {
	for _, access := range []int{1, 2, 3} {
		for _, completed := range []int{1, 2} {
			t.Run(fmt.Sprintf("epoch=%d/completed=%d", access, completed), func(t *testing.T) {
				f, store, id := sipCleanupFixture(t)
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=?,cleanup_completed_epoch=? WHERE id=1", access, completed).Error)
				_, err := store.PrepareSIPBranchCleanup(context.Background(), id, 5, sipCleanupIdentity(1))
				if completed == 1 {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
	for name, change := range map[string]func(*DeviceSIPCleanupAttemptIdentity){
		"attempt":      func(i *DeviceSIPCleanupAttemptIdentity) { i.AttemptID = "bad" },
		"fork":         func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.RemoteTag = "another" },
		"target":       func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.Destination = "127.0.0.2:5062" },
		"URI":          func(i *DeviceSIPCleanupAttemptIdentity) { i.ACK.Request.RequestURI += ";password=secret" },
		"from":         func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.FromURI += "other" },
		"body":         func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.BodyLength = 1 },
		"content-type": func(i *DeviceSIPCleanupAttemptIdentity) { i.ACK.Request.ContentType = "application/sdp" },
		"nil-route":    func(i *DeviceSIPCleanupAttemptIdentity) { i.ACK.Routes = nil },
		"new-route":    func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Routes = []string{"sip:other.example;lr"} },
		"same-branch":  func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.Branch = i.ACK.Request.Branch },
		"old-cseq":     func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.CSeq-- },
		"skip-cseq":    func(i *DeviceSIPCleanupAttemptIdentity) { i.BYE.Request.CSeq++ },
	} {
		t.Run(name, func(t *testing.T) {
			_, store, id := sipCleanupFixture(t)
			i := sipCleanupIdentity(1)
			change(&i)
			_, err := store.PrepareSIPBranchCleanup(context.Background(), id, 5, i)
			require.Error(t, err)
		})
	}
}

func TestDeviceSIPCleanupCapacityAndJSONDoNotDiscardFacts(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx, version := context.Background(), int64(5)
	for n := 1; n <= maxSIPCleanupAttempts; n++ {
		i := sipCleanupIdentity(n)
		_, err := store.PrepareSIPBranchCleanup(ctx, id, version, i)
		require.NoError(t, err)
		version++
		_, err = store.ObserveSIPCleanupQuiesced(ctx, id, version, i.AttemptID)
		require.NoError(t, err)
		version++
	}
	before, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	_, err = store.PrepareSIPBranchCleanup(ctx, id, version, sipCleanupIdentity(maxSIPCleanupAttempts+1))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	after, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before, after)
	for _, value := range []any{after, after.Steps[0].KnownBranch, after.Steps[0].KnownBranch.CleanupAttempts[0], sipCleanupIdentity(1)} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(raw))
	}
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	require.NotContains(t, raw, "application/sdp\r\n")
	require.Less(t, len(raw), maxIntentSIPBytes)
}

func TestDeviceSIPCleanupDamagedWireFailsClosed(t *testing.T) {
	for name, change := range map[string]func(*sipCleanupAttemptWire){
		"owner":             func(w *sipCleanupAttemptWire) { w.OwnerRunID = "fake" },
		"version":           func(w *sipCleanupAttemptWire) { w.Version++ },
		"row-version":       func(w *sipCleanupAttemptWire) { w.RowVersion++ },
		"time-zone":         func(w *sipCleanupAttemptWire) { w.PreparedAt = w.PreparedAt.In(time.FixedZone("bad", 3600)) },
		"time-precision":    func(w *sipCleanupAttemptWire) { w.PreparedAt = w.PreparedAt.Add(time.Nanosecond) },
		"prepared-dispatch": func(w *sipCleanupAttemptWire) { w.ACKDispatchStartedAt = &w.PreparedAt },
		"missing-ack":       func(w *sipCleanupAttemptWire) { w.State = SIPCleanupACKDispatched; w.RowVersion = 2 },
		"fake-closed":       func(w *sipCleanupAttemptWire) { w.State = "closed" },
		"missing-receipt":   func(w *sipCleanupAttemptWire) { w.State = SIPCleanupBYEObserved; w.RowVersion = 4 },
		"old-quiesced": func(w *sipCleanupAttemptWire) {
			earlier := w.PreparedAt.Add(-time.Second)
			w.LocalQuiescedAt = &earlier
			w.RowVersion++
		},
		"bad-branch": func(w *sipCleanupAttemptWire) { w.Identity.BYE.Request.Branch = strings.Repeat("x", 129) },
	} {
		t.Run(name, func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			_, err := store.PrepareSIPBranchCleanup(context.Background(), id, 5, sipCleanupIdentity(1))
			require.NoError(t, err)
			var raw string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
			var wire sipInviteStepsWire
			require.NoError(t, json.Unmarshal([]byte(raw), &wire))
			change(&wire.Steps[0].KnownBranch.CleanupAttempts[0])
			encoded, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(encoded)).Error)
			_, err = store.LoadSIPInviteSteps(context.Background(), id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

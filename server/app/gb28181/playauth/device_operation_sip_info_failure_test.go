package playauth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceSIPINFOCommitUnknownNeverReturnsPermission(t *testing.T) {
	for _, stage := range []string{"prepare", "dispatch", "response", "quiesced"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", stage, committed), func(t *testing.T) {
				f, store, id := sipINFOFixture(t)
				ctx, version := context.Background(), int64(6)
				i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
				if stage != "prepare" {
					_, err := store.PrepareSIPINFO(ctx, id, version, i)
					require.NoError(t, err)
					version++
				}
				if stage == "response" {
					_, err := store.DispatchSIPINFO(ctx, id, version, i.InfoID)
					require.NoError(t, err)
					version++
				}
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				fault := newIntentFixtureStore(faultDB)
				var out DeviceSIPInviteSteps
				var err error
				switch stage {
				case "prepare":
					out, err = fault.PrepareSIPINFO(ctx, id, version, i)
				case "dispatch":
					out, err = fault.DispatchSIPINFO(ctx, id, version, i.InfoID)
				case "response":
					out, err = fault.ObserveSIPINFOResponse(ctx, id, version, sipINFOResponse(i))
				case "quiesced":
					out, err = fault.ObserveSIPINFOQuiesced(ctx, id, version, i.InfoID)
				}
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out)
				loaded, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				wantVersion := version
				if committed {
					wantVersion++
				}
				require.Equal(t, wantVersion, loaded.Intent.RowVersion)
				if committed && stage == "dispatch" {
					_, err = store.DispatchSIPINFO(ctx, id, wantVersion, i.InfoID)
					require.ErrorIs(t, err, ErrDeviceIntentConflict, "readback cannot restore a consumed dispatch")
				}
				if committed && stage == "prepare" {
					out, err = store.PrepareSIPINFO(ctx, id, wantVersion, i)
					require.NoError(t, err)
					require.Equal(t, wantVersion, out.Intent.RowVersion, "no newly confirmed prepare CAS")
				}
			})
		}
	}
}

func TestDeviceSIPINFOFinalResponseMustBeExactAndDecidable(t *testing.T) {
	for _, kind := range []string{"empty", "mansrtsp", "sip_rejected"} {
		for _, status := range []int{200, 400} {
			t.Run(fmt.Sprintf("%s/%d", kind, status), func(t *testing.T) {
				_, store, id := sipINFOFixture(t)
				ctx := context.Background()
				i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "play"})
				_, err := store.PrepareSIPINFO(ctx, id, 6, i)
				require.NoError(t, err)
				_, err = store.DispatchSIPINFO(ctx, id, 7, i.InfoID)
				require.NoError(t, err)
				r := sipINFOResponse(i)
				r.BodyClass = kind
				if kind == "mansrtsp" {
					r.MANSRTSPStatus, r.MANSRTSPCSeq = status, r.CSeq
				}
				if kind == "sip_rejected" {
					r.SIPStatus = 481
				}
				for _, change := range []func(*DeviceSIPINFOResponse){
					func(r *DeviceSIPINFOResponse) { r.CallID += "other" },
					func(r *DeviceSIPINFOResponse) { r.CSeq++ },
					func(r *DeviceSIPINFOResponse) { r.RemoteTag += "other" },
					func(r *DeviceSIPINFOResponse) { r.LocalTag += "other" },
					func(r *DeviceSIPINFOResponse) { r.SIPStatus = 180 },
					func(r *DeviceSIPINFOResponse) { r.BodyClass = "unknown" },
					func(r *DeviceSIPINFOResponse) { r.MANSRTSPCSeq++ },
				} {
					bad := r
					change(&bad)
					_, err = store.ObserveSIPINFOResponse(ctx, id, 8, bad)
					require.ErrorIs(t, err, ErrDeviceIntentConflict)
				}
				// A late response may be recorded after local quiescence; neither
				// fact alone grants new INFO permission.
				_, err = store.ObserveSIPINFOQuiesced(ctx, id, 8, i.InfoID)
				require.NoError(t, err)
				_, err = store.ObserveSIPINFOResponse(ctx, id, 9, r)
				require.NoError(t, err)
				_, err = store.PrepareSIPINFO(ctx, id, 10, sipINFOIdentity(t, 2, DeviceSIPINFOCommand{Action: "resume"}))
				require.NoError(t, err, "exact negative result is known, not an unknown dispatch")
			})
		}
	}
}

func TestDeviceSIPINFODispatchConcurrentSingleWinner(t *testing.T) {
	_, store, id := sipINFOFixture(t)
	ctx, i := context.Background(), sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "play"})
	_, err := store.PrepareSIPINFO(ctx, id, 6, i)
	require.NoError(t, err)
	var wg sync.WaitGroup
	var wins atomic.Int32
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.DispatchSIPINFO(ctx, id, 7, i.InfoID); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
}

package playauth

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceSIPINFOCapacityAndJSONPreserveFacts(t *testing.T) {
	f, store, id := sipINFOFixture(t)
	ctx, version := context.Background(), int64(6)
	for n := 1; n <= maxSIPINFOSteps; n++ {
		i := sipINFOIdentity(t, n, DeviceSIPINFOCommand{Action: "pause"})
		_, err := store.PrepareSIPINFO(ctx, id, version, i)
		require.NoError(t, err)
		version++
		_, err = store.ObserveSIPINFOQuiesced(ctx, id, version, i.InfoID)
		require.NoError(t, err)
		version++
	}
	before, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	_, err = store.PrepareSIPINFO(ctx, id, version, sipINFOIdentity(t, maxSIPINFOSteps+1, DeviceSIPINFOCommand{Action: "pause"}))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	after, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = store.PrepareSIPBranchCleanup(ctx, id, version, sipCleanupIdentity(maxSIPINFOSteps+1))
	require.NoError(t, err, "INFO count exhaustion still permits bounded cleanup")
	for _, value := range []any{after.Steps[0].KnownBranch.InfoSteps[0], sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "play"}), DeviceSIPINFOCommand{}, DeviceSIPINFOResponse{}} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(raw))
	}
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	require.Less(t, len(raw), maxIntentSIPBytes)
}

func TestDeviceSIPINFODamagedWireFailsClosed(t *testing.T) {
	for name, change := range map[string]func(*sipINFOStepWire){
		"owner":                       func(w *sipINFOStepWire) { w.OwnerRunID = "caller-restarted" },
		"version":                     func(w *sipINFOStepWire) { w.Version++ },
		"row-version":                 func(w *sipINFOStepWire) { w.RowVersion++ },
		"body-digest":                 func(w *sipINFOStepWire) { w.Identity.Request.Request.BodySHA256 = sipEmptyBodySHA256 },
		"body-length":                 func(w *sipINFOStepWire) { w.Identity.Request.Request.BodyLength++ },
		"body-type":                   func(w *sipINFOStepWire) { w.Identity.Request.Request.ContentType = "application/sdp" },
		"command":                     func(w *sipINFOStepWire) { w.Identity.Command.Action = "teardown" },
		"parameter":                   func(w *sipINFOStepWire) { w.Identity.Command.Scale = 1 },
		"cseq":                        func(w *sipINFOStepWire) { w.Identity.Request.Request.CSeq++ },
		"cseq-overflow":               func(w *sipINFOStepWire) { w.Identity.Request.Request.CSeq = math.MaxUint32 },
		"remote-tag":                  func(w *sipINFOStepWire) { w.Identity.Request.RemoteTag = "different" },
		"destination":                 func(w *sipINFOStepWire) { w.Identity.Request.Request.Destination = "127.0.0.1:5090" },
		"routes":                      func(w *sipINFOStepWire) { w.Identity.Request.Routes = nil },
		"time-zone":                   func(w *sipINFOStepWire) { w.PreparedAt = w.PreparedAt.In(time.FixedZone("bad", 3600)) },
		"time-precision":              func(w *sipINFOStepWire) { w.PreparedAt = w.PreparedAt.Add(time.Nanosecond) },
		"dispatch-without-time":       func(w *sipINFOStepWire) { w.State = SIPStepMayHaveDispatched; w.RowVersion++ },
		"prepared-with-dispatch-time": func(w *sipINFOStepWire) { w.DispatchStartedAt = &w.PreparedAt; w.RowVersion++ },
		"fake-complete":               func(w *sipINFOStepWire) { w.State = "completed" },
		"old-quiesced": func(w *sipINFOStepWire) {
			earlier := w.PreparedAt.Add(-time.Second)
			w.LocalQuiescedAt = &earlier
			w.RowVersion++
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, store, id := sipINFOFixture(t)
			_, err := store.PrepareSIPINFO(context.Background(), id, 6, sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"}))
			require.NoError(t, err)
			var raw string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
			var wire sipInviteStepsWire
			require.NoError(t, json.Unmarshal([]byte(raw), &wire))
			change(&wire.Steps[0].KnownBranch.InfoSteps[0])
			encoded, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(encoded)).Error)
			_, err = store.LoadSIPInviteSteps(context.Background(), id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

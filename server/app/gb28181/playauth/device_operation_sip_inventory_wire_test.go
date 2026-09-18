package playauth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceSIPInventoryDamagedBranchMaterialFailsClosed(t *testing.T) {
	for name, change := range map[string]func(*sipInviteStepWire){
		"cross-dialog-bye": func(w *sipInviteStepWire) {
			w.AdditionalBranches[0].CleanupAttempts[0].Identity.BYE.RemoteTag = w.KnownBranch.Identity.RemoteTag
		},
		"shared-ACK-via": func(w *sipInviteStepWire) {
			w.AdditionalBranches[0].CleanupAttempts[0].Identity.ACK.Request.Branch = w.KnownBranch.CleanupAttempts[0].Identity.ACK.Request.Branch
		},
		"concurrent-owners": func(w *sipInviteStepWire) {
			w.KnownBranch.CleanupAttempts[0].LocalQuiescedAt = nil
			w.KnownBranch.CleanupAttempts[0].RowVersion--
		},
		"duplicate-tag": func(w *sipInviteStepWire) {
			w.AdditionalBranches = append(w.AdditionalBranches, w.AdditionalBranches[0])
		},
		"extra-original-ACK": func(w *sipInviteStepWire) {
			b := &w.AdditionalBranches[0]
			b.ACKState = SIPStepMayHaveDispatched
			b.ACKRowVersion = 2
			b.ACKDispatchStartedAt = &b.ObservedAt
		},
		"unknown-fault": func(w *sipInviteStepWire) {
			w.BranchInventoryFault = "closed"
			w.BranchInventoryFaultObservedAt = &w.PreparedAt
		},
		"missing-fault-time": func(w *sipInviteStepWire) { w.BranchInventoryFault = SIPBranchObserverIncomplete },
		"fault-time-only":    func(w *sipInviteStepWire) { w.BranchInventoryFaultObservedAt = &w.PreparedAt },
	} {
		t.Run(name, func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			out, err := store.ObserveSIPAdditionalBranch(ctx, id, 5, sipExtraBranch(1))
			require.NoError(t, err)
			out, err = store.PrepareSIPBranchCleanup(ctx, id, out.Intent.RowVersion, sipCleanupIdentity(1))
			require.NoError(t, err)
			out, err = store.ObserveSIPCleanupQuiesced(ctx, id, out.Intent.RowVersion, sipCleanupIdentity(1).AttemptID)
			require.NoError(t, err)
			out, err = store.PrepareSIPBranchCleanup(ctx, id, out.Intent.RowVersion, sipExtraCleanup(1))
			require.NoError(t, err)
			w := sipStepToWire(out.Steps[0])
			change(&w)
			raw, err := json.Marshal(sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{w}})
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(raw)).Error)
			_, err = store.LoadSIPInviteSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

func TestDeviceSIPInventoryRejectsINFOAfterObservationOnRead(t *testing.T) {
	f, store, id := sipINFOFixture(t)
	ctx := context.Background()
	out, err := store.PrepareSIPINFO(ctx, id, 6, sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"}))
	require.NoError(t, err)
	out, err = store.ObserveSIPAdditionalBranch(ctx, id, 7, sipExtraBranch(1))
	require.NoError(t, err)
	w := sipStepToWire(out.Steps[0])
	w.AdditionalBranches[0].ObservedAt = w.KnownBranch.ObservedAt
	w.KnownBranch.InfoSteps[0].PreparedAt = out.Intent.UpdatedAt.Add(-time.Microsecond)
	raw, err := json.Marshal(sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{w}})
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(raw)).Error)
	_, err = store.LoadSIPInviteSteps(ctx, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

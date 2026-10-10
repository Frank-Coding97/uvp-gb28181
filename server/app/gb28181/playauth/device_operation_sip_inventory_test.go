package playauth

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func sipExtraBranch(n int) DeviceSIPKnownBranchIdentity {
	b := sipKnownBranch()
	b.RemoteTag = fmt.Sprintf("remote-extra-%d", n)
	b.RouteSet = []string{}
	return b
}

func sipExtraCleanup(n int) DeviceSIPCleanupAttemptIdentity {
	i := sipCleanupIdentity(n + 20)
	i.ACK.RemoteTag, i.BYE.RemoteTag = sipExtraBranch(n).RemoteTag, sipExtraBranch(n).RemoteTag
	i.BYE.Request.CSeq = i.ACK.Request.CSeq + 1
	return i
}

func TestDeviceSIPInventoryPreservesSelectedAndBlocksINFO(t *testing.T) {
	f, store, id := sipINFOFixture(t)
	ctx := context.Background()
	i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
	before, err := store.PrepareSIPINFO(ctx, id, 6, i)
	require.NoError(t, err)
	out, err := store.ObserveSIPAdditionalBranch(ctx, id, 7, sipExtraBranch(1))
	require.NoError(t, err)
	require.Equal(t, before.Steps[0].KnownBranch, out.Steps[0].KnownBranch)
	require.Len(t, out.Steps[0].AdditionalBranches, 1)
	require.Equal(t, sipExtraBranch(1), out.Steps[0].AdditionalBranches[0].Identity)
	_, err = store.DispatchSIPINFO(ctx, id, 8, i.InfoID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	_, err = store.PrepareSIPINFO(ctx, id, 8, sipINFOIdentity(t, 2, DeviceSIPINFOCommand{Action: "resume"}))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 8, sipExtraBranch(1))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
	duplicate, err := store.ObserveSIPAdditionalBranch(ctx, id, 8, sipExtraBranch(1))
	require.NoError(t, err)
	require.Equal(t, loaded, duplicate)
}

func TestDeviceSIPInventoryBranchCleanupIsIndependent(t *testing.T) {
	for _, response := range []bool{false, true} {
		t.Run(fmt.Sprint(response), func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			out, err := store.ObserveSIPAdditionalBranch(ctx, id, 5, sipExtraBranch(1))
			require.NoError(t, err)
			a := sipCleanupIdentity(1)
			out, err = store.PrepareSIPBranchCleanup(ctx, id, out.Intent.RowVersion, a)
			require.NoError(t, err)
			_, err = store.PrepareSIPBranchCleanup(ctx, id, out.Intent.RowVersion, sipExtraCleanup(1))
			require.ErrorIs(t, err, ErrDeviceIntentConflict, "unquiesced A blocks B network ownership")
			if response {
				out, err = store.DispatchSIPCleanupACK(ctx, id, out.Intent.RowVersion, a.AttemptID)
				require.NoError(t, err)
				out, err = store.DispatchSIPCleanupBYE(ctx, id, out.Intent.RowVersion, a.AttemptID)
				require.NoError(t, err)
				out, err = store.ObserveSIPCleanupBYE(ctx, id, out.Intent.RowVersion, DeviceSIPCleanupBYEResponse{a.AttemptID, a.BYE.Request.CallID, a.BYE.Request.CSeq, a.BYE.Request.LocalTag, a.BYE.RemoteTag, 200})
				require.NoError(t, err)
			}
			out, err = store.ObserveSIPCleanupQuiesced(ctx, id, out.Intent.RowVersion, a.AttemptID)
			require.NoError(t, err)
			selected := out.Steps[0].KnownBranch
			out, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, out.Intent.RowVersion, sipExtraCleanup(1))
			require.NoError(t, err, "A completion or quiesced-unknown cannot suppress B cleanup")
			require.NotNil(t, ticket)
			lease, err := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db)).BeginSIPCleanup(ctx, ticket)
			require.NoError(t, err)
			defer lease.Release()
			out, err = store.DispatchSIPCleanupACK(ctx, id, out.Intent.RowVersion, sipExtraCleanup(1).AttemptID)
			require.NoError(t, err)
			out, err = store.DispatchSIPCleanupBYE(ctx, id, out.Intent.RowVersion, sipExtraCleanup(1).AttemptID)
			require.NoError(t, err)
			require.Equal(t, selected, out.Steps[0].KnownBranch)
			b := out.Steps[0].AdditionalBranches[0]
			require.Equal(t, SIPCleanupBYEDispatched, b.CleanupAttempts[0].State)
			require.Equal(t, a.BYE.Request.CSeq, b.CleanupAttempts[0].Identity.BYE.Request.CSeq)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, out, loaded)
		})
	}
}

func TestDeviceSIPInventoryOverflowAndConflictAreSticky(t *testing.T) {
	_, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	out, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	for n := 1; n < maxSIPObservedBranches; n++ {
		out, err = store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, sipExtraBranch(n))
		require.NoError(t, err)
	}
	before := out.Steps[0]
	out, err = store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, sipExtraBranch(100))
	require.NoError(t, err)
	require.Equal(t, SIPBranchInventoryOverflow, out.Steps[0].BranchInventoryFault)
	require.Equal(t, before.KnownBranch, out.Steps[0].KnownBranch)
	require.Equal(t, before.AdditionalBranches, out.Steps[0].AdditionalBranches)
	bad := sipExtraBranch(1)
	bad.RemoteTarget = "sip:device@127.0.0.2:5062"
	again, err := store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, bad)
	require.NoError(t, err)
	require.Equal(t, out, again, "first fault is permanent and never evicts evidence")
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
}

func TestDeviceSIPInventoryWireVersionCannotDowngrade(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	old, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	encoded, err := json.Marshal(sipInviteStepsWire{Version: 1, Steps: []sipInviteStepWire{sipStepToWire(old.Steps[0])}})
	require.NoError(t, err)
	require.Equal(t, raw, string(encoded), "v1 canonical bytes remain unchanged")
	_, err = store.ObserveSIPAdditionalBranch(ctx, id, 5, sipExtraBranch(1))
	require.NoError(t, err)
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	var wire sipInviteStepsWire
	require.NoError(t, json.Unmarshal([]byte(raw), &wire))
	require.Equal(t, 2, wire.Steps[0].Version)
	wire.Steps[0].Version = 1
	encoded, err = json.Marshal(wire)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(encoded)).Error)
	_, err = store.LoadSIPInviteSteps(ctx, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

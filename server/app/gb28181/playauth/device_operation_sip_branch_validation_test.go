package playauth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceSIPKnownBranchRejectsUnsafeMaterial(t *testing.T) {
	_, store, id := dispatchedSIPBranchFixture(t)
	for _, change := range []func(*DeviceSIPKnownBranchIdentity){
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTag = "" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTag = "tag\r\nAuthorization: secret" },
		func(i *DeviceSIPKnownBranchIdentity) { i.StatusCode = 199 },
		func(i *DeviceSIPKnownBranchIdentity) { i.StatusCode = 300 },
		func(i *DeviceSIPKnownBranchIdentity) { i.CSeq = 0 },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTarget = "sip:device:secret@127.0.0.1:5060" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTarget += "?Authorization=secret" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTarget += ";token=secret" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RemoteTarget = "sip:127.0.0.1:5060" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet = nil },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet = make([]string, 9) },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] += ";lr" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] += "=true" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] += ";transport=tls" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] = "<sip:proxy.example;lr>" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] = "sip:user:secret@proxy.example;lr" },
		func(i *DeviceSIPKnownBranchIdentity) { i.RouteSet[0] = "sip:" + strings.Repeat("x", 1025) },
	} {
		i := sipKnownBranch()
		change(&i)
		_, err := store.ObserveSIPKnownBranch(context.Background(), id, 4, i)
		require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	}
	i := sipKnownBranch()
	i.RouteSet = []string{}
	_, err := store.ObserveSIPKnownBranch(context.Background(), id, 4, i)
	require.NoError(t, err, "direct device route without Record-Route is valid")
}

func TestDeviceSIPKnownBranchRejectsDamagedStorage(t *testing.T) {
	for name, change := range map[string]func(*sipKnownBranchWire){
		"version":                 func(b *sipKnownBranchWire) { b.Version++ },
		"wrong-invite":            func(b *sipKnownBranchWire) { b.Identity.CallID += "-other" },
		"before-dispatch":         func(b *sipKnownBranchWire) { b.ObservedAt = b.ObservedAt.Add(-time.Hour) },
		"future":                  func(b *sipKnownBranchWire) { b.ObservedAt = b.ObservedAt.Add(time.Hour) },
		"offset":                  func(b *sipKnownBranchWire) { b.ObservedAt = b.ObservedAt.In(time.FixedZone("fixture", 3600)) },
		"nanoseconds":             func(b *sipKnownBranchWire) { b.ObservedAt = b.ObservedAt.Add(time.Nanosecond) },
		"unknown-state":           func(b *sipKnownBranchWire) { b.ACKState = "closed" },
		"wrong-ack-version":       func(b *sipKnownBranchWire) { b.ACKRowVersion = 2 },
		"prepared-with-dispatch":  func(b *sipKnownBranchWire) { b.ACKDispatchStartedAt = &b.ObservedAt },
		"dispatched-without-time": func(b *sipKnownBranchWire) { b.ACKState = SIPStepMayHaveDispatched; b.ACKRowVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			f, store, id := dispatchedSIPBranchFixture(t)
			_, err := store.ObserveSIPKnownBranch(context.Background(), id, 4, sipKnownBranch())
			require.NoError(t, err)
			var raw string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
			var wire sipInviteStepsWire
			require.NoError(t, json.Unmarshal([]byte(raw), &wire))
			change(wire.Steps[0].KnownBranch)
			encoded, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(encoded)).Error)
			_, err = store.LoadSIPInviteSteps(context.Background(), id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			_, err = store.DispatchSIPKnownBranchACK(context.Background(), id, 5, sipKnownBranch())
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}

func TestDeviceSIPKnownBranchPreservesOldFormatAndIndependentSlices(t *testing.T) {
	f, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	require.NotContains(t, raw, "knownBranch", "old v1 bytes remain unchanged without observations")
	identity := sipKnownBranch()
	out, err := store.ObserveSIPKnownBranch(ctx, id, 4, identity)
	require.NoError(t, err)
	identity.RouteSet[0] = "sip:changed.example;lr"
	require.Equal(t, sipKnownBranch(), out.Steps[0].KnownBranch.Identity)
	out.Steps[0].KnownBranch.Identity.RouteSet[0] = "sip:also-changed.example;lr"
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, sipKnownBranch(), loaded.Steps[0].KnownBranch.Identity)
	wrong := id
	wrong.DevicePK++
	_, err = store.ObserveSIPKnownBranch(ctx, wrong, 5, sipKnownBranch())
	require.Error(t, err)
	wrong = id
	wrong.TargetCode = "34020000001320000002"
	_, err = store.ObserveSIPKnownBranch(ctx, wrong, 5, sipKnownBranch())
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
}

func TestDeviceSIPKnownBranchByteLimitDoesNotEvictEvidence(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	version := int64(2)
	for n := 1; n <= 16; n++ {
		_, err := store.AddSIPInviteStep(ctx, id, version, sipStepIdentity(n))
		require.NoError(t, err)
		version++
		_, err = store.DispatchSIPInviteStep(ctx, id, version, sipStepIdentity(n).StepID)
		require.NoError(t, err)
		version++
	}
	failed := false
	for n := 1; n <= 16; n++ {
		i := sipStepIdentity(n)
		branch := sipKnownBranch()
		branch.InviteStepID, branch.CallID, branch.LocalTag, branch.CSeq = i.StepID, i.CallID, i.LocalTag, i.CSeq
		branch.RouteSet = make([]string, 8)
		for j := range branch.RouteSet {
			branch.RouteSet[j] = "sip:" + strings.Repeat("u", 256) + "@" + strings.Repeat("p", 240) + ".example;lr"
		}
		var before string
		require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&before).Error)
		out, err := store.ObserveSIPKnownBranch(ctx, id, version, branch)
		if err == nil {
			version++
			continue
		}
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		require.Empty(t, out.Steps)
		var after string
		require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&after).Error)
		require.Equal(t, before, after, "full record cannot discard earlier steps or branches")
		loaded, err := store.LoadSIPInviteSteps(ctx, id)
		require.NoError(t, err)
		require.Len(t, loaded.Steps, 16)
		require.Nil(t, loaded.Steps[n-1].KnownBranch)
		failed = true
		break
	}
	require.True(t, failed, "fixture must actually hit the shared 32 KiB limit")
}

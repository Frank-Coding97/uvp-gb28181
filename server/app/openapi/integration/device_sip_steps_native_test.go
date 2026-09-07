package integration

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func verifySIPInviteStepsNative(t *testing.T, ctx context.Context, db *gorm.DB, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity) {
	t.Helper()
	identity := func(n int) playauth.DeviceSIPInviteIdentity {
		return playauth.DeviceSIPInviteIdentity{StepID: fmt.Sprintf("%032x", n), CallID: fmt.Sprintf("native-invite-%d", n), CSeq: 123,
			RequestURI: "sip:34020000001320000003@3402000000", FromURI: "sip:34020000002000000001@3402000000", LocalTag: "native-from",
			ToURI: "sip:34020000001320000003@3402000000", ContactURI: "sip:34020000002000000001@127.0.0.1:5060",
			Transport: "UDP", Destination: "127.0.0.1:5060", ViaHost: "127.0.0.1", ViaPort: 5060, ViaTransport: "UDP",
			Branch: fmt.Sprintf("z9hG4bK-native-%d", n), MaxForwards: 70, ContentType: "application/sdp", BodyLength: 100, BodySHA256: strings.Repeat("a", 64)}
	}
	for n := 1; n <= 16; n++ {
		out, err := store.AddSIPInviteStep(ctx, id, int64(n+1), identity(n))
		require.NoError(t, err)
		require.Len(t, out.Steps, n)
	}
	_, err := store.AddSIPInviteStep(ctx, id, 18, identity(17))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchSIPInviteStep(ctx, id, 18, identity(1).StepID)
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
		require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps, 16)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, loaded.Steps[0].State)
	for n, step := range loaded.Steps {
		require.Equal(t, identity(n+1), step.Identity)
	}
	knownBranch := func(n int) playauth.DeviceSIPKnownBranchIdentity {
		i := identity(n)
		return playauth.DeviceSIPKnownBranchIdentity{InviteStepID: i.StepID, CallID: i.CallID, LocalTag: i.LocalTag,
			RemoteTag: fmt.Sprintf("native-remote-%d", n), CSeq: i.CSeq, StatusCode: 200,
			RemoteTarget: "sip:device@127.0.0.1:5062", RouteSet: []string{"sip:proxy.example:5060;lr"}}
	}
	_, err = store.ObserveSIPKnownBranch(ctx, id, 19, knownBranch(1))
	require.NoError(t, err)
	wins.Store(0)
	errs = make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchSIPKnownBranchACK(ctx, id, 20, knownBranch(1))
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
		require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	}
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 21, knownBranch(1))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	_, err = store.DispatchSIPInviteStep(ctx, id, 21, identity(2).StepID)
	require.NoError(t, err)
	for _, raw := range []string{"", strings.Repeat("x", 32769)} {
		require.Error(t, db.Table("gb_device_operation_intent").Where("operation_id=?", id.OperationID).Update("sip_steps_json", raw).Error)
	}
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=?", id.DevicePK).Error)
	_, err = store.DispatchSIPInviteStep(ctx, id, 22, identity(3).StepID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	_, err = store.AddSIPInviteStep(ctx, id, 22, identity(17))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	loaded, err = store.ObserveSIPKnownBranch(ctx, id, 22, knownBranch(2))
	require.NoError(t, err, "retain a dispatched transaction's late response after transfer")
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 23, knownBranch(2))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, loaded.Steps[0].KnownBranch.ACKState)
	require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[1].KnownBranch.ACKState)
	loadedAfter, err := playauth.NewDeviceOperationIntentStore(db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	// PostgreSQL returns a fixed-offset location, whereas the successful
	// mutation returns UTC. Compare the instant, not time.Location pointers.
	require.True(t, loaded.Intent.UpdatedAt.Equal(loadedAfter.Intent.UpdatedAt))
	loaded.Intent.UpdatedAt = loaded.Intent.UpdatedAt.UTC()
	loadedAfter.Intent.UpdatedAt = loadedAfter.Intent.UpdatedAt.UTC()
	require.Equal(t, loaded, loadedAfter)
	t.Log("SIP steps: 16 immutable INVITEs, 20 concurrent INVITE/ACK single winners, native byte constraints, persistent first-known branches and old-epoch observation without reauthorization passed")
}

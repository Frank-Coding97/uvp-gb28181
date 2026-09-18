package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func verifySIPCleanupNative(t *testing.T, ctx context.Context, db *gorm.DB, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, loaded playauth.DeviceSIPInviteSteps) playauth.DeviceSIPInviteSteps {
	t.Helper()
	step := loaded.Steps[1]
	ack := step.Identity
	ack.Branch = "z9hG4bK-native-cleanup-ack"
	ack.RequestURI, ack.Destination = step.KnownBranch.Identity.RemoteTarget, "proxy.example:5060"
	ack.ContentType, ack.BodyLength, ack.BodySHA256 = "", 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	bye := ack
	bye.CSeq++
	bye.Branch = "z9hG4bK-native-cleanup-bye"
	i := playauth.DeviceSIPCleanupAttemptIdentity{AttemptID: "00000000000000000000000000000101",
		ACK: playauth.DeviceSIPCleanupRequestIdentity{Request: ack, RemoteTag: step.KnownBranch.Identity.RemoteTag, Routes: []string{"sip:proxy.example:5060;lr"}},
		BYE: playauth.DeviceSIPCleanupRequestIdentity{Request: bye, RemoteTag: step.KnownBranch.Identity.RemoteTag, Routes: []string{"sip:proxy.example:5060;lr"}}}
	out, err := store.PrepareSIPBranchCleanup(ctx, id, loaded.Intent.RowVersion, i)
	require.NoError(t, err)
	version := out.Intent.RowVersion
	_, err = store.DispatchSIPCleanupBYE(ctx, id, version, i.AttemptID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	for _, mutate := range []func(context.Context, playauth.DeviceOperationIntentIdentity, int64, string) (playauth.DeviceSIPInviteSteps, error){
		store.DispatchSIPCleanupACK, store.DispatchSIPCleanupBYE,
	} {
		var wg sync.WaitGroup
		var wins atomic.Int32
		errs := make(chan error, 20)
		for n := 0; n < 20; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := mutate(ctx, id, version, i.AttemptID)
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
		version++
	}
	_, err = store.DispatchSIPCleanupBYE(ctx, id, version, i.AttemptID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict)
	response := playauth.DeviceSIPCleanupBYEResponse{AttemptID: i.AttemptID, CallID: bye.CallID, CSeq: bye.CSeq, LocalTag: bye.LocalTag, RemoteTag: i.BYE.RemoteTag, StatusCode: 200}
	out, err = store.ObserveSIPCleanupBYE(ctx, id, version, response)
	require.NoError(t, err)
	out, err = store.ObserveSIPCleanupQuiesced(ctx, id, out.Intent.RowVersion, i.AttemptID)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPCleanupBYEObserved, out.Steps[1].KnownBranch.CleanupAttempts[0].State)
	require.Equal(t, playauth.SIPStepPrepared, out.Steps[1].KnownBranch.ACKState)
	require.Equal(t, playauth.IntentDispatched, out.Intent.State)
	t.Log("cleanup: old epoch dedicated ACK/BYE CAS each 20 concurrent single winner; exact receipt and local quiescence retained; original ACK and device completion unchanged; no SIP network in native fixture")
	return out
}

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
	for _, raw := range []string{"", strings.Repeat("x", 32769)} {
		require.Error(t, db.Table("gb_device_operation_intent").Where("operation_id=?", id.OperationID).Update("sip_steps_json", raw).Error)
	}
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=?", id.DevicePK).Error)
	_, err = store.DispatchSIPInviteStep(ctx, id, 19, identity(2).StepID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	_, err = store.AddSIPInviteStep(ctx, id, 19, identity(17))
	require.ErrorIs(t, err, playauth.ErrDeviceIntentRevoked)
	loadedAfter, err := playauth.NewDeviceOperationIntentStore(db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, loaded, loadedAfter)
	t.Log("SIP steps: 16 immutable INVITEs, bounded growth, 20 concurrent single-step CAS, native byte constraints and old-epoch read-only recovery passed")
}

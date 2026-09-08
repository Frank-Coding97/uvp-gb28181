package uac

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// The parent kills this actual process after receiving its real ACK/BYE.
// No cleanup defer runs; its persistent attempt must remain unquiesced.
func TestPlaybackRecoveryProcessChild(t *testing.T) {
	path := os.Getenv("UVP_PLAYBACK_RECOVERY_TEST_DB")
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	db := authoritytest.OpenSQLite(t, path)
	authority := authoritytest.Register(t, db, os.Getenv("UVP_PLAYBACK_RECOVERY_TEST_STATE"))
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if os.Getenv("UVP_PLAYBACK_RECOVERY_TEST_MODE") == "seed" {
		id := playbackIntentSchemaFixture(t, db)
		require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
		_, err := store.Reserve(ctx, id)
		require.NoError(t, err)
		_, err = store.Dispatch(ctx, id, 1)
		require.NoError(t, err)
		in := validPlaybackInvite()
		in.Destination, in.Transport = os.Getenv("UVP_PLAYBACK_RECOVERY_TEST_PEER"), "UDP"
		request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), in)
		require.NoError(t, err)
		identity := prepared.Steps[0].Identity
		_, err = store.DispatchSIPInviteStep(ctx, id, 3, identity.StepID)
		require.NoError(t, err)
		peer, err := net.ResolveUDPAddr("udp4", in.Destination)
		require.NoError(t, err)
		response := sip.NewResponseFromRequest(request, 200, "OK", nil)
		response.To().Params.Add("tag", "recovery-remote")
		response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: peer.IP.String(), Port: peer.Port}})
		_, err = observeStoredPlaybackBranch(ctx, store, id, 4, identity, request, response)
		require.NoError(t, err)
		require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
		return
	}
	var intent playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id = ?", strings.Repeat("a", 32)).First(&intent).Error)
	r, err := u.beginRecoveredPlaybackCleanup(ctx, store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), intent.DeviceOperationIntentIdentity, strings.Repeat("b", 32), "recovery-remote")
	require.NoError(t, err)
	require.NoError(t, r.Run(ctx))
	t.Fatal("parent must kill the child before any successful completion")
}

func TestPlaybackRecoveryActualProcessDeathThenFreshWireAttempt(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	stepID := strings.Repeat("b", 32)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// The coordinator holds no authority while seed and recovery children run.
	// Every generation uses this same file and lifetime-lock directory.
	path := filepath.Join(t.TempDir(), "recovery.sqlite")
	db := authoritytest.OpenSQLite(t, path)
	stateDir := authoritytest.StateDirectory(t)
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	binary, err := os.Executable()
	require.NoError(t, err)
	seed := exec.CommandContext(ctx, binary, "-test.run=^TestPlaybackRecoveryProcessChild$", "-test.count=1")
	seed.Env = append(os.Environ(), "UVP_PLAYBACK_RECOVERY_TEST_DB="+path, "UVP_PLAYBACK_RECOVERY_TEST_STATE="+stateDir, "UVP_PLAYBACK_RECOVERY_TEST_MODE=seed", "UVP_PLAYBACK_RECOVERY_TEST_PEER="+peer.LocalAddr().String())
	seedOutput, err := seed.CombinedOutput()
	require.NoError(t, err, "%s", seedOutput)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPlaybackRecoveryProcessChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "UVP_PLAYBACK_RECOVERY_TEST_DB="+path, "UVP_PLAYBACK_RECOVERY_TEST_STATE="+stateDir)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ack, _ := readCleanupRequest(t, peer)
	oldBYE, _ := readCleanupRequest(t, peer)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, oldBYE.Method)
	require.NoError(t, cmd.Process.Kill())
	err = cmd.Wait()
	waited = true
	require.Error(t, err, "the actual owner process was killed, not gracefully closed: %s", output.String())
	require.NotNil(t, cmd.ProcessState)
	require.False(t, cmd.ProcessState.Success())
	authority := authoritytest.Register(t, db, stateDir)
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	var intent playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id = ?", strings.Repeat("a", 32)).First(&intent).Error)
	f := &playbackOperationUDPFixture{u: u, db: db, store: store, peer: peer, id: intent.DeviceOperationIntentIdentity,
		barrier: playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))}
	old, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	first := old.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, first, 1)
	require.Nil(t, first[0].LocalQuiescedAt)
	require.Nil(t, first[0].Response)
	require.Equal(t, playauth.SIPCleanupBYEDispatched, first[0].State)
	_, err = f.store.DispatchSIPCleanupBYE(ctx, f.id, old.Intent.RowVersion, first[0].Identity.AttemptID)
	require.ErrorIs(t, err, playauth.ErrDeviceIntentConflict, "old process permission never resumes")
	observation, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
	require.NoError(t, err)
	defer observation.CloseLocal(ctx)
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	listenerDone := make(chan error, 1)
	go func() { listenerDone <- f.u.client.TransportLayer().ServeUDP(listener) }()
	defer func() { _ = listener.Close(); <-listenerDone }()
	late := recoveredObservationResponse(t, old.Steps[0].Identity, "after-process-death")
	_, err = f.peer.WriteTo([]byte(late.String()), listener.LocalAddr())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		out, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(out.Steps[0].AdditionalBranches) == 1
	}, 2*time.Second, time.Millisecond)
	requirePlaybackNoPacket(t, f.peer) // Includes INVITE: observation never revives original SIP work.
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	newACK, _ := readCleanupRequest(t, f.peer)
	newBYE, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, newACK.Method)
	require.Equal(t, sip.BYE, newBYE.Method)
	require.Equal(t, oldBYE.CSeq().SeqNo+1, newBYE.CSeq().SeqNo)
	require.Equal(t, ack.CSeq().SeqNo, newACK.CSeq().SeqNo)
	require.Equal(t, string(*oldBYE.CallID()), string(*newBYE.CallID()))
	require.NotEqual(t, oldBYE.Via().Params.GetOr("branch", ""), newBYE.Via().Params.GetOr("branch", ""))
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(newBYE, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.ErrorIs(t, <-result, ErrPlaybackCleanupUnknown, "late branch and restart gap remain unknown despite this branch's 200")
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	attempts := loaded.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, attempts, 2)
	require.Equal(t, first[0], attempts[0], "do not fabricate dead process quiescence")
	require.NotEqual(t, attempts[0].OwnerRunID, attempts[1].OwnerRunID)
	require.NotNil(t, attempts[1].Response)
	require.NotNil(t, attempts[1].LocalQuiescedAt)
	require.Len(t, loaded.Steps[0].AdditionalBranches, 1)
	require.Empty(t, loaded.Steps[0].AdditionalBranches[0].CleanupAttempts)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	_, err = f.barrier.BeginEpoch(ctx, f.id.DeviceCode, 2)
	require.Error(t, err, "one branch's response cannot open the device completion gate")
	requirePlaybackNoPacket(t, f.peer)
}

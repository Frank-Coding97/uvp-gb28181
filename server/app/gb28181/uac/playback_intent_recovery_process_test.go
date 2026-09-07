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
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
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
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var intent playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id = ?", strings.Repeat("a", 32)).First(&intent).Error)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := u.beginRecoveredPlaybackCleanup(ctx, playauth.NewDeviceOperationIntentStore(db), playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), intent.DeviceOperationIntentIdentity, strings.Repeat("b", 32), "recovery-remote")
	require.NoError(t, err)
	require.NoError(t, r.Run(ctx))
	t.Fatal("parent must kill the child before any successful completion")
}

func TestPlaybackRecoveryActualProcessDeathThenFreshWireAttempt(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Copy only our isolated test database. Both the killed child and the
	// survivor open this exact file, never a rebuilt or in-memory replacement.
	path := filepath.Join(t.TempDir(), "recovery.sqlite")
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	require.NoError(t, f.db.Exec("VACUUM INTO ?", path).Error)
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	f.store = playauth.NewDeviceOperationIntentStore(db)
	f.barrier = playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	binary, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPlaybackRecoveryProcessChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "UVP_PLAYBACK_RECOVERY_TEST_DB="+path)
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
	ack, _ := readCleanupRequest(t, f.peer)
	oldBYE, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, oldBYE.Method)
	require.NoError(t, cmd.Process.Kill())
	err = cmd.Wait()
	waited = true
	require.Error(t, err, "the actual owner process was killed, not gracefully closed: %s", output.String())
	require.NotNil(t, cmd.ProcessState)
	require.False(t, cmd.ProcessState.Success())
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
	f.noACK(t) // Restoring observation must not revive any original SIP work.
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
	f.noACK(t)
}

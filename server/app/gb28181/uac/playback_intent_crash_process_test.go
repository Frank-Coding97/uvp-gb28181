package uac

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestPlaybackOriginalProcessCrashRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "invite-dispatched", "branch-observed", "ack-committed", "ack-dispatched", "info-prepared", "info-committed", "info-dispatched"} {
		t.Run(phase, func(t *testing.T) {
			testPlaybackOriginalProcessCrash(t, phase)
		})
	}
}

// Pause only after a real SQL commit, before its caller regains control.
// The parent kills the OS process while stdin stays open: no owner defer,
// transaction termination, or synthetic quiescence runs in the old process.
type playbackCrashCommitPool struct {
	gorm.ConnPool
	commits atomic.Int32
	pauseAt int32
}

type playbackCrashCommitTx struct {
	*sql.Tx
	pool *playbackCrashCommitPool
}

func (p *playbackCrashCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(*sql.DB).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &playbackCrashCommitTx{tx, p}, nil
}

func (p *playbackCrashCommitPool) GetDBConn() (*sql.DB, error) {
	return p.ConnPool.(*sql.DB), nil
}

func (tx *playbackCrashCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.commits.Add(1) == tx.pool.pauseAt {
		fmt.Fprintln(os.Stdout, "UVP_ORIGINAL_COMMIT_PAUSED")
		_, err := bufio.NewReader(os.Stdin).ReadByte()
		return err
	}
	return nil
}

func TestPlaybackOriginalProcessCrashChild(t *testing.T) {
	path := os.Getenv("UVP_ORIGINAL_CRASH_DB")
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	var intent playauth.DeviceOperationIntent
	require.NoError(t, db.Where("operation_id = ?", strings.Repeat("a", 32)).First(&intent).Error)
	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	u, err := New(ua, "34020000002000000001", "3402000000", "192.0.2.1", 5061, false)
	require.NoError(t, err)
	phase := os.Getenv("UVP_ORIGINAL_CRASH_PHASE")
	pool := &playbackCrashCommitPool{ConnPool: raw}
	switch phase {
	case "prepared":
		pool.pauseAt = 1
	case "branch-observed":
		pool.pauseAt = 3 // Prepare, INVITE dispatch, first branch observation.
	case "ack-committed":
		pool.pauseAt = 4
	case "info-prepared":
		pool.pauseAt = 5
	case "info-committed":
		pool.pauseAt = 6
	}
	pausedDB := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
	pausedDB.Statement.ConnPool = pool
	store := playauth.NewDeviceOperationIntentStore(pausedDB)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	in := validPlaybackInvite()
	in.Destination, in.Transport = os.Getenv("UVP_ORIGINAL_CRASH_PEER"), "UDP"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op, err := u.beginPlaybackIntentOperation(ctx, store, barrier, intent.DeviceOperationIntentIdentity, intent.RowVersion, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	require.NoError(t, op.Start(ctx))
	_, err = op.ReadAndAccept(ctx)
	require.NoError(t, err)
	if strings.HasPrefix(phase, "info-") {
		require.NoError(t, op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}))
		t.Fatal("parent must kill the original process before INFO returns")
	}
	_, err = bufio.NewReader(os.Stdin).ReadByte()
	t.Fatalf("parent must kill the original process, stdin returned: %v", err)
}

func testPlaybackOriginalProcessCrash(t *testing.T, phase string) {
	t.Helper()
	// Parent seeds only the parent intent. No original SIP owner, branch,
	// transaction, or snapshot is created in the surviving UAC.
	u, seed, _, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, seed.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	path := filepath.Join(t.TempDir(), "original-crash.sqlite")
	require.NoError(t, seed.Exec("VACUUM INTO ?", path).Error)
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	binary, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPlaybackOriginalProcessCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "UVP_ORIGINAL_CRASH_DB="+path, "UVP_ORIGINAL_CRASH_PHASE="+phase, "UVP_ORIGINAL_CRASH_PEER="+peer.LocalAddr().String())
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	defer stdout.Close()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	paused := make(chan struct{})
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "UVP_ORIGINAL_COMMIT_PAUSED" {
				close(paused)
			}
		}
	}()
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		<-outputDone
		if t.Failed() {
			t.Logf("child stderr: %s", stderr.String())
		}
	}()
	waitCommit := func() {
		select {
		case <-paused:
		case <-outputDone:
			t.Fatal("child exited without reaching committed crash point")
		case <-ctx.Done():
			t.Fatal("child did not reach committed crash point")
		}
	}
	var invite, infoRequest *sip.Request
	var originalAddress net.Addr
	var response *sip.Response
	if phase == "prepared" {
		waitCommit()
		requirePlaybackNoPacket(t, peer)
	} else {
		invite, originalAddress = readCleanupRequest(t, peer)
		require.Equal(t, sip.INVITE, invite.Method)
		response = sip.NewResponseFromRequest(invite, 200, "OK", nil)
		response.To().Params.Add("tag", "original-crash-device")
		response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
		if phase != "invite-dispatched" {
			_, err = peer.WriteTo([]byte(response.String()), originalAddress)
			require.NoError(t, err)
			if phase == "branch-observed" || phase == "ack-committed" {
				waitCommit()
				requirePlaybackNoPacket(t, peer)
			} else {
				ack, _ := readCleanupRequest(t, peer)
				require.Equal(t, sip.ACK, ack.Method)
				if phase == "info-dispatched" {
					infoRequest, _ = readCleanupRequest(t, peer)
					require.Equal(t, sip.INFO, infoRequest.Method)
				} else if strings.HasPrefix(phase, "info-") {
					waitCommit()
					requirePlaybackNoPacket(t, peer)
				}
			}
		}
	}
	require.NoError(t, cmd.Process.Kill())
	err = cmd.Wait()
	waited = true
	require.Error(t, err)
	require.False(t, cmd.ProcessState.Success(), "real original owner died without CloseLocal")
	// Drain only pre-crash datagrams before enabling survivor transport. This
	// prevents queued old retransmissions from masking any new business send.
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	buffer := make([]byte, 8192)
	for {
		n, _, err := peer.ReadFrom(buffer)
		if err != nil {
			var timeout net.Error
			require.ErrorAs(t, err, &timeout)
			require.True(t, timeout.Timeout())
			break
		}
		message, err := sip.ParseMessage(buffer[:n])
		require.NoError(t, err)
		r, ok := message.(*sip.Request)
		require.True(t, ok)
		require.NotNil(t, invite)
		require.Equal(t, string(*invite.CallID()), string(*r.CallID()))
		require.True(t, r.Method == sip.INVITE || (phase == "info-dispatched" && r.Method == sip.INFO))
	}
	// Reopen the exact file only after OS exit; no reconstruction or copying
	// of the child's committed SIP state occurs at the restart boundary.
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	defer raw.Close()
	store := playauth.NewDeviceOperationIntentStore(db)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	defer closeScanObservations(t, u)
	old, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, old.Steps, 1)
	step := old.Steps[0]
	require.Equal(t, playauth.IntentDispatched, old.Intent.State)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	if phase == "prepared" {
		require.Equal(t, playauth.SIPStepPrepared, step.State)
		require.Nil(t, step.KnownBranch)
	} else {
		require.Equal(t, playauth.SIPStepMayHaveDispatched, step.State)
		require.Equal(t, string(*invite.CallID()), step.Identity.CallID)
		require.Equal(t, invite.CSeq().SeqNo, step.Identity.CSeq)
		if phase == "invite-dispatched" {
			require.Nil(t, step.KnownBranch)
		} else {
			require.NotNil(t, step.KnownBranch)
			wantACK := playauth.SIPStepMayHaveDispatched
			if phase == "branch-observed" {
				wantACK = playauth.SIPStepPrepared
			}
			require.Equal(t, wantACK, step.KnownBranch.ACKState)
			if strings.HasPrefix(phase, "info-") {
				require.Len(t, step.KnownBranch.InfoSteps, 1)
				a := step.KnownBranch.InfoSteps[0]
				want := playauth.SIPStepMayHaveDispatched
				if phase == "info-prepared" {
					want = playauth.SIPStepPrepared
				}
				require.Equal(t, want, a.State)
				require.Equal(t, invite.CSeq().SeqNo+1, a.Identity.Request.Request.CSeq)
				if infoRequest != nil {
					require.Equal(t, infoRequest.CSeq().SeqNo, a.Identity.Request.Request.CSeq)
				}
				require.Nil(t, a.Response)
				require.Nil(t, a.LocalQuiescedAt)
			} else {
				require.Empty(t, step.KnownBranch.InfoSteps)
			}
		}
	}
	recoverPage := func() error {
		page, err := u.RecoverPlaybackIntents(ctx, store, barrier, 1, id.DeviceCode, 2, "", 1)
		if page.Scanned != 1 || page.Pending != 1 || page.Cancelled != 0 {
			return fmt.Errorf("unexpected recovery page: %+v: %w", page, err)
		}
		return err
	}
	if step.KnownBranch == nil {
		require.ErrorIs(t, recoverPage(), ErrPlaybackCleanupUnknown)
		requirePlaybackNoPacket(t, peer)
		if phase == "invite-dispatched" {
			// Restore the same actual local UDP endpoint, not an unrelated new
			// test port. The peer's delayed response targets its original sender.
			listener, err := net.ListenPacket("udp4", originalAddress.String())
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- u.client.TransportLayer().ServeUDP(listener) }()
			defer func() { _ = listener.Close(); <-done }()
			_, err = peer.WriteTo([]byte(response.String()), originalAddress)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				loaded, err := store.LoadSIPInviteSteps(ctx, id)
				return err == nil && loaded.Steps[0].KnownBranch != nil
			}, 2*time.Second, time.Millisecond)
			requirePlaybackNoPacket(t, peer)
		}
	}
	if phase != "prepared" {
		finished := make(chan error, 1)
		done := make(chan struct{})
		go func() { defer close(done); finished <- recoverPage() }()
		defer func() { cancel(); <-done }()
		ack, _ := readCleanupRequest(t, peer)
		bye, address := readCleanupRequest(t, peer)
		require.Equal(t, sip.ACK, ack.Method)
		require.Equal(t, sip.BYE, bye.Method)
		require.Equal(t, invite.CSeq().SeqNo, ack.CSeq().SeqNo)
		last := invite.CSeq().SeqNo
		if step.KnownBranch != nil && len(step.KnownBranch.InfoSteps) != 0 {
			last = step.KnownBranch.InfoSteps[0].Identity.Request.Request.CSeq
		}
		require.Equal(t, last+1, bye.CSeq().SeqNo)
		require.Equal(t, string(*invite.CallID()), string(*bye.CallID()))
		require.Equal(t, "original-crash-device", bye.To().Params.GetOr("tag", ""))
		_, err = peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
		require.NoError(t, err)
		require.ErrorIs(t, <-finished, ErrPlaybackCleanupUnknown)
	}
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, step.Identity, loaded.Steps[0].Identity)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	if phase == "prepared" {
		require.Equal(t, step, loaded.Steps[0], "never revive prepared original work")
		require.Empty(t, u.playbackObservations)
	} else {
		require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
		branch := loaded.Steps[0].KnownBranch
		require.Len(t, branch.CleanupAttempts, 1)
		require.NotNil(t, branch.CleanupAttempts[0].Response)
		require.NotNil(t, branch.CleanupAttempts[0].LocalQuiescedAt)
		if step.KnownBranch != nil {
			require.Equal(t, step.KnownBranch.InfoSteps, branch.InfoSteps, "never invent the dead INFO owner's response or quiescence")
		}
	}
	require.ErrorIs(t, recoverPage(), ErrPlaybackCleanupUnknown)
	requirePlaybackNoPacket(t, peer) // Includes INVITE/INFO, unlike noACK.
	state, err := playauth.NewDeviceCleanupStore(db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
	_, err = barrier.BeginEpoch(ctx, id.DeviceCode, 2)
	require.Error(t, err, "restart gap cannot authorize the new device epoch")
}

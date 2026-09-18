package uac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func awaitCleanupFirstBranch(t *testing.T, f *playbackOperationUDPFixture) {
	t.Helper()
	f.respond(t, 200)
	require.Eventually(t, func() bool {
		f.op.factMu.Lock()
		defer f.op.factMu.Unlock()
		return f.op.first != nil
	}, time.Second, time.Millisecond)
}

func readCleanupRequest(t *testing.T, peer net.PacketConn) (*sip.Request, net.Addr) {
	t.Helper()
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	buffer := make([]byte, 8192)
	n, address, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	request, ok := message.(*sip.Request)
	require.True(t, ok)
	return request, address
}

func TestPlaybackIntentCleanupActualUDPOnlyAfterDurableHandoff(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	ack, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	bye, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.BYE, bye.Method)
	require.Equal(t, ack.CSeq().SeqNo+1, bye.CSeq().SeqNo)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	attempts := stored.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, attempts, 1)
	require.Equal(t, playauth.SIPCleanupBYEDispatched, attempts[0].State)
	require.Equal(t, attempts[0].Identity.BYE.Request.Branch, bye.Via().Params.GetOr("branch", ""))
	waitCtx, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "actual cleanup tx still owns barrier after original quiescence")
	stop()
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	stored, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	a := stored.Steps[0].KnownBranch.CleanupAttempts[0]
	require.Equal(t, playauth.SIPCleanupBYEObserved, a.State)
	require.NotNil(t, a.LocalQuiescedAt)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	state, err := playauth.NewDeviceCleanupStore(f.db).Load(ctx, f.id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
	require.NoError(t, f.op.CleanupKnownBranch(ctx), "repeat persists facts only")
	f.noACK(t)
}

func TestPlaybackIntentCleanupAfterLocalReleaseCannotSend(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	require.NoError(t, f.op.CloseLocal(context.Background()))
	require.ErrorIs(t, f.op.CleanupKnownBranch(context.Background()), ErrPlaybackCleanupUnknown)
	stored, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
	require.NoError(t, err)
	require.Empty(t, stored.Steps[0].KnownBranch.CleanupAttempts)
	f.noACK(t)
}

func TestPlaybackIntentCleanupCannotReleaseIfOriginalEvidenceDisappears(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	ctx := context.Background()
	require.NoError(t, f.op.stopOriginal(ctx))
	require.NoError(t, f.op.enter(ctx))
	defer f.op.leave()
	stored, err := f.op.persistOriginalFacts(ctx)
	require.NoError(t, err)
	owner, err := f.op.dua.NewBranchCleanup(f.op.request, f.op.first, f.op.invite.CSeq+1)
	require.NoError(t, err)
	f.op.cleanup = &playbackIntentCleanup{owned: owner, branch: stored.Steps[0].KnownBranch.Identity, stopDone: make(chan struct{})}
	var saved string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&saved).Error)
	require.NotNil(t, stored.Steps[0].KnownBranch)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=NULL").Error)
	require.ErrorIs(t, f.op.finishCleanup(ctx), ErrPlaybackCleanupUnknown)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", saved).Error)
	require.NoError(t, f.op.finishCleanup(ctx))
}

func TestPlaybackIntentCleanupUnknownCommitNeverResends(t *testing.T) {
	for _, stage := range []int32{2, 3, 4, 5, 6} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage=%d/commit=%v", stage, committed), func(t *testing.T) {
				if !authoritytest.InProcess(t) {
					return
				}

				f := newPlaybackOperationUDPFixture(t)
				awaitCleanupFirstBranch(t, f)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				faultDB := authoritytest.CommitFaultDB(t, f.db, stage, committed)
				f.op.store = newAuthorizedIntentTestStore(t, faultDB)
				finished := make(chan error, 1)
				go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
				if stage >= 4 {
					ack, _ := readCleanupRequest(t, f.peer)
					require.Equal(t, sip.ACK, ack.Method)
				}
				if stage >= 5 {
					bye, address := readCleanupRequest(t, f.peer)
					require.Equal(t, sip.BYE, bye.Method)
					_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
					require.NoError(t, err)
				}
				require.Error(t, <-finished)
				if stage >= 5 {
					waitCtx, stop := context.WithTimeout(ctx, 20*time.Millisecond)
					require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "uncertain fact persistence retains cleanup lease")
					stop()
				}
				require.NoError(t, f.op.CloseLocal(ctx), "retry confirms facts without resending")
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				if stage >= 5 {
					require.NoError(t, f.op.CleanupKnownBranch(ctx))
				} else {
					require.ErrorIs(t, f.op.CleanupKnownBranch(ctx), ErrPlaybackCleanupUnknown)
				}
				f.noACK(t)
				loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
				require.NoError(t, err)
				require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
			})
		}
	}
}

func TestPlaybackIntentCleanupHandoffSQLGapsKeepBarrier(t *testing.T) {
	for _, stage := range []string{"prepare-attempt", "register-lease", "dispatch-ack"} {
		t.Run(stage, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f := newPlaybackOperationUDPFixture(t)
			awaitCleanupFirstBranch(t, f)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var count atomic.Int32
			block := func(tx *gorm.DB) {
				if tx.Statement.Table != "gb_device_operation_intent" {
					return
				}
				target := int32(2)
				if stage == "register-lease" {
					target = 4
				} else if stage == "dispatch-ack" {
					target = 3
				}
				if count.Add(1) == target {
					close(entered)
					<-release
				}
			}
			if stage == "register-lease" {
				require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("fixture:cleanup-gap", block))
				defer f.db.Callback().Query().Remove("fixture:cleanup-gap")
			} else {
				require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture:cleanup-gap", block))
				defer f.db.Callback().Update().Remove("fixture:cleanup-gap")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("fixture never reached handoff boundary")
			}
			select {
			case <-f.op.owned.Quiesced():
			default:
				t.Fatal("original transaction must exit before cleanup")
			}
			if stage == "dispatch-ack" {
				require.ErrorIs(t, f.op.lease.Context().Err(), context.Canceled, "original lease is released after cleanup installation")
			} else {
				require.NoError(t, f.op.lease.Context().Err(), "original lease retained through handoff")
			}
			waitCtx, stop := context.WithTimeout(ctx, 20*time.Millisecond)
			require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
			stop()
			f.noACK(t)
			once.Do(func() { close(release) })
			ack, _ := readCleanupRequest(t, f.peer)
			require.Equal(t, sip.ACK, ack.Method)
			bye, address := readCleanupRequest(t, f.peer)
			require.Equal(t, sip.BYE, bye.Method)
			_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
			require.NoError(t, err)
			require.NoError(t, <-finished)
		})
	}
}

func TestPlaybackIntentCleanupActualTCP(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := newAuthorizedBarrierTest(t, db)
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.Addr().String(), "TCP"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	op, err := u.beginPlaybackIntentOperation(ctx, store, barrier, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	defer op.CloseLocal(context.Background())
	require.NoError(t, peer.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	conn, err := peer.Accept()
	require.NoError(t, err)
	defer conn.Close()
	parser := sip.NewParser().NewSIPStream()
	defer parser.Close()
	var pending []sip.Message
	read := func() *sip.Request {
		t.Helper()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		buffer := make([]byte, 8192)
		for len(pending) == 0 {
			n, err := conn.Read(buffer)
			require.NoError(t, err)
			err = parser.ParseSIPStream(buffer[:n], func(m sip.Message) { pending = append(pending, m) })
			if !errors.Is(err, sip.ErrParseSipPartial) {
				require.NoError(t, err)
			}
		}
		r := pending[0].(*sip.Request)
		pending = pending[1:]
		return r
	}
	require.NoError(t, op.Start(ctx))
	invite := read()
	response := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	response.To().Params.Add("tag", "cleanup-tcp")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.Addr().(*net.TCPAddr).Port}})
	_, err = conn.Write([]byte(response.String()))
	require.NoError(t, err)
	require.Eventually(t, func() bool { op.factMu.Lock(); defer op.factMu.Unlock(); return op.first != nil }, time.Second, time.Millisecond)
	finished := make(chan error, 1)
	go func() { finished <- op.CleanupKnownBranch(ctx) }()
	require.Equal(t, sip.ACK, read().Method)
	bye := read()
	require.Equal(t, sip.BYE, bye.Method)
	_, err = conn.Write([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()))
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPCleanupBYEObserved, loaded.Steps[0].KnownBranch.CleanupAttempts[0].State)
	require.NotNil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].LocalQuiescedAt)
}

func TestPlaybackIntentCleanupCancellationAnd481StayUnknown(t *testing.T) {
	for _, outcome := range []string{"481", "cancel", "transfer"} {
		t.Run(outcome, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f := newPlaybackOperationUDPFixture(t)
			awaitCleanupFirstBranch(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
			ack, _ := readCleanupRequest(t, f.peer)
			require.Equal(t, sip.ACK, ack.Method)
			bye, address := readCleanupRequest(t, f.peer)
			require.Equal(t, sip.BYE, bye.Method)
			switch outcome {
			case "481":
				_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 481, "No dialog", nil).String()), address)
				require.NoError(t, err)
			case "cancel":
				cancel()
			case "transfer":
				guard, err := f.barrier.LockTransfer(ctx, 1)
				require.NoError(t, err)
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				guard.Commit(2)
				guard.Release()
			}
			require.Error(t, <-finished)
			verifyCtx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			require.NoError(t, f.barrier.WaitBefore(verifyCtx, 1, 2))
			loaded, err := f.store.LoadSIPInviteSteps(verifyCtx, f.id)
			require.NoError(t, err)
			a := loaded.Steps[0].KnownBranch.CleanupAttempts[0]
			require.Equal(t, playauth.SIPCleanupBYEDispatched, a.State)
			require.Nil(t, a.Response)
			require.NotNil(t, a.LocalQuiescedAt)
			require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
			require.ErrorIs(t, f.op.CleanupKnownBranch(verifyCtx), ErrPlaybackCleanupUnknown)
			f.noACK(t)
		})
	}
}

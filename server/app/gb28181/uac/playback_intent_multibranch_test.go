package uac

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func addPlaybackCleanupFork(t *testing.T, f *playbackOperationUDPFixture, tag string) net.PacketConn {
	t.Helper()
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	f.op.factMu.Lock()
	r := f.op.first.Clone()
	f.op.factMu.Unlock()
	r.To().Params.Add("tag", tag)
	r.Contact().Address.Port = peer.LocalAddr().(*net.UDPAddr).Port
	_, err = f.peer.WriteTo([]byte(r.String()), f.address)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for _, response := range f.op.owned.ObservedBranches().Responses {
			if singlePlaybackBranchTag(response.To().Params) == tag {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	return peer
}

func TestPlaybackIntentMultiBranchHandoffHasNoBarrierGap(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	peerB := addPlaybackCleanupFork(t, f, "branch-B")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture:branch-B-prepare", func(tx *gorm.DB) {
		if tx.Statement.Table != "gb_device_operation_intent" {
			return
		}
		values, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok {
			return
		}
		raw, ok := values["sip_steps_json"].(string)
		if !ok {
			return
		}
		var wire struct {
			Steps []struct {
				AdditionalBranches []struct{ CleanupAttempts []struct{ State string } }
			}
		}
		if json.Unmarshal([]byte(raw), &wire) != nil || len(wire.Steps) != 1 || len(wire.Steps[0].AdditionalBranches) != 1 {
			return
		}
		attempts := wire.Steps[0].AdditionalBranches[0].CleanupAttempts
		if len(attempts) != 1 || attempts[0].State != playauth.SIPCleanupPrepared {
			return
		}
		select {
		case <-entered:
			return
		default:
			close(entered)
		}
		<-release
	}))
	defer f.db.Callback().Update().Remove("fixture:branch-B-prepare")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	_, _ = readCleanupRequest(t, f.peer)
	byeA, addrA := readCleanupRequest(t, f.peer)
	_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(byeA, 200, "OK", nil).String()), addrA)
	require.NoError(t, err)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("B prepare was never reached")
	}
	waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "A quiesced; B not registered yet; parent still owns device")
	stop()
	once.Do(func() { close(release) })
	_, _ = readCleanupRequest(t, peerB)
	byeB, addrB := readCleanupRequest(t, peerB)
	_, err = peerB.WriteTo([]byte(sip.NewResponseFromRequest(byeB, 200, "OK", nil).String()), addrB)
	require.NoError(t, err)
	require.NoError(t, <-finished)
}

func TestPlaybackIntentMultiBranchCleanupUsesExactDialogs(t *testing.T) {
	for _, statusA := range []int{200, 481} {
		t.Run(fmt.Sprint(statusA), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f := newPlaybackOperationUDPFixture(t)
			awaitCleanupFirstBranch(t, f)
			peerB := addPlaybackCleanupFork(t, f, "branch-B")
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
			ackA, _ := readCleanupRequest(t, f.peer)
			byeA, addrA := readCleanupRequest(t, f.peer)
			require.Equal(t, sip.ACK, ackA.Method)
			require.Equal(t, sip.BYE, byeA.Method)
			require.Equal(t, "owned-device", singlePlaybackBranchTag(byeA.To().Params))
			_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(byeA, statusA, "fixture", nil).String()), addrA)
			require.NoError(t, err)
			ackB, _ := readCleanupRequest(t, peerB)
			byeB, addrB := readCleanupRequest(t, peerB)
			require.Equal(t, sip.ACK, ackB.Method)
			require.Equal(t, sip.BYE, byeB.Method)
			require.Equal(t, "branch-B", singlePlaybackBranchTag(ackB.To().Params))
			require.Equal(t, "branch-B", singlePlaybackBranchTag(byeB.To().Params))
			require.Equal(t, ackA.CSeq().SeqNo, ackB.CSeq().SeqNo)
			require.Equal(t, byeA.CSeq().SeqNo, byeB.CSeq().SeqNo)
			require.NotEqual(t, byeA.Via().Params.GetOr("branch", ""), byeB.Via().Params.GetOr("branch", ""))
			stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
			require.NoError(t, err)
			require.NotNil(t, stored.Steps[0].KnownBranch.CleanupAttempts[0].LocalQuiescedAt, "A must really exit before B is admitted")
			waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
			require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
			stop()
			_, err = peerB.WriteTo([]byte(sip.NewResponseFromRequest(byeB, 200, "OK", nil).String()), addrB)
			require.NoError(t, err)
			err = <-finished
			if statusA == 200 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
			}
			require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
			stored, err = f.store.LoadSIPInviteSteps(ctx, f.id)
			require.NoError(t, err)
			b := stored.Steps[0].AdditionalBranches[0]
			require.Len(t, b.CleanupAttempts, 1)
			require.Equal(t, playauth.SIPCleanupBYEObserved, b.CleanupAttempts[0].State)
			require.NotNil(t, b.CleanupAttempts[0].LocalQuiescedAt)
			require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
			state, err := playauth.NewDeviceCleanupStore(f.db).Load(ctx, f.id.DeviceCode)
			require.NoError(t, err)
			require.EqualValues(t, 1, state.CleanupCompletedEpoch, "known dialogs do not prove complete fork coverage")
			_ = f.op.CleanupKnownBranch(ctx)
			f.noACK(t)
			require.NoError(t, peerB.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
			_, _, err = peerB.ReadFrom(make([]byte, 8192))
			require.Error(t, err, "repeat must not send another B attempt")
		})
	}
}

func TestPlaybackIntentMultiBranchCloseStopsCurrentWithoutStartingNext(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	peerB := addPlaybackCleanupFork(t, f, "branch-B")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	_, _ = readCleanupRequest(t, f.peer)
	byeA, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.BYE, byeA.Method)
	closeCtx, stop := context.WithTimeout(ctx, 300*time.Millisecond)
	defer stop()
	require.NoError(t, f.op.CloseLocal(closeCtx), "Close must interrupt the active BYE without waiting for its caller timeout")
	require.Error(t, <-finished)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.NotNil(t, stored.Steps[0].KnownBranch.CleanupAttempts[0].LocalQuiescedAt)
	require.Empty(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	require.Error(t, f.op.CleanupKnownBranch(ctx), "aborted batch cannot resume untouched B after releasing its original owner")
	require.NoError(t, peerB.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, _, err = peerB.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "Close and repeated cleanup must not send B")
}

func TestPlaybackIntentMultiBranchUnknownPermissionStopsBatch(t *testing.T) {
	for _, stage := range []int32{1, 2, 3, 4, 5} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage=%d/commit=%v", stage, committed), func(t *testing.T) {
				if !authoritytest.InProcess(t) {
					return
				}

				f := newPlaybackOperationUDPFixture(t)
				awaitCleanupFirstBranch(t, f)
				peerB := addPlaybackCleanupFork(t, f, "branch-B")
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				// Persist the real inventory first so the injected transaction is
				// unambiguously A's fresh cleanup permission, not branch observation.
				require.NoError(t, f.op.stopOriginal(ctx))
				require.NoError(t, f.op.enter(ctx))
				_, err := f.op.persistOriginalFacts(ctx)
				require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
				f.op.multiCleanup = true
				f.op.leave()
				faultDB := authoritytest.CommitFaultDB(t, f.db, stage, committed)
				f.op.store = newAuthorizedIntentTestStore(t, faultDB)
				finished := make(chan error, 1)
				go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
				if stage >= 3 {
					ack, _ := readCleanupRequest(t, f.peer)
					require.Equal(t, sip.ACK, ack.Method)
				}
				if stage >= 4 {
					bye, address := readCleanupRequest(t, f.peer)
					require.Equal(t, sip.BYE, bye.Method)
					_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
					require.NoError(t, err)
				}
				select {
				case err = <-finished:
					require.Error(t, err)
				case <-time.After(250 * time.Millisecond):
					t.Fatal("uncertain A permission must stop the batch, not start waiting for B")
				}
				stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
				require.NoError(t, err)
				require.Empty(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts)
				waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
				require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
				stop()
				require.NoError(t, f.op.CloseLocal(ctx), "only reconcile the attempted branch; never start B")
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				require.Error(t, f.op.CleanupKnownBranch(ctx), "closed batch cannot mint B permission")
				f.noACK(t)
				require.NoError(t, peerB.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
				_, _, err = peerB.ReadFrom(make([]byte, 8192))
				require.Error(t, err)
			})
		}
	}
}

func TestPlaybackIntentMultiBranchFaultNeverProvesCoverage(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	peerB := addPlaybackCleanupFork(t, f, "branch-B")
	f.op.factMu.Lock()
	conflict := f.op.first.Clone()
	f.op.factMu.Unlock()
	conflict.Contact().Address.Port++
	_, err := f.peer.WriteTo([]byte(conflict.String()), f.address)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.op.owned.ObservedBranches().Incomplete }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	for _, peer := range []net.PacketConn{f.peer, peerB} {
		ack, _ := readCleanupRequest(t, peer)
		require.Equal(t, sip.ACK, ack.Method)
		bye, address := readCleanupRequest(t, peer)
		require.Equal(t, sip.BYE, bye.Method)
		_, err = peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
		require.NoError(t, err)
	}
	require.ErrorIs(t, <-finished, ErrPlaybackCleanupUnknown)
	require.ErrorIs(t, f.op.CloseLocal(ctx), ErrPlaybackCleanupUnknown)
	waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
	defer stop()
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.NotEmpty(t, stored.Steps[0].BranchInventoryFault)
	for _, branch := range playbackObservedBranchRecords(stored.Steps[0]) {
		require.Equal(t, playauth.SIPCleanupBYEObserved, branch.CleanupAttempts[0].State)
		require.NotNil(t, branch.CleanupAttempts[0].LocalQuiescedAt)
	}
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
}

func TestPlaybackIntentMultiBranchRetryContinuesOnlyUntouchedBranch(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	peerB := addPlaybackCleanupFork(t, f, "branch-B")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, f.op.stopOriginal(ctx))
	require.NoError(t, f.op.enter(ctx))
	_, err := f.op.persistOriginalFacts(ctx)
	require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
	f.op.multiCleanup = true
	f.op.leave()
	faultDB := authoritytest.CommitFaultDB(t, f.db, 1, true)
	f.op.store = newAuthorizedIntentTestStore(t, faultDB)
	require.Error(t, f.op.CleanupKnownBranch(ctx))
	f.noACK(t)
	// A's actual owner is now quiesced and durably reconciled. A subsequent
	// explicit cleanup can use the still-held original owner for untouched B.
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	ack, _ := readCleanupRequest(t, peerB)
	bye, address := readCleanupRequest(t, peerB)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, bye.Method)
	_, err = peerB.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.ErrorIs(t, <-finished, ErrPlaybackCleanupUnknown, "A never obtained a remote success")
	f.noACK(t)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	a := stored.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, a, 1)
	require.Equal(t, playauth.SIPCleanupPrepared, a[0].State)
	require.NotNil(t, a[0].LocalQuiescedAt)
	require.Len(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts, 1)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
}

func TestPlaybackIntentMultiBranchEightObservedDialogs(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	peers := []net.PacketConn{f.peer}
	for index := 1; index < 8; index++ {
		peers = append(peers, addPlaybackCleanupFork(t, f, fmt.Sprintf("branch-%d", index)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	for index, peer := range peers {
		ack, _ := readCleanupRequest(t, peer)
		bye, address := readCleanupRequest(t, peer)
		require.Equal(t, sip.ACK, ack.Method)
		require.Equal(t, sip.BYE, bye.Method)
		if index > 0 {
			require.Equal(t, fmt.Sprintf("branch-%d", index), singlePlaybackBranchTag(bye.To().Params))
		}
		_, err := peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
		require.NoError(t, err)
	}
	require.NoError(t, <-finished)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	state, err := playauth.NewDeviceCleanupStore(f.db).Load(ctx, f.id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch, "bounded observed inventory is not full coverage")
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	require.Len(t, stored.Steps[0].AdditionalBranches, 7)
	for _, branch := range playbackObservedBranchRecords(stored.Steps[0]) {
		require.Len(t, branch.CleanupAttempts, 1)
		require.NotNil(t, branch.CleanupAttempts[0].LocalQuiescedAt)
		require.Equal(t, playauth.SIPCleanupBYEObserved, branch.CleanupAttempts[0].State)
	}
}

func TestPlaybackIntentMultiBranchAdditionalCommitUnknownRetainsOwner(t *testing.T) {
	for _, stage := range []int32{6, 7, 8, 9, 10} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage=%d/commit=%v", stage, committed), func(t *testing.T) {
				if !authoritytest.InProcess(t) {
					return
				}

				f := newPlaybackOperationUDPFixture(t)
				awaitCleanupFirstBranch(t, f)
				peerB := addPlaybackCleanupFork(t, f, "branch-B")
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				require.NoError(t, f.op.stopOriginal(ctx))
				require.NoError(t, f.op.enter(ctx))
				_, err := f.op.persistOriginalFacts(ctx)
				require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
				f.op.multiCleanup = true
				f.op.leave()
				faultDB := authoritytest.CommitFaultDB(t, f.db, stage, committed)
				f.op.store = newAuthorizedIntentTestStore(t, faultDB)
				finished := make(chan error, 1)
				go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
				_, _ = readCleanupRequest(t, f.peer)
				byeA, addressA := readCleanupRequest(t, f.peer)
				_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(byeA, 200, "OK", nil).String()), addressA)
				require.NoError(t, err)
				if stage >= 8 {
					ack, _ := readCleanupRequest(t, peerB)
					require.Equal(t, sip.ACK, ack.Method)
				}
				if stage >= 9 {
					bye, address := readCleanupRequest(t, peerB)
					require.Equal(t, sip.BYE, bye.Method)
					_, err = peerB.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
					require.NoError(t, err)
				}
				require.Error(t, <-finished)
				waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
				require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "A success cannot release unresolved B")
				stop()
				require.NoError(t, f.op.CloseLocal(ctx))
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				err = f.op.CleanupKnownBranch(ctx)
				if stage >= 9 {
					require.NoError(t, err, "only persist this process's actual B response")
				} else {
					require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
				}
				f.noACK(t)
				require.NoError(t, peerB.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
				_, _, err = peerB.ReadFrom(make([]byte, 8192))
				require.Error(t, err, "retry cannot resend the additional branch")
				stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
				require.NoError(t, err)
				require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
				require.Equal(t, playauth.SIPCleanupBYEObserved, stored.Steps[0].KnownBranch.CleanupAttempts[0].State)
			})
		}
	}
}

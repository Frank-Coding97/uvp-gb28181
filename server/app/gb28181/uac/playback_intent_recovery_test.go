package uac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackRecoveryRejectsLiveOwnerAndDifferentBarrier(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	ctx := context.Background()
	for _, barrier := range []*playauth.DeviceOperationBarrier{f.barrier, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(f.db))} {
		r, err := f.u.beginRecoveredPlaybackCleanup(ctx, playauth.NewDeviceOperationIntentStore(f.db), barrier, f.id, f.op.invite.StepID, "owned-device")
		require.Error(t, err)
		require.Nil(t, r)
	}
	require.NoError(t, f.op.CloseLocal(ctx))
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, f.op.invite.StepID, "owned-device")
	require.Error(t, err, "local release does not remove original strong observation owner")
	require.Nil(t, r)
	f.noACK(t)
}

// This fixture starts from durable known-branch material, not a claimed
// process restart. Restart semantics require a separate OS-process test.
func recoveredPlaybackUDPFixture(t *testing.T) (*playbackOperationUDPFixture, string) {
	t.Helper()
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.LocalAddr().String(), "UDP"
	ctx := context.Background()
	request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, i.StepID)
	require.NoError(t, err)
	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	response.To().Params.Add("tag", "recovery-remote")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
	_, err = observeStoredPlaybackBranch(ctx, store, id, 4, i, request, response)
	require.NoError(t, err)
	return &playbackOperationUDPFixture{u: u, db: db, store: store, id: id, peer: peer,
		barrier: playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))}, i.StepID
}

func TestPlaybackRecoveryReservationSingleWinner(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	winners := make(chan *playbackIntentRecovery, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
			if err == nil {
				winners <- r
			}
		}()
	}
	wg.Wait()
	close(winners)
	require.Len(t, winners, 1)
	r := <-winners
	defer r.CloseLocal(ctx)
	_, err := f.u.beginPlaybackIntentOperation(ctx, f.store, f.barrier, f.id, 5, strings.Repeat("c", 32), validPlaybackInvite())
	require.Error(t, err)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, loaded.Steps[0].KnownBranch.CleanupAttempts, "reservation does not prepare or send")
	require.NoError(t, r.CloseLocal(ctx))
	_, err = f.u.beginRecoveredPlaybackCleanup(ctx, f.store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(f.db)), f.id, stepID, "recovery-remote")
	require.Error(t, err, "binding cannot silently replace the shared barrier")
	f.noACK(t)
}

func TestPlaybackRecoveryActualUDPAndRepeatCannotResend(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	ack, _ := readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, bye.Method)
	waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	stop()
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, <-result)
	require.NoError(t, r.Run(ctx), "repeat can join/persist this attempt only")
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	a := loaded.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, a, 1)
	require.NotNil(t, a[0].Response)
	require.NotNil(t, a[0].LocalQuiescedAt)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	f.noACK(t)
}

func TestPlaybackRecoveryCloseInterruptsWithoutResend(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	readCleanupRequest(t, f.peer)
	readCleanupRequest(t, f.peer)
	closeCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	require.ErrorIs(t, r.CloseLocal(closeCtx), ErrPlaybackCleanupUnknown)
	require.Error(t, <-result)
	require.NoError(t, f.barrier.WaitBefore(closeCtx, 1, 2))
	require.ErrorIs(t, r.Run(ctx), ErrPlaybackCleanupUnknown)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	a := loaded.Steps[0].KnownBranch.CleanupAttempts
	require.Len(t, a, 1)
	require.Nil(t, a[0].Response)
	require.NotNil(t, a[0].LocalQuiescedAt)
	f.noACK(t)
}

func TestPlaybackRecoveryUnknownCommitRetainsUntilFacts(t *testing.T) {
	for _, stage := range []int32{1, 2, 3, 4, 5} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage=%d/commit=%v", stage, committed), func(t *testing.T) {
				f, stepID := recoveredPlaybackUDPFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: stage, commitFirst: committed}
				r, err := f.u.beginRecoveredPlaybackCleanup(ctx, playauth.NewDeviceOperationIntentStore(faultDB), f.barrier, f.id, stepID, "recovery-remote")
				require.NoError(t, err)
				defer r.CloseLocal(ctx)
				result := make(chan error, 1)
				go func() { result <- r.Run(ctx) }()
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
				require.Error(t, <-result)
				if stage >= 4 {
					next, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
					require.Error(t, err, "unconfirmed facts retain the strong reservation")
					require.Nil(t, next)
					waitCtx, stop := context.WithTimeout(ctx, 15*time.Millisecond)
					require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
					stop()
					require.NoError(t, r.Run(ctx), "only confirm this attempt's facts")
				} else {
					require.ErrorIs(t, r.Run(ctx), ErrPlaybackCleanupUnknown)
				}
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				f.u.playbackIntentMu.Lock()
				require.NotContains(t, f.u.playbackRecoveries, f.id.OperationID)
				f.u.playbackIntentMu.Unlock()
				f.noACK(t)
			})
		}
	}
}

func TestPlaybackRecoveryReservesBeforeLoad(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("fixture:recovery-load", func(*gorm.DB) {
		once.Do(func() { close(entered); <-release })
	}))
	defer f.db.Callback().Query().Remove("fixture:recovery-load")
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	result := make(chan *playbackIntentRecovery, 1)
	go func() {
		r, _ := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
		result <- r
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.Error(t, err)
	require.Nil(t, r)
	_, err = f.u.beginPlaybackIntentOperation(ctx, f.store, f.barrier, f.id, 5, strings.Repeat("c", 32), validPlaybackInvite())
	require.Error(t, err)
	close(release)
	r = <-result
	require.NotNil(t, r)
	require.NoError(t, r.CloseLocal(ctx))
	f.noACK(t)
}

func TestPlaybackRecoveryActualTCP(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.Addr().String(), "TCP"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request, prepared, err := u.prepareStoredPlaybackInvite(ctx, store, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	i := prepared.Steps[0].Identity
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, i.StepID)
	require.NoError(t, err)
	response := sip.NewResponseFromRequest(request, 200, "OK", nil)
	response.To().Params.Add("tag", "recovery-tcp")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.Addr().(*net.TCPAddr).Port}})
	_, err = observeStoredPlaybackBranch(ctx, store, id, 4, i, request, response)
	require.NoError(t, err)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	r, err := u.beginRecoveredPlaybackCleanup(ctx, store, barrier, id, i.StepID, "recovery-tcp")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
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
		request := pending[0].(*sip.Request)
		pending = pending[1:]
		return request
	}
	require.Equal(t, sip.ACK, read().Method)
	bye := read()
	require.Equal(t, sip.BYE, bye.Method)
	_, err = conn.Write([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()))
	require.NoError(t, err)
	require.NoError(t, <-result)
	require.NoError(t, r.Run(ctx))
	require.Empty(t, pending)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	buffer := make([]byte, 8192)
	n, err := conn.Read(buffer)
	require.Zero(t, n, "repeat must not send SIP")
	require.Error(t, err)
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps[0].KnownBranch.CleanupAttempts, 1)
	require.NotNil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].Response)
	require.NotNil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].LocalQuiescedAt)
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
}

func TestPlaybackRecoveryCapacityAndLoadFailure(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	f.u.playbackRecoveries = make(map[string]*playbackIntentRecovery)
	for n := 0; n < maxPlaybackIntentOperations; n++ {
		f.u.playbackRecoveries[fmt.Sprint(n)] = &playbackIntentRecovery{}
	}
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.Error(t, err)
	require.Nil(t, r)
	_, err = f.u.beginPlaybackIntentOperation(ctx, f.store, f.barrier, f.id, 5, strings.Repeat("c", 32), validPlaybackInvite())
	require.Error(t, err, "capacity is shared with original owners")
	require.Len(t, f.u.playbackRecoveries, maxPlaybackIntentOperations, "never evict a strong owner")
	f.u.playbackRecoveries = nil // Only inert test entries, not real owners.
	r, err = f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "missing-remote")
	require.Error(t, err)
	require.Nil(t, r)
	require.Empty(t, f.u.playbackRecoveries, "failed pure Load releases its reservation")
	r, err = f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	require.NoError(t, r.CloseLocal(ctx))
	require.ErrorIs(t, r.Run(ctx), ErrPlaybackCleanupUnknown, "unused closed preparation never sends")
	f.noACK(t)
}

func TestPlaybackRecoveryBranchSuccessPreservesInventoryUnknown(t *testing.T) {
	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := f.store.ObserveSIPBranchInventoryFault(ctx, f.id, 5, stepID, playauth.SIPBranchObserverIncomplete)
	require.NoError(t, err)
	r, err := f.u.beginRecoveredPlaybackCleanup(ctx, f.store, f.barrier, f.id, stepID, "recovery-remote")
	require.NoError(t, err)
	defer r.CloseLocal(ctx)
	result := make(chan error, 1)
	go func() { result <- r.Run(ctx) }()
	readCleanupRequest(t, f.peer)
	bye, address := readCleanupRequest(t, f.peer)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.ErrorIs(t, <-result, ErrPlaybackCleanupUnknown)
	require.ErrorIs(t, r.Run(ctx), ErrPlaybackCleanupUnknown)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
	require.NotNil(t, loaded.Steps[0].KnownBranch.CleanupAttempts[0].Response)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	f.noACK(t)
}

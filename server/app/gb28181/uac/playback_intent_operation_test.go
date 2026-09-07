package uac

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type playbackOperationUDPFixture struct {
	u       *UAC
	db      *gorm.DB
	store   *playauth.DeviceOperationIntentStore
	id      playauth.DeviceOperationIntentIdentity
	barrier *playauth.DeviceOperationBarrier
	op      *playbackIntentOperation
	peer    net.PacketConn
	address net.Addr
	invite  *sip.Request
}

func newPlaybackOperationUDPFixture(t *testing.T, options ...sipgo.UserAgentOption) *playbackOperationUDPFixture {
	t.Helper()
	f := newPlaybackOperationPreparedUDPFixture(t, options...)
	require.NoError(t, f.op.Start(context.Background()))
	buffer := make([]byte, 8192)
	require.NoError(t, f.peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, address, err := f.peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	f.address, f.invite = address, message.(*sip.Request)
	return f
}

func newPlaybackOperationPreparedUDPFixture(t *testing.T, options ...sipgo.UserAgentOption) *playbackOperationUDPFixture {
	t.Helper()
	u, db, store, id, _ := playbackIntentStoreFixture(t, options...)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.LocalAddr().String(), "UDP"
	op, err := u.beginPlaybackIntentOperation(context.Background(), store, barrier, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = op.CloseLocal(ctx)
	})
	u.playbackIntentMu.Lock()
	require.Same(t, op, u.playbackIntents[id.OperationID], "strong owner precedes first wire write")
	u.playbackIntentMu.Unlock()
	return &playbackOperationUDPFixture{u: u, db: db, store: store, id: id, barrier: barrier, op: op, peer: peer}
}

func (f *playbackOperationUDPFixture) respond(t *testing.T, status int) {
	t.Helper()
	response := sip.NewResponseFromRequest(f.invite, status, "fixture", nil)
	response.To().Params.Add("tag", "owned-device")
	if status >= 200 && status < 300 {
		response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: f.peer.LocalAddr().(*net.UDPAddr).Port}})
	}
	_, err := f.peer.WriteTo([]byte(response.String()), f.address)
	require.NoError(t, err)
}

func (f *playbackOperationUDPFixture) noACK(t *testing.T) {
	t.Helper()
	require.NoError(t, f.peer.SetReadDeadline(time.Now().Add(80*time.Millisecond)))
	buffer := make([]byte, 8192)
	for {
		n, _, err := f.peer.ReadFrom(buffer)
		if err != nil {
			var timeout net.Error
			require.ErrorAs(t, err, &timeout)
			require.True(t, timeout.Timeout())
			return
		}
		message, err := sip.ParseMessage(buffer[:n])
		require.NoError(t, err)
		require.Equal(t, sip.INVITE, message.(*sip.Request).Method, "only original transaction retransmission is allowed")
	}
}

func TestPlaybackIntentOperationPumpRetainsFinalWithoutConsumer(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	for _, code := range []int{100, 180, 183, 200} {
		f.respond(t, code)
	}
	require.Eventually(t, func() bool {
		f.op.factMu.Lock()
		defer f.op.factMu.Unlock()
		return f.op.first != nil
	}, time.Second, time.Millisecond, "a busy workflow must not block the sole SIP response reader")
	require.NoError(t, f.op.CloseLocal(context.Background()))
	loaded, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].KnownBranch)
	require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[0].KnownBranch.ACKState)
	f.noACK(t)
}

func TestPlaybackIntentOperationBranchPersistenceFailureRetainsLease(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	require.NoError(t, f.db.Exec(`CREATE TRIGGER deny_owner_observe BEFORE UPDATE ON gb_device_operation_intent BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`).Error)
	f.respond(t, 200)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := f.op.ReadAndAccept(ctx)
	require.Error(t, err)
	f.noACK(t)
	require.Error(t, f.op.CloseLocal(ctx))
	waitCtx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	stop()
	require.NoError(t, f.db.Exec("DROP TRIGGER deny_owner_observe").Error)
	require.NoError(t, f.op.CloseLocal(context.Background()))
	require.NoError(t, f.op.CloseLocal(context.Background()), "recovery is repeatable, lease release is once-only")
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	_, err = f.op.ReadAndAccept(ctx)
	require.True(t, err != nil && !errors.Is(err, context.DeadlineExceeded), "recovery never grants a fresh business attempt")
	f.noACK(t)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[0].KnownBranch.ACKState)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
}

type playbackOperationCommitFault struct {
	gorm.ConnPool
	commits     atomic.Int32
	failAt      int32
	commitFirst bool
}

type playbackOperationFaultTx struct {
	*sql.Tx
	pool *playbackOperationCommitFault
}

func (p *playbackOperationCommitFault) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(*sql.DB).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &playbackOperationFaultTx{tx, p}, nil
}

func (p *playbackOperationCommitFault) GetDBConn() (*sql.DB, error) {
	return p.ConnPool.(*sql.DB), nil
}

func (tx *playbackOperationFaultTx) Commit() error {
	if tx.pool.commits.Add(1) != tx.pool.failAt {
		return tx.Tx.Commit()
	}
	if tx.pool.commitFirst {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	}
	return errors.New("fixture lost commit reply")
}

func TestPlaybackIntentOperationUnknownCommitNeverWritesACK(t *testing.T) {
	for _, step := range []int32{1, 2} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("step=%d/committed=%v", step, committed), func(t *testing.T) {
				f := newPlaybackOperationUDPFixture(t)
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
				faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: step, commitFirst: committed}
				f.op.store = playauth.NewDeviceOperationIntentStore(faultDB)
				f.respond(t, 200)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := f.op.ReadAndAccept(ctx)
				require.Error(t, err)
				f.noACK(t)
				f.op.store = f.store
				require.NoError(t, f.op.CloseLocal(ctx))
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
				require.NoError(t, err)
				want := playauth.SIPStepPrepared
				if step == 2 && committed {
					want = playauth.SIPStepMayHaveDispatched
				}
				require.Equal(t, want, loaded.Steps[0].KnownBranch.ACKState)
				require.Error(t, f.op.Start(ctx))
				f.noACK(t)
			})
		}
	}
}

func TestPlaybackIntentOperationTransferCANCELRetainsLateSuccess(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	f.respond(t, 180)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	guard, err := f.barrier.LockTransfer(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	guard.Commit(2)
	guard.Release()
	buffer := make([]byte, 8192)
	require.NoError(t, f.peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, address, err := f.peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	cancelRequest := message.(*sip.Request)
	require.Equal(t, sip.CANCEL, cancelRequest.Method)
	require.Equal(t, string(*f.invite.CallID()), string(*cancelRequest.CallID()))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, loaded.Steps[0].Cancel.State)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(cancelRequest, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	waitCtx, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "CANCEL 200 alone cannot release the original operation")
	stop()
	f.respond(t, 200)
	cleanupACK, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, cleanupACK.Method)
	cleanupBYE, cleanupAddress := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.BYE, cleanupBYE.Method)
	waitCleanupCtx, stopCleanupWait := context.WithTimeout(ctx, 20*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCleanupCtx, 1, 2), context.DeadlineExceeded, "cleanup owner overlaps the original lease")
	stopCleanupWait()
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(cleanupBYE, 200, "OK", nil).String()), cleanupAddress)
	require.NoError(t, err)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2), "supervisor joins actual cleanup and persists its exact facts")
	loaded, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].KnownBranch)
	require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[0].KnownBranch.ACKState, "original business ACK remains forbidden")
	require.Len(t, loaded.Steps[0].KnownBranch.CleanupAttempts, 1)
	require.Equal(t, playauth.SIPCleanupBYEObserved, loaded.Steps[0].KnownBranch.CleanupAttempts[0].State)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
	f.noACK(t)
	f.u.playbackIntentMu.Lock()
	require.Same(t, f.op, f.u.playbackIntents[f.id.OperationID])
	f.u.playbackIntentMu.Unlock()
}

func TestPlaybackIntentOperationRegistryCapacityDoesNotEvict(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	retained := &playbackIntentOperation{}
	u.playbackIntents = make(map[string]*playbackIntentOperation)
	for i := 0; i < maxPlaybackIntentOperations; i++ {
		u.playbackIntents[fmt.Sprintf("%032x", i)] = retained
	}
	op, err := u.beginPlaybackIntentOperation(context.Background(), store, playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), id, 2, strings.Repeat("b", 32), validPlaybackInvite())
	require.Error(t, err)
	require.Nil(t, op)
	require.Len(t, u.playbackIntents, maxPlaybackIntentOperations)
	for _, entry := range u.playbackIntents {
		require.Same(t, retained, entry)
	}
	loaded, err := store.LoadSIPInviteSteps(context.Background(), id)
	require.NoError(t, err)
	require.Empty(t, loaded.Steps, "reject before creating more durable work or SIP handles")
}

func TestPlaybackIntentOperationFinalBeforeCancelDoesNotDispatch(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	f.respond(t, 200)
	require.Eventually(t, func() bool {
		f.op.factMu.Lock()
		defer f.op.factMu.Unlock()
		return f.op.first != nil
	}, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, f.op.Cancel(ctx), ErrPlaybackCleanupUnknown)
	f.noACK(t)
	require.NoError(t, f.op.CloseLocal(ctx))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].Cancel)
	require.NotNil(t, loaded.Steps[0].KnownBranch)
}

func TestPlaybackIntentOperationRejectedInviteOwnsTransactionACK(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	f.respond(t, 486)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := f.op.ReadAndAccept(ctx)
	require.ErrorIs(t, err, ErrPlaybackRejected)
	buffer := make([]byte, 8192)
	require.NoError(t, f.peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err := f.peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	ack := message.(*sip.Request)
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, string(*f.invite.CallID()), string(*ack.CallID()))
	require.Equal(t, f.invite.Via().Params.GetOr("branch", ""), ack.Via().Params.GetOr("branch", ""), "non-2xx ACK belongs to the original concrete INVITE transaction")
	require.NoError(t, f.op.CloseLocal(ctx))
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].KnownBranch)
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
}

func TestPlaybackIntentOperationBlockedPersistenceCannotReleaseLease(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	var intercepted atomic.Bool
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture_block_observe", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device_operation_intent" && intercepted.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}))
	f.respond(t, 200)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	accepted := make(chan error, 1)
	go func() { _, err := f.op.ReadAndAccept(ctx); accepted <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("Observe did not enter the real DB transaction")
	}
	f.noACK(t)
	closeCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	require.ErrorIs(t, f.op.CloseLocal(closeCtx), context.DeadlineExceeded)
	stop()
	waitCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded)
	stop()
	released.Do(func() { close(release) })
	require.Error(t, <-accepted, "late DB success cannot restore network permission after close")
	require.NoError(t, f.op.CloseLocal(ctx))
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.noACK(t)
}

func TestPlaybackIntentOperationActualTCPCommitBeforeACK(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
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
	connection, err := peer.Accept()
	require.NoError(t, err)
	defer connection.Close()
	buffer := make([]byte, 8192)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, err = connection.Read(buffer)
	require.Error(t, err, "connection preparation contains no SIP bytes")
	require.NoError(t, op.Start(ctx))
	parser := sip.NewParser().NewSIPStream()
	defer parser.Close()
	read := func() *sip.Request {
		t.Helper()
		require.NoError(t, connection.SetReadDeadline(time.Now().Add(time.Second)))
		var messages []sip.Message
		for len(messages) == 0 {
			n, err := connection.Read(buffer)
			require.NoError(t, err)
			err = parser.ParseSIPStream(buffer[:n], func(message sip.Message) { messages = append(messages, message) })
			if !errors.Is(err, sip.ErrParseSipPartial) {
				require.NoError(t, err)
			}
		}
		require.Len(t, messages, 1)
		return messages[0].(*sip.Request)
	}
	invite := read()
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, loaded.Steps[0].State)
	require.Equal(t, string(*invite.CallID()), loaded.Steps[0].Identity.CallID)
	response := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	response.To().Params.Add("tag", "owned-tcp")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.Addr().(*net.TCPAddr).Port}})
	_, err = connection.Write([]byte(response.String()))
	require.NoError(t, err)
	metadata, err := op.ReadAndAccept(ctx)
	require.NoError(t, err)
	require.Equal(t, "owned-tcp", metadata.RemoteTag)
	require.Equal(t, sip.ACK, read().Method)
	loaded, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, loaded.Steps[0].KnownBranch.ACKState)
	require.NoError(t, op.CloseLocal(ctx))
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
	require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
}

func TestPlaybackIntentOperationActualUDPCommitBeforeACK(t *testing.T) {
	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peer.LocalAddr().String(), "UDP"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	operation, err := u.beginPlaybackIntentOperation(ctx, store, barrier, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	defer operation.CloseLocal(context.Background())
	require.NoError(t, operation.Start(ctx))
	buffer := make([]byte, 8192)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, address, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	invite := message.(*sip.Request)
	stored, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, stored.Steps[0].State)
	require.Equal(t, string(*invite.CallID()), stored.Steps[0].Identity.CallID)
	response := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	response.To().Params.Add("tag", "owned-device")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
	_, err = peer.WriteTo([]byte(response.String()), address)
	require.NoError(t, err)
	metadata, err := operation.ReadAndAccept(ctx)
	require.NoError(t, err)
	require.Equal(t, "owned-device", metadata.RemoteTag)
	require.Nil(t, u.playbackDialogs.get(string(*invite.CallID())), "durable owner is never exposed to legacy Do/INFO/BYE")
	stored, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepMayHaveDispatched, stored.Steps[0].KnownBranch.ACKState)
	n, _, err = peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err = sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	require.Equal(t, sip.ACK, message.(*sip.Request).Method)
	require.Error(t, operation.Start(ctx), "operation is not a retryable business command")
	require.NoError(t, operation.CloseLocal(ctx))
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2), "local tx gone and persisted facts retained")
	stored, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State, "local release never completes remote cleanup")
}

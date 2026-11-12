package sipgo

import (
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

type dialogServerTestTx struct {
	done          chan struct{}
	acks          chan *sip.Request
	responseTimes chan time.Time
	doneOnce      sync.Once

	mu          sync.Mutex
	err         error
	onTerminate sip.FnTxTerminate
}

func newDialogServerTestTx() *dialogServerTestTx {
	return &dialogServerTestTx{
		done:          make(chan struct{}),
		acks:          make(chan *sip.Request),
		responseTimes: make(chan time.Time, 128),
	}
}

func (tx *dialogServerTestTx) Respond(*sip.Response) error {
	tx.responseTimes <- time.Now()
	return nil
}

func (tx *dialogServerTestTx) Terminate() {
	tx.closeWithError(sip.ErrTransactionTerminated)
}

func (tx *dialogServerTestTx) OnTerminate(f sip.FnTxTerminate) bool {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	select {
	case <-tx.done:
		return false
	default:
	}
	tx.onTerminate = f
	return true
}

func (tx *dialogServerTestTx) Done() <-chan struct{} {
	return tx.done
}

func (tx *dialogServerTestTx) Err() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.err
}

func (tx *dialogServerTestTx) Acks() <-chan *sip.Request {
	return tx.acks
}

func (tx *dialogServerTestTx) OnCancel(sip.FnTxCancel) bool {
	return true
}

func (tx *dialogServerTestTx) closeWithError(err error) {
	tx.doneOnce.Do(func() {
		tx.mu.Lock()
		tx.err = err
		onTerminate := tx.onTerminate
		close(tx.done)
		tx.mu.Unlock()
		if onTerminate != nil {
			onTerminate("dialog-server-test", err)
		}
	})
}

func newDialogServerAckTestSession(t *testing.T) (*DialogServerSession, *dialogServerTestTx) {
	t.Helper()
	invite, _, _ := createTestInvite(t, "sip:uas@127.0.0.1", "udp", "127.0.0.1:5090")
	invite.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "uac", Host: "127.0.0.1", Port: 5090}})

	tx := newDialogServerTestTx()
	ua := DialogUA{
		ContactHDR: sip.ContactHeader{Address: sip.Uri{User: "uas", Host: "127.0.0.1", Port: 5060}},
	}
	dialog, err := ua.ReadInvite(invite, tx)
	require.NoError(t, err)
	return dialog, tx
}

func setDialogServerTestTimers(t *testing.T, t1, t2 time.Duration) {
	t.Helper()
	oldT1, oldT2, oldT4 := sip.T1, sip.T2, sip.T4
	sip.SetTimers(t1, t2, oldT4)
	t.Cleanup(func() {
		sip.SetTimers(oldT1, oldT2, oldT4)
	})
}

func startDialogServerWriteResponse(t *testing.T, dialog *DialogServerSession) (*sip.Response, <-chan error) {
	t.Helper()
	res := sip.NewResponseFromRequest(dialog.InviteRequest, sip.StatusOK, "OK", nil)
	done := make(chan error, 1)
	go func() {
		done <- dialog.WriteResponse(res)
	}()
	return res, done
}

func TestDialogServerWriteResponseAckDeadlineIsFixed(t *testing.T) {
	setDialogServerTestTimers(t, 3*time.Millisecond, 12*time.Millisecond)
	dialog, tx := newDialogServerAckTestSession(t)
	_, done := startDialogServerWriteResponse(t, dialog)

	select {
	case <-tx.responseTimes:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("initial 2xx response was not sent")
	}

	select {
	case err := <-done:
		require.Error(t, err)
		require.Equal(t, sip.DialogStateEstablished, dialog.LoadState())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WriteResponse did not stop at its ACK deadline")
	}
}

func TestDialogServerWriteResponseStopsWhenTransactionDone(t *testing.T) {
	dialog, tx := newDialogServerAckTestSession(t)
	_, done := startDialogServerWriteResponse(t, dialog)

	select {
	case <-tx.responseTimes:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("initial 2xx response was not sent")
	}

	tx.Terminate()
	select {
	case err := <-done:
		require.ErrorIs(t, err, sip.ErrTransactionTerminated)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WriteResponse did not stop when the transaction ended")
	}
}

func TestDialogServerWriteResponseRetransmitsWithExponentialBackoff(t *testing.T) {
	setDialogServerTestTimers(t, 20*time.Millisecond, 70*time.Millisecond)
	dialog, tx := newDialogServerAckTestSession(t)
	res, done := startDialogServerWriteResponse(t, dialog)

	times := make([]time.Time, 0, 5)
	for range 5 {
		select {
		case sentAt := <-tx.responseTimes:
			times = append(times, sentAt)
		case <-time.After(300 * time.Millisecond):
			t.Fatal("2xx response retransmission did not arrive")
		}
	}

	secondInterval := times[2].Sub(times[1])
	require.GreaterOrEqual(t, secondInterval, 25*time.Millisecond)
	require.Less(t, secondInterval, 60*time.Millisecond)

	thirdInterval := times[3].Sub(times[2])
	require.GreaterOrEqual(t, thirdInterval, 50*time.Millisecond)
	require.Less(t, thirdInterval, 100*time.Millisecond)

	fourthInterval := times[4].Sub(times[3])
	require.GreaterOrEqual(t, fourthInterval, 50*time.Millisecond)
	require.Less(t, fourthInterval, 110*time.Millisecond)

	ack := newAckRequestUAC(dialog.InviteRequest, res, nil)
	require.NoError(t, dialog.ReadAck(ack, tx))
	require.NoError(t, <-done)
	require.Equal(t, sip.DialogStateConfirmed, dialog.LoadState())
}

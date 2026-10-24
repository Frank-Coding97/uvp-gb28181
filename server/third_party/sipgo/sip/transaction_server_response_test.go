package sip

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerTCPResponseDoesNotReportSuccessfulTimerJAsFailure(t *testing.T) {
	// Timer J is zero for reliable transports. It can run immediately after
	// the FSM sends the response, before Respond inspects the outcome.
	for i := 0; i < 10000; i++ {
		req := testCreateRequest(t, "MESSAGE", "sip:example.com", "TCP", "127.0.0.1:5060")
		conn := &validatorTestConnection{}
		tx := NewServerTx("response", req, conn, slog.Default())
		require.NoError(t, tx.Init())
		err := tx.Respond(NewResponseFromRequest(req, StatusOK, "OK", nil))
		<-tx.Done()
		require.Len(t, conn.messages, 1)
		require.NoError(t, err, "successfully sent response must not depend on timer scheduling")
		require.ErrorIs(t, tx.Err(), ErrTransactionTerminated)
		// A later call really is operating on an ended transaction.
		require.ErrorIs(t, tx.Respond(NewResponseFromRequest(req, StatusOK, "OK", nil)), ErrTransactionTerminated)
	}
}

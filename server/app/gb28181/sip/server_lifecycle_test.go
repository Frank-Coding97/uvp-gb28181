package sip

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoggingRequestBusinessGateClosesAdmissionButJoinsAcceptedWork(t *testing.T) {
	gate := &requestBusinessGate{}
	require.True(t, gate.enter())
	gate.close()
	require.False(t, gate.enter())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, gate.wait(ctx), context.Canceled)

	gate.leave()
	require.NoError(t, gate.wait(context.Background()))
}

func TestLoggingShutdownReportsIncompleteBusinessAndKeepsTheSameResult(t *testing.T) {
	server, err := NewServer(testConfig())
	require.NoError(t, err)
	require.True(t, server.businessWork.enter())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	firstErr := server.Shutdown(ctx)
	require.Error(t, firstErr)
	require.True(t, errors.Is(firstErr, context.DeadlineExceeded))

	server.businessWork.leave()
	require.ErrorIs(t, server.Shutdown(context.Background()), context.DeadlineExceeded)
}

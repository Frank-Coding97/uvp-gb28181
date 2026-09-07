//go:build windows

package controlpipe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/require"
)

var pipeTestSequence uint64

func TestAuthenticatedPipeRoundTrip(t *testing.T) {
	const secret = "round-trip-secret"
	listener, name := requirePipeListenerNamed(t, secret)
	acceptResult := acceptPipe(listener)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := Dial(ctx, name, secret)
	require.NoError(t, err)
	defer client.Close()

	accepted := <-acceptResult
	require.NoError(t, accepted.err)
	require.NotNil(t, accepted.conn)
	defer accepted.conn.Close()

	message := []byte("authenticated control data")
	_, err = client.Write(message)
	require.NoError(t, err)
	got := make([]byte, len(message))
	_, err = io.ReadFull(accepted.conn, got)
	require.NoError(t, err)
	require.Equal(t, message, got)

	_, err = accepted.conn.Write(message)
	require.NoError(t, err)
	got = make([]byte, len(message))
	_, err = io.ReadFull(client, got)
	require.NoError(t, err)
	require.Equal(t, message, got)
}

func TestWrongSecretIsRejectedOnBothSides(t *testing.T) {
	const secret = "correct-secret"
	listener, name := requirePipeListenerNamed(t, secret)
	acceptResult := acceptPipe(listener)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := Dial(ctx, name, "wrong-secret")
	require.Nil(t, client)
	require.ErrorIs(t, err, ErrAuthentication)
	require.NotContains(t, err.Error(), secret)

	accepted := <-acceptResult
	require.Nil(t, accepted.conn)
	require.ErrorIs(t, accepted.err, ErrAuthentication)
	require.NotContains(t, accepted.err.Error(), secret)
}

func TestDialRejectsFakeServerProof(t *testing.T) {
	const secret = "client-secret"
	rawListener, name := requireRawPipeListener(t)
	serverResult := make(chan error, 1)
	go func() {
		conn, err := rawListener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		defer conn.Close()
		hello, err := readFrame(conn)
		if err != nil {
			serverResult <- err
			return
		}
		if len(hello) != 1+controlNonceSize || hello[0] != frameHello {
			serverResult <- ErrMalformedFrame
			return
		}
		var serverNonce [controlNonceSize]byte
		if _, err := io.ReadFull(rand.Reader, serverNonce[:]); err != nil {
			serverResult <- err
			return
		}
		challenge := append([]byte{frameChallenge}, serverNonce[:]...)
		if err := writeFrame(conn, challenge); err != nil {
			serverResult <- err
			return
		}
		if _, err := readFrame(conn); err != nil {
			serverResult <- err
			return
		}
		bogusProof := append([]byte{frameServerProof}, make([]byte, sha256.Size)...)
		serverResult <- writeFrame(conn, bogusProof)
	}()
	t.Cleanup(func() { _ = rawListener.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Dial(ctx, name, secret)
	require.Nil(t, conn)
	require.ErrorIs(t, err, ErrAuthentication)
	require.NotContains(t, err.Error(), secret)
	require.NoError(t, <-serverResult)
}

func TestServerRejectsMalformedAndOversizeFrames(t *testing.T) {
	tests := []struct {
		name       string
		writeFrame func(net.Conn) error
		want       error
	}{
		{
			name: "malformed frame",
			writeFrame: func(conn net.Conn) error {
				return writeAll(conn, []byte{0, 0, 0, 1, frameHello})
			},
			want: ErrMalformedFrame,
		},
		{
			name: "oversize frame",
			writeFrame: func(conn net.Conn) error {
				header := make([]byte, frameHeaderSize)
				binary.BigEndian.PutUint32(header, maxHandshakeFrame+1)
				return writeAll(conn, header)
			},
			want: ErrFrameTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, name := requirePipeListenerNamed(t, "malformed-secret")
			acceptResult := acceptPipe(listener)
			path, err := pipePath(name)
			require.NoError(t, err)
			raw, err := winio.DialPipeContext(context.Background(), path)
			require.NoError(t, err)

			require.NoError(t, tt.writeFrame(raw))
			accepted := <-acceptResult
			_ = raw.Close()
			require.Nil(t, accepted.conn)
			require.ErrorIs(t, accepted.err, tt.want)
		})
	}
}

func TestExistingPipeNameFailsClosed(t *testing.T) {
	listener, name := requirePipeListenerNamed(t, "first-instance-secret")
	defer listener.Close()

	second, err := Listen(name, "first-instance-secret")
	require.Nil(t, second)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "first-instance-secret")
}

func TestListenerCloseInterruptsAccept(t *testing.T) {
	listener, _ := requirePipeListenerNamed(t, "close-secret")
	acceptResult := acceptPipe(listener)

	require.NoError(t, listener.Close())
	select {
	case accepted := <-acceptResult:
		require.Nil(t, accepted.conn)
		require.ErrorIs(t, accepted.err, net.ErrClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after listener Close")
	}
}

func TestServerHandshakeDeadlineIsBounded(t *testing.T) {
	listener, name := requirePipeListenerNamed(t, "deadline-secret")
	acceptResult := acceptPipe(listener)
	path, err := pipePath(name)
	require.NoError(t, err)
	raw, err := winio.DialPipeContext(context.Background(), path)
	require.NoError(t, err)
	start := time.Now()

	select {
	case accepted := <-acceptResult:
		elapsed := time.Since(start)
		_ = raw.Close()
		require.Nil(t, accepted.conn)
		require.Error(t, accepted.err)
		var netErr net.Error
		require.ErrorAs(t, accepted.err, &netErr)
		require.True(t, netErr.Timeout())
		require.Less(t, elapsed, 8*time.Second)
	case <-time.After(8 * time.Second):
		_ = raw.Close()
		t.Fatal("server handshake deadline exceeded bound")
	}
}

func TestDialContextCancelsHandshake(t *testing.T) {
	rawListener, name := requireRawPipeListener(t)
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := rawListener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	t.Cleanup(func() { _ = rawListener.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	conn, err := Dial(ctx, name, "context-secret")
	require.Nil(t, conn)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case conn := <-accepted:
		_ = conn.Close()
	case err := <-acceptErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("raw server did not observe the client")
	}
}

func TestPipeListenerUsesEffectiveUserAndSystemSDDL(t *testing.T) {
	sid, err := effectiveUserSIDString()
	require.NoError(t, err)
	require.NotEmpty(t, sid)

	listener, _ := requirePipeListenerNamed(t, "sddl-secret")
	defer listener.Close()
	require.Equal(t, "D:P(A;;GA;;;"+sid+")(A;;GA;;;SY)", protectedPipeSDDL(sid))
}

type acceptPipeResult struct {
	conn net.Conn
	err  error
}

func acceptPipe(listener net.Listener) <-chan acceptPipeResult {
	result := make(chan acceptPipeResult, 1)
	go func() {
		conn, err := listener.Accept()
		result <- acceptPipeResult{conn: conn, err: err}
	}()
	return result
}

func requirePipeListenerNamed(t *testing.T, secret string) (net.Listener, string) {
	name := pipeTestName()
	listener, err := Listen(name, secret)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return listener, name
}

func requireRawPipeListener(t *testing.T) (net.Listener, string) {
	name := pipeTestName()
	path, err := pipePath(name)
	require.NoError(t, err)
	sid, err := effectiveUserSIDString()
	require.NoError(t, err)
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: protectedPipeSDDL(sid),
	})
	require.NoError(t, err)
	return listener, name
}

func pipeTestName() string {
	return fmt.Sprintf("test-%d", atomic.AddUint64(&pipeTestSequence, 1))
}

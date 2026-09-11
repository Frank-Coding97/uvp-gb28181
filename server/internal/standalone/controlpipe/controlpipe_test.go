package controlpipe

import (
	"context"
	"errors"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPipePathUsesPrivateControlDomain(t *testing.T) {
	path, err := pipePath("local-control-1")

	require.NoError(t, err)
	require.Equal(t, `\\.\pipe\uvp-standalone-control-v1-local-control-1`, path)

	_, err = pipePath("ready")
	require.NoError(t, err)
	require.NotContains(t, path, "ready")
}

func TestPipeNameRejectsAmbiguousWindowsNames(t *testing.T) {
	for _, name := range []string{
		"",
		".",
		"..",
		"-leading",
		"with space",
		"with/slash",
		`with\slash`,
		"with:colon",
		"trailing.",
		"trailing ",
		"CON",
		"con.txt",
		"COM1",
		"LPT9.log",
	} {
		_, err := pipePath(name)
		require.ErrorIs(t, err, ErrInvalidName, name)
	}
}

func TestProtectedPipeSDDLIsExactAndPrivate(t *testing.T) {
	sid := "S-1-5-21-111111111-222222222-333333333-1001"

	require.Equal(t, "D:P(A;;GA;;;"+sid+")(A;;GA;;;SY)", protectedPipeSDDL(sid))
}

func TestListenAndDialValidateInputsBeforePlatform(t *testing.T) {
	_, err := Listen("", "secret")
	require.ErrorIs(t, err, ErrInvalidName)

	_, err = Dial(context.Background(), "control", "")
	require.ErrorIs(t, err, ErrInvalidSecret)
}

func TestAuthenticatedHandshakeRoundTripOverNetPipe(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- authenticateServer(serverConn, []byte("net-pipe-secret"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, authenticateClient(ctx, clientConn, []byte("net-pipe-secret")))
	require.NoError(t, <-serverResult)
}

func TestUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows transport is covered by native tests")
	}

	listener, err := Listen("control", "secret")
	require.Nil(t, listener)
	require.ErrorIs(t, err, ErrUnsupported)

	conn, err := Dial(context.Background(), "control", "secret")
	require.Nil(t, conn)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestProtocolErrorsDoNotContainSecret(t *testing.T) {
	secret := "do-not-echo-this-secret"
	for _, err := range []error{
		ErrAuthentication,
		ErrMalformedFrame,
		ErrFrameTooLarge,
		ErrUnsupported,
		ErrInvalidName,
		ErrInvalidSecret,
	} {
		require.NotContains(t, err.Error(), secret)
	}
	require.False(t, errors.Is(ErrAuthentication, ErrMalformedFrame))
}

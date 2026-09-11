//go:build windows

package controlpipe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

var pipeTestSequence uint64

var controlPipeAdvapi = windows.NewLazySystemDLL("advapi32.dll")

var (
	controlPipeImpersonateLoggedOnUser = controlPipeAdvapi.NewProc("ImpersonateLoggedOnUser")
	controlPipeLogonUser               = controlPipeAdvapi.NewProc("LogonUserW")
	controlPipeWaitNamedPipe           = windows.NewLazySystemDLL("kernel32.dll").NewProc("WaitNamedPipeW")
)

func TestAuthenticatedPipeRoundTrip(t *testing.T) {
	const secret = "round-trip-secret"
	listener, name := requirePipeListenerNamed(t, secret)
	acceptResult := acceptPipe(listener)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := Dial(ctx, name, secret)
	require.NoError(t, err)
	defer client.Close()

	var accepted acceptPipeResult
	select {
	case accepted = <-acceptResult:
	case <-time.After(6 * time.Second):
		t.Fatal("server did not finish the handshake before the deadline")
	}
	require.NoError(t, accepted.err)
	require.NotNil(t, accepted.conn)
	defer accepted.conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	require.NoError(t, client.SetDeadline(deadline))
	require.NoError(t, accepted.conn.SetDeadline(deadline))

	message := []byte("authenticated control data")
	serverRead := make(chan readPipeResult, 1)
	go func() {
		got := make([]byte, len(message))
		_, err := io.ReadFull(accepted.conn, got)
		serverRead <- readPipeResult{data: got, err: err}
	}()
	_, err = client.Write(message)
	require.NoError(t, err)
	var readResult readPipeResult
	select {
	case readResult = <-serverRead:
	case <-time.After(6 * time.Second):
		t.Fatal("server read did not finish before the deadline")
	}
	require.NoError(t, readResult.err)
	require.Equal(t, message, readResult.data)

	clientRead := make(chan readPipeResult, 1)
	go func() {
		got := make([]byte, len(message))
		_, err := io.ReadFull(client, got)
		clientRead <- readPipeResult{data: got, err: err}
	}()
	_, err = accepted.conn.Write(message)
	require.NoError(t, err)
	select {
	case readResult = <-clientRead:
	case <-time.After(6 * time.Second):
		t.Fatal("client read did not finish before the deadline")
	}
	require.NoError(t, readResult.err)
	require.Equal(t, message, readResult.data)
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

	select {
	case accepted := <-acceptResult:
		require.Nil(t, accepted.conn)
		require.ErrorIs(t, accepted.err, ErrAuthentication)
		require.NotContains(t, accepted.err.Error(), secret)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not reject the wrong secret before the deadline")
	}
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
	select {
	case serverErr := <-serverResult:
		require.NoError(t, serverErr)
	case <-time.After(2 * time.Second):
		t.Fatal("fake server did not finish before the deadline")
	}
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
			dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			raw, err := winio.DialPipeContext(dialCtx, path)
			cancel()
			require.NoError(t, err)
			require.NoError(t, raw.SetWriteDeadline(time.Now().Add(2*time.Second)))

			require.NoError(t, tt.writeFrame(raw))
			select {
			case accepted := <-acceptResult:
				_ = raw.Close()
				require.Nil(t, accepted.conn)
				require.ErrorIs(t, accepted.err, tt.want)
			case <-time.After(2 * time.Second):
				_ = raw.Close()
				t.Fatal("server did not reject the frame before the deadline")
			}
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

func TestListenerCloseInterruptsHandshake(t *testing.T) {
	listener, name := requirePipeListenerNamed(t, "close-handshake-secret")
	acceptResult := acceptPipe(listener)
	path, err := pipePath(name)
	require.NoError(t, err)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	raw, err := winio.DialPipeContext(dialCtx, path)
	cancel()
	require.NoError(t, err)
	defer raw.Close()
	require.NoError(t, raw.SetWriteDeadline(time.Now().Add(2*time.Second)))

	header := make([]byte, frameHeaderSize)
	binary.BigEndian.PutUint32(header, 1+controlNonceSize)
	require.NoError(t, writeAll(raw, append(header, frameHello)))
	authenticated := listener.(*authenticatedListener)
	waitForActiveHandshake(t, authenticated)

	require.NoError(t, listener.Close())
	select {
	case accepted := <-acceptResult:
		require.Nil(t, accepted.conn)
		require.ErrorIs(t, accepted.err, net.ErrClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after listener Close during handshake")
	}
}

func TestServerHandshakeDeadlineIsBounded(t *testing.T) {
	listener, name := requirePipeListenerNamed(t, "deadline-secret")
	acceptResult := acceptPipe(listener)
	path, err := pipePath(name)
	require.NoError(t, err)
	dialCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	raw, err := winio.DialPipeContext(dialCtx, path)
	cancel()
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
	// The effective token is thread-local. Keep identity lookup and Listen on
	// the same OS thread so this test also covers an impersonated caller.
	runtime.LockOSThread()
	sid, sidErr := effectiveUserSIDString()
	name := pipeTestName()
	listener, listenErr := Listen(name, "sddl-secret")
	runtime.UnlockOSThread()
	require.NoError(t, sidErr)
	require.NotEmpty(t, sid)
	require.NoError(t, listenErr)
	defer listener.Close()

	path, err := pipePath(name)
	require.NoError(t, err)
	assertPipeSecurityDescriptor(t, listener, path, sid)
}

func TestPipeRejectsDifferentWindowsUser(t *testing.T) {
	credentialPath := os.Getenv("UVP_CONTROLPIPE_OTHER_USER_CREDENTIAL_FILE")
	if credentialPath == "" {
		t.Skip("set UVP_CONTROLPIPE_OTHER_USER_CREDENTIAL_FILE to opt in with a pre-provisioned ordinary account; this test creates no accounts")
	}
	credentials := readControlPipeTestCredentials(t, credentialPath)

	runtime.LockOSThread()
	ownerSID, ownerErr := effectiveUserSIDString()
	name := pipeTestName()
	listener, listenErr := Listen(name, "different-user-secret")
	runtime.UnlockOSThread()
	require.NoError(t, ownerErr)
	require.NoError(t, listenErr)
	defer listener.Close()

	token, err := logonControlPipeTestUser(credentials)
	require.NoError(t, err)
	defer token.Close()
	user, err := token.GetTokenUser()
	require.NoError(t, err)
	require.NotNil(t, user)
	require.NotNil(t, user.User.Sid)
	require.NotEqual(t, ownerSID, user.User.Sid.String())
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	require.NoError(t, err)
	isAdministrator, err := token.IsMember(administrators)
	require.NoError(t, err)
	require.False(t, isAdministrator, "the opt-in account must be an ordinary user")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = withImpersonatedControlPipeTestToken(token, func() error {
		conn, dialErr := Dial(ctx, name, "different-user-secret")
		if conn != nil {
			_ = conn.Close()
		}
		return dialErr
	})
	require.Error(t, err)
	require.ErrorIs(t, err, windows.ERROR_ACCESS_DENIED)
}

type readPipeResult struct {
	data []byte
	err  error
}

type controlPipeTestCredentials struct {
	Domain   string `json:"domain"`
	User     string `json:"user"`
	Password string `json:"password"`
}

func readControlPipeTestCredentials(t *testing.T, path string) controlPipeTestCredentials {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	var credentials controlPipeTestCredentials
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&credentials))
	require.NotEmpty(t, credentials.User)
	require.NotEmpty(t, credentials.Password)
	if credentials.Domain == "" {
		credentials.Domain = "."
	}
	return credentials
}

func logonControlPipeTestUser(credentials controlPipeTestCredentials) (windows.Token, error) {
	user, err := windows.UTF16PtrFromString(credentials.User)
	if err != nil {
		return 0, errors.New("invalid test account name")
	}
	domain, err := windows.UTF16PtrFromString(credentials.Domain)
	if err != nil {
		return 0, errors.New("invalid test account domain")
	}
	password, err := windows.UTF16PtrFromString(credentials.Password)
	if err != nil {
		return 0, errors.New("invalid test account password")
	}
	var token windows.Token
	ok, _, callErr := controlPipeLogonUser.Call(
		uintptr(unsafe.Pointer(user)),
		uintptr(unsafe.Pointer(domain)),
		uintptr(unsafe.Pointer(password)),
		2,
		0,
		uintptr(unsafe.Pointer(&token)),
	)
	if ok == 0 {
		if callErr == nil {
			callErr = windows.ERROR_GEN_FAILURE
		}
		return 0, callErr
	}
	defer token.Close()
	var impersonationToken windows.Token
	if err := windows.DuplicateTokenEx(
		token,
		windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE,
		nil,
		windows.SecurityImpersonation,
		windows.TokenImpersonation,
		&impersonationToken,
	); err != nil {
		return 0, err
	}
	return impersonationToken, nil
}

func withImpersonatedControlPipeTestToken(token windows.Token, fn func() error) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok, _, callErr := controlPipeImpersonateLoggedOnUser.Call(uintptr(token))
	if ok == 0 {
		if callErr == nil {
			callErr = windows.ERROR_GEN_FAILURE
		}
		return errors.New("impersonate test account: " + callErr.Error())
	}
	defer func() {
		if revertErr := windows.RevertToSelf(); revertErr != nil && err == nil {
			err = errors.New("revert test account: " + revertErr.Error())
		}
	}()
	return fn()
}

func waitForActiveHandshake(t *testing.T, listener *authenticatedListener) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		listener.mu.Lock()
		active := len(listener.active)
		listener.mu.Unlock()
		if active > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not track the connected pipe before the deadline")
}

func assertPipeSecurityDescriptor(t *testing.T, listener net.Listener, path, userSID string) {
	t.Helper()
	authenticated, ok := listener.(*authenticatedListener)
	require.True(t, ok)
	// Query the metadata through a real client handle while a raw server
	// Accept is pending. This bypasses only the test authentication wrapper;
	// the listener and its kernel-created pipe instance are production ones.
	acceptResult := acceptPipe(authenticated.raw)
	metadataHandle, err := openPipeSecurityHandle(path)
	require.NoError(t, err)
	defer windows.Close(metadataHandle)
	var serverConn net.Conn
	select {
	case accepted := <-acceptResult:
		require.NoError(t, accepted.err)
		serverConn = accepted.conn
		require.NotNil(t, serverConn)
	case <-time.After(2 * time.Second):
		t.Fatal("raw server Accept did not complete the metadata connection")
	}
	defer serverConn.Close()

	descriptor, err := windows.GetSecurityInfo(
		metadataHandle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
	)
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	control, _, err := descriptor.Control()
	require.NoError(t, err)
	require.NotZero(t, control&windows.SE_DACL_PRESENT)
	require.NotZero(t, control&windows.SE_DACL_PROTECTED)

	dacl, _, err := descriptor.DACL()
	require.NoError(t, err)
	require.NotNil(t, dacl)
	require.Equal(t, uint16(2), dacl.AceCount)
	expected := map[string]int{"S-1-5-18": 1}
	expected[userSID]++
	actual := make(map[string]int, len(expected))
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, index, &ace))
		require.NotNil(t, ace)
		require.Equal(t, uint8(windows.ACCESS_ALLOWED_ACE_TYPE), ace.Header.AceType)
		require.Zero(t, ace.Header.AceFlags)
		require.NotZero(t, ace.Mask)
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		require.True(t, sid.IsValid())
		key := sid.String()
		_, ok := expected[key]
		require.True(t, ok, "unexpected pipe DACL principal %s", key)
		actual[key]++
	}
	require.Equal(t, expected, actual)
}

func openPipeSecurityHandle(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	deadline := time.Now().Add(time.Second)
	var lastErr error
	for {
		waitErr := waitNamedPipe(name, 1000)
		if waitErr != nil {
			if !isPipeAvailabilityError(waitErr) {
				return windows.InvalidHandle, waitErr
			}
			lastErr = waitErr
		} else {
			handle, openErr := windows.CreateFile(
				name,
				windows.READ_CONTROL,
				0,
				nil,
				windows.OPEN_EXISTING,
				0,
				0,
			)
			if openErr == nil {
				return handle, nil
			}
			if !isPipeAvailabilityError(openErr) {
				return windows.InvalidHandle, openErr
			}
			lastErr = openErr
		}
		if !time.Now().Before(deadline) {
			if lastErr == nil {
				lastErr = windows.ERROR_PIPE_BUSY
			}
			return windows.InvalidHandle, lastErr
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitNamedPipe(name *uint16, timeoutMS uint32) error {
	result, _, callErr := controlPipeWaitNamedPipe.Call(uintptr(unsafe.Pointer(name)), uintptr(timeoutMS))
	if result != 0 {
		return nil
	}
	if callErr == nil {
		return windows.ERROR_GEN_FAILURE
	}
	return callErr
}

func isPipeAvailabilityError(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PIPE_BUSY)
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

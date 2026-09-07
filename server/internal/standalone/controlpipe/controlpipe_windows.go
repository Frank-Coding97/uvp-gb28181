//go:build windows

package controlpipe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listenPlatform(path string, secret []byte) (net.Listener, error) {
	userSID, err := effectiveUserSIDString()
	if err != nil {
		return nil, err
	}
	raw, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: protectedPipeSDDL(userSID),
	})
	if err != nil {
		return nil, fmt.Errorf("listen control pipe: %w", err)
	}
	return &authenticatedListener{
		raw:    raw,
		secret: secret,
		active: make(map[net.Conn]struct{}),
	}, nil
}

func dialPlatform(ctx context.Context, path string, secret []byte) (net.Conn, error) {
	raw, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := authenticateClient(ctx, raw, secret); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return raw, nil
}

type authenticatedListener struct {
	raw    net.Listener
	secret []byte

	mu     sync.Mutex
	closed bool
	active map[net.Conn]struct{}
}

func (l *authenticatedListener) Accept() (net.Conn, error) {
	conn, err := l.raw.Accept()
	if err != nil {
		return nil, err
	}
	if !l.track(conn) {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	defer l.untrack(conn)

	err = authenticateServer(conn, l.secret)
	if err != nil {
		_ = conn.Close()
		l.mu.Lock()
		closed := l.closed
		l.mu.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		return nil, err
	}
	return conn, nil
}

func (l *authenticatedListener) track(conn net.Conn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	l.active[conn] = struct{}{}
	return true
}

func (l *authenticatedListener) untrack(conn net.Conn) {
	l.mu.Lock()
	delete(l.active, conn)
	l.mu.Unlock()
}

func (l *authenticatedListener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	active := make([]net.Conn, 0, len(l.active))
	for conn := range l.active {
		active = append(active, conn)
	}
	l.mu.Unlock()
	for _, conn := range active {
		_ = conn.Close()
	}
	return l.raw.Close()
}

func (l *authenticatedListener) Addr() net.Addr {
	return l.raw.Addr()
}

func effectiveUserSIDString() (string, error) {
	token := windows.GetCurrentThreadEffectiveToken()
	user, err := token.GetTokenUser()
	if err == nil {
		if user.User.Sid == nil || !user.User.Sid.IsValid() {
			return "", errors.New("invalid effective Windows user SID")
		}
		return user.User.Sid.String(), nil
	}
	if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return "", errors.New("query effective Windows user token")
	}
	var processToken windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &processToken); err != nil {
		return "", errors.New("query process Windows user token")
	}
	defer processToken.Close()
	user, err = processToken.GetTokenUser()
	if err != nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return "", errors.New("query process Windows user token")
	}
	return user.User.Sid.String(), nil
}

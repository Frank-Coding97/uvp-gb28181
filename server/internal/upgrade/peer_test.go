//go:build !windows

package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixPeerAuthorizerFailsClosedWithoutAllowedUID(t *testing.T) {
	authorize := NewUnixPeerAuthorizer(nil)
	if err := authorize(nil); err == nil || !strings.Contains(err.Error(), "no authorized") {
		t.Fatalf("empty allowlist error = %v", err)
	}
}

func TestUnixPeerAuthorizerChecksAcceptedSocketPeer(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	server := &UnixServer{
		SocketPath:     daemonSocketPath(),
		Store:          store,
		PeerAuthorizer: NewUnixPeerAuthorizer([]uint32{uint32(os.Getuid())}),
	}
	cancel, done := startUnixServer(t, server)
	defer func() { cancel(); <-done }()
	response, err := CallUnix(context.Background(), server.SocketPath, ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus})
	if err != nil || !response.OK {
		t.Fatalf("authorized peer response = %#v, err = %v", response, err)
	}
}

func TestUnixPeerAuthorizerRejectsUnexpectedSocketPeer(t *testing.T) {
	root := t.TempDir()
	store, err := OpenTaskStore(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	server := &UnixServer{
		SocketPath:     daemonSocketPath(),
		Store:          store,
		PeerAuthorizer: NewUnixPeerAuthorizer([]uint32{uint32(os.Getuid()) + 1}),
	}
	cancel, done := startUnixServer(t, server)
	defer func() { cancel(); <-done }()
	response, err := CallUnix(context.Background(), server.SocketPath, ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus})
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || !strings.Contains(response.Error, "not authorized") {
		t.Fatalf("unexpected peer response = %#v", response)
	}
}

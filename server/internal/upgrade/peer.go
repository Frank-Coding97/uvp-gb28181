package upgrade

import (
	"net"
)

// NewUnixPeerAuthorizer permits only administrator-registered local peer
// identities. An empty set is a deliberate fail-closed configuration: a helper
// without a fixed local caller identity must not accept privileged protocol
// requests.
//
// The platform hook authorizesLocalPeer decides how a local identity is read;
// on Unix that is the socket peer UID, on Windows there is no comparable
// credential on an AF_UNIX socket.
func NewUnixPeerAuthorizer(allowedUIDs []uint32) PeerAuthorizer {
	allowed := make(map[uint32]struct{}, len(allowedUIDs))
	for _, uid := range allowedUIDs {
		allowed[uid] = struct{}{}
	}
	return func(conn net.Conn) error {
		return authorizeLocalPeer(conn, allowed)
	}
}

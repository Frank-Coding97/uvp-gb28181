package upgrade

import (
	"errors"
	"fmt"
	"net"
)

// NewUnixPeerAuthorizer permits only administrator-registered Unix peer UIDs.
// An empty set is a deliberate fail-closed configuration: a helper without a
// fixed local caller identity must not accept privileged protocol requests.
func NewUnixPeerAuthorizer(allowedUIDs []uint32) PeerAuthorizer {
	allowed := make(map[uint32]struct{}, len(allowedUIDs))
	for _, uid := range allowedUIDs {
		allowed[uid] = struct{}{}
	}
	return func(conn net.Conn) error {
		if len(allowed) == 0 {
			return errors.New("updater has no authorized Unix peer UID")
		}
		uid, err := unixPeerUID(conn)
		if err != nil {
			return fmt.Errorf("read updater Unix peer credentials: %w", err)
		}
		if _, ok := allowed[uid]; !ok {
			return fmt.Errorf("updater Unix peer UID %d is not authorized", uid)
		}
		return nil
	}
}

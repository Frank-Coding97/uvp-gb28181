//go:build linux || darwin

package upgrade

import (
	"errors"
	"fmt"
	"net"
)

func authorizeLocalPeer(conn net.Conn, allowed map[uint32]struct{}) error {
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

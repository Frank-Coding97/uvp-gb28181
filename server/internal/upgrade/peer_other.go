//go:build !linux && !darwin && !windows

package upgrade

import (
	"errors"
	"net"
)

func unixPeerUID(net.Conn) (uint32, error) {
	return 0, errors.New("Unix peer credentials are unsupported on this platform")
}

func authorizeLocalPeer(net.Conn, map[uint32]struct{}) error {
	return errors.New("updater peer authorization is unsupported on this platform")
}

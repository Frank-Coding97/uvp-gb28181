//go:build !linux && !darwin

package upgrade

import (
	"errors"
	"net"
)

func unixPeerUID(net.Conn) (uint32, error) {
	return 0, errors.New("Unix peer credentials are unsupported on this platform")
}

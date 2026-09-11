//go:build !windows

package controlpipe

import (
	"context"
	"net"
)

func listenPlatform(_ string, _ []byte) (net.Listener, error) {
	return nil, ErrUnsupported
}

func dialPlatform(_ context.Context, _ string, _ []byte) (net.Conn, error) {
	return nil, ErrUnsupported
}

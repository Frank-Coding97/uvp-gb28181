//go:build windows

package upgrade

import "net"

// authorizeLocalPeer is a deliberate accept-all on Windows: Go's AF_UNIX
// implementation exposes no SO_PEERCRED equivalent, so the helper cannot learn
// which local process connected.
//
// The security boundary therefore moves to the filesystem. The socket lives
// under the installation root, which the installer creates inside the
// administrator's own profile: a standard user cannot reach it at all, and
// every process that can is already able to replace the binaries the helper
// would swap. That is the same trust level the Windows control script already
// runs at, so refusing the connection would add no protection.
//
// Windows deployments always run the helper and the business process under one
// account. If that ever changes, this function must be replaced with a peer
// check (SIO_AF_UNIX_GETPEERPID + token comparison), not extended.
func authorizeLocalPeer(net.Conn, map[uint32]struct{}) error {
	return nil
}

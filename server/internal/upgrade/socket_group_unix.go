//go:build linux || darwin

package upgrade

import "os"

func setSocketGroup(path string, gid int) error {
	// Keep the owner unchanged. The helper normally runs as root, while the
	// business process only needs group access to the socket.
	return os.Chown(path, -1, gid)
}

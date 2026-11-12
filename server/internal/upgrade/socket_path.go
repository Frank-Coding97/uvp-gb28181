package upgrade

import (
	"errors"
	"fmt"
	"strings"
)

// MaxSocketPathBytes is the largest AF_UNIX socket path that can be bound or
// dialed on every supported platform. sockaddr_un.sun_path is 108 bytes and
// the trailing NUL is mandatory, so 107 bytes remain for the path itself.
//
// Windows is where this bites: the standalone package keeps the socket under
// the installation root, so a customer who extracts the release into a nested
// or deep folder (Downloads\uvp-...-1.1.21\uvp-...-1.1.21\run) exceeds it
// without any single path component looking unusual. Go then fails with a bare
// "bind: invalid argument" / "connect: invalid argument" and the service
// upgrade page only shows a generic 503. 2026-11-10 on 192.168.10.222 the
// real path was exactly 108 bytes; renaming the inner folder to "app" brought
// it down to 79 and the helper came up immediately.
const MaxSocketPathBytes = 107

// ValidateSocketPath rejects paths AF_UNIX cannot represent. len(string)
// counts UTF-8 bytes, which is exactly what sun_path stores, so a path with
// multi-byte characters is measured the way the OS measures it.
func ValidateSocketPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("updater socket path is required")
	}
	if len(path) > MaxSocketPathBytes {
		return fmt.Errorf(
			"updater socket path is %d bytes, exceeds the %d-byte AF_UNIX limit (sun_path is 108 bytes including the trailing NUL): move the installation to a shorter directory (e.g. D:\\uvp) or remove the duplicated release folder; path=%q",
			len(path), MaxSocketPathBytes, path,
		)
	}
	return nil
}

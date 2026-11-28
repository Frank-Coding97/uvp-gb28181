//go:build windows

package upgrade

import (
	"errors"
	"fmt"
	"os"
)

// openTrustedRegistration on Windows cannot reproduce the openat/O_NOFOLLOW
// walk: there are no POSIX owners and no directory file descriptors that keep
// path resolution pinned. What still matters is that the helper never follows a
// reparse point out of the installation root, and that it reads a real file.
//
// The registration lives inside the installation directory, which the installer
// creates under the administrator's own profile. Any account able to rewrite it
// can already replace the binaries the helper would swap, so an additional ACL
// walk would not raise the trust level; it would only need to be re-audited
// every time Windows changes its default inheritance.
func openTrustedRegistration(path string) (*os.File, error) {
	if _, err := registrationPathComponents(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat updater registration: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("updater registration must be a regular file, not a reparse point")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("updater registration must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open updater registration: %w", err)
	}
	// Re-check the opened handle so a swap between Lstat and Open is caught.
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat opened updater registration: %w", err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, errors.New("updater registration changed while it was opened")
	}
	return file, nil
}

//go:build windows

package upgrade

// setSocketGroup has no Windows equivalent: an AF_UNIX socket file inherits
// the ACL of the directory it is created in, and the helper state directory is
// created inside the installation root.
func setSocketGroup(_ string, _ int) error { return nil }

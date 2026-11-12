//go:build windows

package upgrade

// newStandaloneAdapter selects the standalone deployment strategy for the host.
// Windows cannot use the release-directory + current link model, so the
// registration's own layout drives an in-place swap instead.
func newStandaloneAdapter(registration Registration) DeploymentAdapter {
	return &WindowsStandaloneAdapter{Layout: registration.Standalone, Runner: registeredBusinessRunner(registration), ExternalBackup: registration.DatabaseBackup}
}

//go:build !windows

package upgrade

// newStandaloneAdapter selects the standalone deployment strategy for the host.
// Every non-Windows platform uses the release-directory + current link model.
func newStandaloneAdapter(registration Registration) DeploymentAdapter {
	return &StandaloneAdapter{Layout: registration.Standalone, Runner: registeredBusinessRunner(registration), ExternalBackup: registration.DatabaseBackup}
}

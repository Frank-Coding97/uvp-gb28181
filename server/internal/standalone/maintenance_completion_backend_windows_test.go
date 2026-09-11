//go:build windows

package standalone

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsMaintenanceBackendAcceptsPersistedReleaseIdentity(t *testing.T) {
	fixture := newMaintenanceBackendTestFixture(t, maintenanceBackendTestPath(t))
	_, digest, err := installedMaintenanceReleaseSnapshot(fixture.paths.InstallDir)
	require.NoError(t, err)
	fixture.journal.ReleaseSetSHA256 = digest
	raw, err := json.Marshal(fixture.journal)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(fixture.paths.InstallDir, maintenanceDirName, "journal.json"), raw, true, nil))
	frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, "db_check", fixture.journal.CandidateVersion)
	require.NoError(t, err)
	defer clear(frame)
	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-db-check"}, frame)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	require.Zero(t, result.exitCode, "backend must accept the current persisted maintenance protocol")
	assertMaintenanceCompleteOutput(t, result.stdout)
	assertMaintenancePermitMissing(t, fixture.paths.InstallDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
}

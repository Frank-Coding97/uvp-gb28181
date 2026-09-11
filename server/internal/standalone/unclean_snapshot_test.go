package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type uncleanSnapshotFixture struct {
	paths       Paths
	lock        *InstanceLock
	journal     MaintenanceJournal
	trust       string
	marker      []byte
	sqliteBytes map[string][]byte
}

func TestUncleanSnapshotCopiesBoundMarkerAndPreservesSQLiteFamily(t *testing.T) {
	fixture := newUncleanSnapshotFixture(t, "unclean-snapshot")
	destination := fixture.journal.BackupRoot

	manifest, err := backupStoppedAdmitted(context.Background(), fixture.paths, destination, fixture.journal.OperationID, fixture.trust, fixture.journal.ReleaseSetSHA256)
	require.NoError(t, err)
	require.Equal(t, 2, manifest.FormatVersion)
	require.Equal(t, "unclean_snapshot", manifest.Kind)
	require.Equal(t, fixture.journal.OperationID, manifest.OperationID)
	require.Equal(t, fixture.journal.RunMarkerSHA256, manifest.RunMarkerSHA256)
	require.Equal(t, fixture.journal.OldCurrentSHA256, manifest.SourceCurrentSHA256)

	for _, file := range manifest.Files {
		require.NotEqual(t, "data/.uvp-running.json", file.Path)
	}
	verified, err := VerifyBackup(context.Background(), destination)
	require.NoError(t, err)
	require.Equal(t, manifest, verified)

	markerAfter, err := os.ReadFile(filepath.Join(fixture.paths.DataDir, runMarkerName))
	require.NoError(t, err)
	require.Equal(t, fixture.marker, markerAfter)
	for suffix, before := range fixture.sqliteBytes {
		after, err := os.ReadFile(fixture.paths.DatabasePath + suffix)
		require.NoError(t, err)
		require.Equal(t, before, after, "source SQLite family changed for %q", suffix)
	}
	_, err = os.Stat(filepath.Join(destination, "data", runMarkerName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestOrdinaryBackupRejectsRunMarkerWithoutChangingIt(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	_, err = BeginRun(paths)
	require.NoError(t, err)
	markerPath := filepath.Join(paths.DataDir, runMarkerName)
	before, err := os.ReadFile(markerPath)
	require.NoError(t, err)

	destination := filepath.Join(filepath.Dir(paths.InstallDir), "ordinary-marker-backup")
	_, err = backupStoppedAdmitted(context.Background(), paths, destination, "", "", "")
	require.ErrorContains(t, err, "run marker")
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
	after, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestSchema1PreparingBackupRejectsRunMarkerWithoutChangingIt(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	_, err = BeginRun(paths)
	require.NoError(t, err)
	markerPath := filepath.Join(paths.DataDir, runMarkerName)
	before, err := os.ReadFile(markerPath)
	require.NoError(t, err)

	candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t24-candidate", false)
	current, err := LoadRelease(paths.InstallDir)
	require.NoError(t, err)
	currentPointer, err := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
	require.NoError(t, err)
	currentSum := sha256.Sum256(currentPointer)
	currentBackend, err := releaseFileSHA256(current.BackendExe)
	require.NoError(t, err)
	candidateBackend, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
	require.NoError(t, err)
	identity := mustInstalledMaintenanceReleaseIdentity(t, paths.InstallDir)
	journal := maintenanceTestJournal(paths.InstallDir)
	journal.OldVersion = current.Version
	journal.CandidateVersion = candidate.manifest.Version
	journal.OldCurrentSHA256 = hex.EncodeToString(currentSum[:])
	journal.BackupRoot = filepath.Join(filepath.Dir(paths.InstallDir), "schema1-marker-backup")
	journal.BackupManifestSHA256 = ""
	journal.Phase = MaintenancePreparing
	journal.ReleaseSetSHA256 = identity
	require.NoError(t, createPreparingMaintenanceJournal(paths.InstallDir, journal))

	_, err = backupStoppedAdmitted(context.Background(), paths, journal.BackupRoot, journal.OperationID, currentBackend+","+candidateBackend, identity)
	require.ErrorContains(t, err, "run marker")
	_, err = os.Stat(journal.BackupRoot)
	require.ErrorIs(t, err, os.ErrNotExist)
	after, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestUncleanSnapshotVerifyRejectsBindingTamperAndMissingField(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BackupManifest)
	}{
		{
			name: "source current binding",
			mutate: func(manifest *BackupManifest) {
				wrong := strings.Repeat("0", 64)
				if wrong == manifest.SourceCurrentSHA256 {
					wrong = strings.Repeat("1", 64)
				}
				manifest.SourceCurrentSHA256 = wrong
			},
		},
		{
			name: "run marker field missing",
			mutate: func(manifest *BackupManifest) {
				manifest.RunMarkerSHA256 = ""
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newUncleanSnapshotFixture(t, "unclean-verify")
			_, err := backupStoppedAdmitted(context.Background(), fixture.paths, fixture.journal.BackupRoot, fixture.journal.OperationID, fixture.trust, fixture.journal.ReleaseSetSHA256)
			require.NoError(t, err)
			rewriteUncleanSnapshotManifest(t, fixture.journal.BackupRoot, tc.mutate)
			_, err = VerifyBackup(context.Background(), fixture.journal.BackupRoot)
			require.Error(t, err)
		})
	}
}

func TestUncleanSnapshotCopyFailureRetainsDedicatedStaging(t *testing.T) {
	fixture := newUncleanSnapshotFixture(t, "unclean-copy-failure")
	manifestPath := filepath.Join(fixture.paths.DataDir, "redis", "appendonlydir", "manifest")
	require.NoError(t, os.WriteFile(manifestPath, []byte("file missing.aof seq 2 type i\n"), 0o600))
	dataIdentity, err := recoveryTreeIdentity(context.Background(), fixture.paths.DataDir)
	require.NoError(t, err)
	fixture.journal.SourceDataSHA256 = dataIdentity
	journalRaw, err := json.Marshal(fixture.journal)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(fixture.paths.InstallDir, maintenanceDirName, "journal.json"), journalRaw, true, nil))

	_, err = backupStoppedAdmitted(context.Background(), fixture.paths, fixture.journal.BackupRoot, fixture.journal.OperationID, fixture.trust, fixture.journal.ReleaseSetSHA256)
	require.ErrorContains(t, err, "AOF manifest")
	_, err = os.Stat(fixture.journal.BackupRoot)
	require.ErrorIs(t, err, os.ErrNotExist)
	staging, err := filepath.Glob(filepath.Join(filepath.Dir(fixture.journal.BackupRoot), ".uvp-unclean-snapshot-*"))
	require.NoError(t, err)
	require.Len(t, staging, 1)
	_, err = os.Stat(filepath.Join(staging[0], "data", "uvp.db"))
	require.NoError(t, err)
	markerAfter, err := os.ReadFile(filepath.Join(fixture.paths.DataDir, runMarkerName))
	require.NoError(t, err)
	require.Equal(t, fixture.marker, markerAfter)
}

func newUncleanSnapshotFixture(t *testing.T, destinationName string) uncleanSnapshotFixture {
	t.Helper()
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	_, err = BeginRun(paths)
	require.NoError(t, err)
	markerPath := filepath.Join(paths.DataDir, runMarkerName)
	marker, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	markerSum := sha256.Sum256(marker)
	current, err := LoadRelease(paths.InstallDir)
	require.NoError(t, err)
	currentPointer, err := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
	require.NoError(t, err)
	currentSum := sha256.Sum256(currentPointer)
	configIdentity, err := recoveryTreeIdentity(context.Background(), paths.ConfigDir)
	require.NoError(t, err)
	dataIdentity, err := recoveryTreeIdentity(context.Background(), paths.DataDir)
	require.NoError(t, err)
	_, releaseIdentity, err := installedMaintenanceReleaseSnapshot(paths.InstallDir)
	require.NoError(t, err)
	backendHash, err := releaseFileSHA256(current.BackendExe)
	require.NoError(t, err)
	destination := filepath.Join(filepath.Dir(paths.InstallDir), destinationName)
	journal := MaintenanceJournal{
		Schema:             2,
		OperationID:        strings.Repeat("a", 64),
		OldVersion:         current.Version,
		Kind:               maintenanceKindUnclean,
		RunMarkerSHA256:    hex.EncodeToString(markerSum[:]),
		SourceConfigSHA256: configIdentity,
		SourceDataSHA256:   dataIdentity,
		OldCurrentSHA256:   hex.EncodeToString(currentSum[:]),
		BackupRoot:         destination,
		Phase:              MaintenancePreparing,
		CreatedAt:          time.Now().UTC(),
		ReleaseSetSHA256:   releaseIdentity,
	}
	require.NoError(t, createUncleanPreparingJournal(paths.InstallDir, journal))
	family := make(map[string][]byte)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		raw, readErr := os.ReadFile(paths.DatabasePath + suffix)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		require.NoError(t, readErr)
		family[suffix] = raw
	}
	return uncleanSnapshotFixture{paths: paths, lock: lock, journal: journal, trust: backendHash, marker: marker, sqliteBytes: family}
}

func rewriteUncleanSnapshotManifest(t *testing.T, root string, mutate func(*BackupManifest)) {
	t.Helper()
	manifestPath := filepath.Join(root, backupManifestFile)
	raw, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var manifest BackupManifest
	require.NoError(t, decodeReleaseJSON(raw, &manifest))
	mutate(&manifest)
	updated, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(manifestPath, updated, true, nil))
	digest := sha256.Sum256(updated)
	marker, err := json.Marshal(backupCompleteMarker{ManifestPath: backupManifestFile, ManifestSHA256: hex.EncodeToString(digest[:])})
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(root, "complete.json"), marker, true, nil))
}

func mustInstalledMaintenanceReleaseIdentity(t *testing.T, installDir string) string {
	t.Helper()
	_, identity, err := installedMaintenanceReleaseSnapshot(installDir)
	require.NoError(t, err)
	return identity
}

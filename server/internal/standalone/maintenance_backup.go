package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

// Called while both installation locks are held. The gate is already visible,
// so this scan also catches processes admitted just before its publication.
func checkPreparingBackupAdmission(paths Paths, destination, operationID, trust string) (string, error) {
	journal, err := ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return "", err
	}
	root, err := cleanAbsolute(destination)
	if err != nil {
		return "", err
	}
	if journal.Phase != MaintenancePreparing || journal.OperationID != operationID || journal.BackupRoot != root {
		return "", errors.New("preparing backup operation or destination mismatch")
	}
	current, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, journal.CandidateVersion, trust)
	if err != nil {
		return "", err
	}
	currentHash, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
	if err != nil || currentHash != journal.OldCurrentSHA256 || current.Version != journal.OldVersion {
		return "", errors.New("preparing backup current release changed")
	}
	releases, fingerprint, err := installedMaintenanceReleaseSnapshot(paths.InstallDir)
	if err != nil {
		return "", err
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return "", err
	}
	return fingerprint, nil
}

func installedMaintenanceReleaseSnapshot(installDir string) ([]Release, string, error) {
	releases, err := loadInstalledMaintenanceReleases(installDir)
	if err != nil {
		return nil, "", err
	}
	var identity strings.Builder
	for _, release := range releases {
		digest, err := releaseFileSHA256(filepath.Join(release.ReleaseDir, "manifest.json"))
		if err != nil {
			return nil, "", err
		}
		identity.WriteString(release.Version + "\x00" + digest + "\n")
	}
	sum := sha256.Sum256([]byte(identity.String()))
	return releases, hex.EncodeToString(sum[:]), nil
}

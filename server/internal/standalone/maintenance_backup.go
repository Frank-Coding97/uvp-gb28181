package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Called while both installation locks are held. The gate is already visible,
// so this scan also catches processes admitted just before its publication.
func checkPreparingBackupAdmission(paths Paths, destination, operationID, trust string) (string, error) {
	return checkPreparingBackupAdmissionContext(context.Background(), paths, destination, operationID, trust)
}

// checkPreparingBackupAdmissionContext is the context-aware implementation
// used by the copy/recheck path. The public shape above is kept for the
// package's existing tests and unexported callers.
func checkPreparingBackupAdmissionContext(ctx context.Context, paths Paths, destination, operationID, trust string) (string, error) {
	if ctx == nil {
		return "", errors.New("preparing backup context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
	if journal.Schema != 1 && !(journal.Schema == 2 && journal.Kind == maintenanceKindUnclean) {
		return "", errors.New("preparing backup operation kind is unsupported")
	}
	current, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, maintenanceSourceSelection(journal), trust)
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
	if journal.Schema == 2 {
		if journal.Kind != maintenanceKindUnclean || journal.CandidateVersion != "" ||
			!validMaintenancePermitHex(journal.RunMarkerSHA256) ||
			!validMaintenancePermitHex(journal.SourceConfigSHA256) ||
			!validMaintenancePermitHex(journal.SourceDataSHA256) ||
			!validMaintenancePermitHex(journal.OldCurrentSHA256) ||
			!validMaintenancePermitHex(journal.ReleaseSetSHA256) {
			return "", errors.New("invalid unclean preparing backup identity")
		}
		if fingerprint != journal.ReleaseSetSHA256 {
			return "", errors.New("preparing backup release set changed")
		}
		markerSHA, err := preparingRunMarkerSHA(paths)
		if err != nil || markerSHA != journal.RunMarkerSHA256 {
			if err != nil {
				return "", err
			}
			return "", errors.New("preparing backup run marker changed")
		}
		configSHA, err := recoveryTreeIdentity(ctx, paths.ConfigDir)
		if err != nil {
			return "", err
		}
		dataSHA, err := recoveryTreeIdentity(ctx, paths.DataDir)
		if err != nil {
			return "", err
		}
		if configSHA != journal.SourceConfigSHA256 || dataSHA != journal.SourceDataSHA256 {
			return "", errors.New("preparing backup source tree changed")
		}
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return "", err
	}
	return fingerprint, nil
}

// preparingRunMarkerSHA reads the marker while ConfigLock is already held by
// the caller. Calling InspectRunMarker here would try to acquire ConfigLock a
// second time and deadlock; this helper deliberately performs only the
// read/decode/hash part and never removes or rewrites the marker.
func preparingRunMarkerSHA(paths Paths) (string, error) {
	path := filepath.Join(paths.DataDir, runMarkerName)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("preparing backup run marker is missing")
		}
		return "", err
	}
	if info.Size() > 4096 {
		return "", errors.New("preparing backup run marker exceeds limit")
	}
	raw, err := readSecureConfigFile(path)
	if err != nil {
		return "", err
	}
	if _, err := decodeRunMarker(raw); err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
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

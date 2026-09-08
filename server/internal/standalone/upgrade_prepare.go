package standalone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"
)

// The returned owner must survive migration, health checks and commit/recovery.
// Close only releases the lock; it never clears an unfinished maintenance gate.
type upgradePreparation struct {
	journal MaintenanceJournal
	lock    *InstanceLock
}

func (p *upgradePreparation) Close() error {
	if p == nil || p.lock == nil {
		return nil
	}
	return p.lock.Close()
}

func prepareUpgradeStopped(ctx context.Context, paths Paths, candidateVersion, destination string) (*upgradePreparation, error) {
	return prepareUpgradeStoppedWithTrust(ctx, paths, candidateVersion, destination, maintenanceBackendSHA256Allowlist)
}

func prepareUpgradeStoppedWithTrust(ctx context.Context, paths Paths, candidateVersion, destination, trust string) (*upgradePreparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, candidateVersion, trust)
	if err != nil {
		return nil, err
	}
	_, releaseIdentity, err := installedMaintenanceReleaseSnapshot(paths.InstallDir)
	if err != nil {
		return nil, err
	}
	currentSHA, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
	if err != nil {
		return nil, err
	}
	destination, err = backupDestination(paths, destination)
	if err != nil {
		return nil, err
	}
	if err := CheckMaintenanceGate(paths.InstallDir); err != nil {
		return nil, err
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if err != nil {
		return nil, err
	}
	retained := false
	defer func() {
		if !retained {
			_ = lock.Close()
		}
	}()
	if err := CheckMaintenanceGate(paths.InstallDir); err != nil {
		return nil, err
	}
	lockedCurrent, _, err := loadMaintenanceReleasesWithTrust(paths.InstallDir, candidateVersion, trust)
	if err != nil {
		return nil, err
	}
	_, lockedIdentity, err := installedMaintenanceReleaseSnapshot(paths.InstallDir)
	if err != nil {
		return nil, err
	}
	lockedSHA, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
	if err != nil || lockedSHA != currentSHA || lockedIdentity != releaseIdentity || lockedCurrent.Version != current.Version {
		return nil, errors.New("installation changed while acquiring upgrade ownership")
	}
	if _, err := backupDestination(paths, destination); err != nil {
		return nil, err
	}
	if err := paths.Validate(); err != nil {
		return nil, err
	}
	if _, err := LoadConfig(paths); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	journal := MaintenanceJournal{
		Schema: 1, OperationID: hex.EncodeToString(nonce[:]), OldVersion: current.Version,
		CandidateVersion: candidateVersion, OldCurrentSHA256: currentSHA, BackupRoot: destination,
		Phase: MaintenancePreparing, CreatedAt: time.Now().UTC(),
	}
	if err := createPreparingMaintenanceJournal(paths.InstallDir, journal); err != nil {
		return nil, err
	}
	if _, err := backupStoppedAdmitted(ctx, paths, destination, journal.OperationID, trust, lockedIdentity); err != nil {
		return nil, err
	}
	if err := promotePreparedMaintenance(ctx, paths.InstallDir, journal.OperationID, destination); err != nil {
		return nil, err
	}
	journal, err = ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return nil, err
	}
	retained = true
	return &upgradePreparation{journal: journal, lock: lock}, nil
}

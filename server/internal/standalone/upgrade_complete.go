package standalone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Only the same single owner that executed and committed the upgrade can mint
// the durable completion proof. Even afterward the active gate remains closed.
func (p *upgradePreparation) markCompletionReady(ctx context.Context) error {
	return p.withCompletionReady(ctx, nil)
}

func (p *upgradePreparation) withCompletionReady(ctx context.Context, complete func(MaintenanceJournal) error) error {
	if p == nil || p.lock == nil || !p.committed {
		return errors.New("upgrade completion requires the successful transaction owner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return withConfigLock(p.paths.InstallDir, func() error {
		if err := requireMaintenanceInstanceLock(p.paths.InstallDir); err != nil {
			return err
		}
		journal, err := ReadMaintenanceJournal(p.paths.InstallDir)
		if err != nil {
			return err
		}
		expected := p.journal
		expected.Phase = MaintenanceCommitting
		normalized := journal
		if journal.Phase == MaintenanceCompletionReady {
			normalized.Phase = MaintenanceCommitting
			normalized.CompletedAt, normalized.CandidateCurrentSHA256 = "", ""
		}
		if normalized != expected || journal.ReleaseSetSHA256 == "" || journal.ReleaseSetSHA256 != p.releaseIdentity {
			return errors.New("upgrade completion identity changed")
		}
		if _, _, err := loadMaintenanceReleasesWithTrust(p.paths.InstallDir, journal.CandidateVersion, p.trust); err != nil {
			return err
		}
		releases, identity, err := installedMaintenanceReleaseSnapshot(p.paths.InstallDir)
		if err != nil {
			return err
		}
		if identity != journal.ReleaseSetSHA256 {
			return errors.New("upgrade completion release set changed")
		}
		if err := backupComponentsStopped(releases...); err != nil {
			return err
		}
		if _, err := os.Lstat(maintenancePermitPath(p.paths.InstallDir)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("upgrade completion requires no outstanding permit")
		}
		candidateRaw, err := json.Marshal(releaseCurrentPointer{Version: journal.CandidateVersion})
		if err != nil {
			return err
		}
		actual, err := os.ReadFile(filepath.Join(p.paths.InstallDir, "current.json"))
		if err != nil || !bytes.Equal(actual, candidateRaw) {
			return errors.New("upgrade completion current pointer changed")
		}
		if _, err := VerifyBackup(ctx, journal.BackupRoot); err != nil {
			return err
		}
		digest, err := releaseFileSHA256(filepath.Join(journal.BackupRoot, backupManifestFile))
		if err != nil || digest != journal.BackupManifestSHA256 {
			return errors.New("upgrade completion backup changed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		candidateSHA := currentPointerSHA256(candidateRaw)
		if journal.Phase == MaintenanceCompletionReady {
			if journal.CandidateCurrentSHA256 != candidateSHA {
				return errors.New("upgrade completion pointer proof changed")
			}
			if complete != nil {
				return complete(journal)
			}
			return nil
		}
		journal.Phase = MaintenanceCompletionReady
		journal.CandidateCurrentSHA256 = candidateSHA
		journal.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := validateMaintenanceJournal(journal); err != nil {
			return err
		}
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		if err := writeSecureConfigFile(filepath.Join(p.paths.InstallDir, maintenanceDirName, "journal.json"), raw, true, nil); err != nil {
			return err
		}
		if complete != nil {
			return complete(journal)
		}
		return nil
	})
}

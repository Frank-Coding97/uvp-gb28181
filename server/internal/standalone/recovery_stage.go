package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type recoveryRedisRunner func(context.Context, string, string, string, string, int) error

// Prepare a disposable, isolated old-schema copy. Runners must wait for every
// owned process and verify their durable results before returning success.
// Caller owns InstanceLock; no active config/data file is changed here.
func prepareRecoveryStage(ctx context.Context, paths Paths, operation, trust string, run MaintenanceRunner, redis recoveryRedisRunner) (Paths, error) {
	return prepareRecoveryStageWithSpace(ctx, paths, operation, trust, run, redis, recoveryAvailableSpace)
}

func prepareRecoveryStageWithSpace(ctx context.Context, paths Paths, operation, trust string, run MaintenanceRunner, redis recoveryRedisRunner, available func(string) (uint64, error)) (Paths, error) {
	var empty Paths
	if ctx == nil || run == nil || redis == nil {
		return empty, errors.New("recovery staging requires offline runners")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	root := paths.InstallDir
	if err := requireMaintenanceInstanceLock(root); err != nil {
		return empty, err
	}
	outer, err := ReadMaintenanceJournal(root)
	if err != nil {
		return empty, err
	}
	if outer.OperationID != operation || (outer.Phase != MaintenanceRestoreRequired && outer.Phase != MaintenanceRestoring) {
		return empty, errors.New("invalid recovery staging operation")
	}
	if _, _, err := loadMaintenanceReleasesWithTrust(root, outer.CandidateVersion, trust); err != nil {
		return empty, err
	}
	releases, identity, err := installedMaintenanceReleaseSnapshot(root)
	if err != nil {
		return empty, err
	}
	if identity != outer.ReleaseSetSHA256 {
		return empty, errors.New("recovery release set changed")
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return empty, err
	}
	if _, err := os.Lstat(maintenancePermitPath(root)); !errors.Is(err, os.ErrNotExist) {
		return empty, errors.New("recovery staging has outstanding permit")
	}
	manifest, err := VerifyBackup(ctx, outer.BackupRoot)
	if err != nil {
		return empty, err
	}
	backupSHA, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
	if err != nil {
		return empty, err
	}
	if backupSHA != outer.BackupManifestSHA256 || manifest.Version != outer.OldVersion {
		return empty, errors.New("recovery backup identity mismatch")
	}
	old, err := LoadReleaseVersion(root, outer.OldVersion)
	if err != nil {
		return empty, err
	}
	gate := filepath.Join(root, maintenanceDirName)
	work := filepath.Join(gate, "work", operation)
	failed := filepath.Join(gate, "failed", operation)
	stage, err := ResolvePaths(PathOptions{InstallDir: root, ConfigDir: filepath.Join(work, "config"), DataDir: filepath.Join(work, "data"), ResourceDir: old.ResourceDir, WebDir: old.WebDir, RecordingsDir: paths.RecordingsDir})
	if err != nil {
		return empty, err
	}
	j, readErr := readRecoveryJournal(root)
	if errors.Is(readErr, os.ErrNotExist) {
		if outer.Phase != MaintenanceRestoreRequired {
			return empty, errors.New("restoring operation is missing recovery progress")
		}
		for _, path := range []string{work, failed} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return empty, errors.New("unowned recovery work already exists")
			}
		}
		j = recoveryJournal{Schema: 1, OperationID: operation, BackupManifestSHA256: outer.BackupManifestSHA256, OldVersion: outer.OldVersion, OldCurrentSHA256: outer.OldCurrentSHA256, ReleaseSetSHA256: identity, Phase: "staging"}
		if err := persistRecoveryJournal(root, nil, j); err != nil {
			return empty, err
		}
	} else if readErr != nil {
		return empty, readErr
	}
	if j.OperationID != operation || j.BackupManifestSHA256 != backupSHA || j.OldVersion != outer.OldVersion || j.OldCurrentSHA256 != outer.OldCurrentSHA256 || j.ReleaseSetSHA256 != identity {
		return empty, errors.New("recovery staging identity mismatch")
	}
	if j.Phase == "staged" {
		if outer.Phase != MaintenanceRestoring {
			return empty, errors.New("sealed recovery has invalid outer phase")
		}
		return stage, checkRecoveryDirectoryState(ctx, root, j, 1)
	}
	if j.Phase != "staging" {
		return empty, errors.New("recovery publication already started")
	}
	if outer.Phase == MaintenanceRestoreRequired {
		if err := advanceMaintenance(root, operation, MaintenanceRestoreRequired, MaintenanceRestoring); err != nil {
			return empty, err
		}
	}
	if entries, err := os.ReadDir(failed); err == nil {
		if len(entries) != 0 {
			return empty, errors.New("recovery failed scene already exists")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	configSHA, err := recoveryTreeIdentity(ctx, paths.ConfigDir)
	if err != nil {
		return empty, err
	}
	dataSHA, err := recoveryTreeIdentity(ctx, paths.DataDir)
	if err != nil {
		return empty, err
	}
	if _, err := os.Lstat(work); err == nil {
		if _, err := recoveryTreeIdentity(ctx, work); err != nil {
			return empty, err
		}
		// Only this operation's unsealed disposable work is removed. Active
		// data, the verified backup and any failed scene are never deleted.
		if err := os.RemoveAll(work); err != nil {
			return empty, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	if err := checkRecoverySpace(gate, manifest, available); err != nil {
		return empty, err
	}
	sourceRedis, control := filepath.Join(work, "redis-source"), filepath.Join(work, "redis-control")
	for _, dir := range []string{stage.ConfigDir, stage.DataDir, filepath.Join(stage.DataDir, "redis"), sourceRedis, control} {
		if err := mkdirRecoveryTree(gate, dir); err != nil {
			return empty, err
		}
	}
	for _, file := range manifest.Files {
		if !strings.HasPrefix(file.Path, "config/") && !strings.HasPrefix(file.Path, "data/") {
			continue
		}
		target := filepath.Join(work, filepath.FromSlash(file.Path))
		if strings.HasPrefix(file.Path, "data/redis/") {
			target = filepath.Join(sourceRedis, filepath.FromSlash(strings.TrimPrefix(file.Path, "data/redis/")))
		}
		if err := mkdirRecoveryTree(gate, filepath.Dir(target)); err != nil {
			return empty, err
		}
		source := filepath.Join(outer.BackupRoot, filepath.FromSlash(file.Path))
		switch file.Path {
		case "config/config.yml", "config/redis.conf", "config/zlm.ini":
			raw, err := os.ReadFile(source)
			if err != nil {
				return empty, err
			}
			if err := writeSecureConfigFile(target, raw, false, nil); err != nil {
				return empty, err
			}
		default:
			if err := backupCopyFile(ctx, source, target, nil); err != nil {
				return empty, err
			}
		}
		digest, err := releaseFileSHA256(target)
		if err != nil {
			return empty, err
		}
		if digest != file.SHA256 {
			return empty, errors.New("recovery copy checksum mismatch")
		}
	}
	if err := run(ctx, stage, operation, "revoke_sessions", old.Version); err != nil {
		return empty, errors.New("recovery session revocation failed")
	}
	cfg, err := LoadConfig(stage)
	if err != nil {
		return empty, err
	}
	if err := rotateRecoveryCredentials(stage, operation, cfg.ConfigSHA256, nil); err != nil {
		return empty, err
	}
	if err := redis(ctx, old.RedisExe, sourceRedis, filepath.Join(stage.DataDir, "redis"), control, configInt(cfg.values, "redis", "indexdb")); err != nil {
		return empty, errors.New("recovery Redis staging failed")
	}
	if err := run(ctx, stage, operation, "db_check", old.Version); err != nil {
		return empty, errors.New("recovery old database check failed")
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return empty, err
	}
	if _, err := os.Lstat(maintenancePermitPath(root)); !errors.Is(err, os.ErrNotExist) {
		return empty, errors.New("recovery staging permit remains")
	}
	if _, err := VerifyBackup(ctx, outer.BackupRoot); err != nil {
		return empty, err
	}
	if actual, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile)); err != nil || actual != backupSHA {
		return empty, errors.New("recovery backup changed during staging")
	}
	if _, actual, err := installedMaintenanceReleaseSnapshot(root); err != nil || actual != identity {
		return empty, errors.New("recovery release set changed during staging")
	}
	if actual, err := recoveryTreeIdentity(ctx, paths.ConfigDir); err != nil || actual != configSHA {
		return empty, errors.New("active configuration changed during staging")
	}
	if actual, err := recoveryTreeIdentity(ctx, paths.DataDir); err != nil || actual != dataSHA {
		return empty, errors.New("active data changed during staging")
	}
	next := j
	next.Phase = "staged"
	next.FailedConfigSHA256 = configSHA
	next.FailedDataSHA256 = dataSHA
	next.StagedConfigSHA256, err = recoveryTreeIdentity(ctx, stage.ConfigDir)
	if err != nil {
		return empty, err
	}
	next.StagedDataSHA256, err = recoveryTreeIdentity(ctx, stage.DataDir)
	if err != nil {
		return empty, err
	}
	if err := mkdirRecoveryTree(gate, failed); err != nil {
		return empty, err
	}
	if err := persistRecoveryJournal(root, &j, next); err != nil {
		return empty, err
	}
	return stage, nil
}

func mkdirRecoveryTree(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrPathOutsideInstall
	}
	if err := protectConfigDir(root, false); err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	path := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		err := os.Mkdir(path, 0700)
		created := err == nil
		if err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := protectConfigDir(path, created); err != nil {
			return err
		}
	}
	return nil
}

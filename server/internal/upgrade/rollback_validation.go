package upgrade

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

func expectedRollbackIdentity(record TaskRecord) (RuntimeIdentity, error) {
	if record.Preflight == nil {
		return RuntimeIdentity{}, errors.New("rollback requires original installation preflight")
	}
	if !validDigest(record.Preflight.MigrationDigest) || NormalizeDatabaseDialect(record.Preflight.DatabaseDialect) == "" {
		return RuntimeIdentity{}, errors.New("rollback requires original database identity")
	}
	expected := RuntimeIdentity{Version: record.Preflight.CurrentVersion, BuildID: record.Preflight.CurrentBuildID, TaskID: record.TaskID, DatabaseReady: true, DatabaseDialect: record.Preflight.DatabaseDialect, MigrationDigest: record.Preflight.MigrationDigest}
	if record.Preflight.DatabaseDialect != "sqlite" && !failedBeforeMigration(record) {
		if !record.Compatibility.AllowsAutomaticRollback() || !validDigest(record.Compatibility.Database.TargetMigrationDigest) {
			return RuntimeIdentity{}, errors.New("rollback requires signed compatibility with the migrated external database")
		}
		// External databases are never restored automatically. The previous
		// binary must validate the migrated state declared by the signer.
		expected.MigrationDigest = record.Compatibility.Database.TargetMigrationDigest
	}
	if err := validateExpectedRuntimeIdentity(expected); err != nil {
		return RuntimeIdentity{}, fmt.Errorf("rollback source identity: %w", err)
	}
	return expected, nil
}

func validateRollbackRuntime(ctx context.Context, layout RegisteredLayout, record TaskRecord, probe RuntimeIdentityProbe) error {
	expected, err := expectedRollbackIdentity(record)
	if err != nil {
		return err
	}
	probe.URL = layout.ReadinessURL
	return probe.Validate(ctx, expected)
}

// restoreRollbackDatabase is called only after all candidate writers have
// stopped, and before the old process starts. External databases are never
// overwritten automatically: only an explicit signed old-binary-compatible
// contract can authorize trying the old process against the migrated schema.
func restoreRollbackDatabase(ctx context.Context, layout RegisteredLayout, record TaskRecord) error {
	if record.Committed {
		return errors.New("committed task cannot restore a prior database snapshot")
	}
	if record.Preflight == nil {
		return errors.New("rollback requires database source evidence")
	}
	if failedBeforeMigration(record) {
		// No migration boundary has been entered yet.
		return nil
	}
	if record.Preflight.DatabaseDialect != "sqlite" {
		if !record.Compatibility.AllowsAutomaticRollback() {
			return errors.New("external database rollback lacks signed old-binary compatibility")
		}
		return nil
	}
	database, err := registeredSQLiteDatabase(layout, record)
	if err != nil {
		return err
	}
	if database == "" {
		return errors.New("registered SQLite restore destination is unavailable")
	}
	backupRoot := filepath.Join(layout.State, "backups", record.TaskID)
	complete, err := validateBackupComplete(backupRoot, database)
	if err != nil {
		return err
	}
	if !complete {
		return errors.New("SQLite rollback snapshot is incomplete")
	}
	return SQLiteRestore(ctx, filepath.Join(backupRoot, "database.sqlite"), database)
}

func failedBeforeMigration(record TaskRecord) bool {
	phase := record.Phase
	if phase == PhaseFailed || phase == PhaseRecoveryRequired {
		phase = record.FailurePhase
	}
	return phase == PhaseStopping || phase == PhaseBackingUp
}

// rollbackExecutionBoundary reports whether the task has crossed the durable
// service-switch boundary. Preparation failures do not have a release or
// database state to restore; invoking a deployment adapter for those tasks
// would stop a healthy service and turn a harmless failed download into a
// recovery incident. The boundary is retained in FailurePhase because the
// terminal phase alone cannot distinguish preparation from execution failure.
func rollbackExecutionBoundary(record TaskRecord) bool {
	if record.Committed {
		return false
	}
	phase := record.Phase
	if phase == PhaseFailed || phase == PhaseRecoveryRequired {
		phase = record.FailurePhase
	}
	switch phase {
	case PhaseStopping, PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating:
		return true
	default:
		return false
	}
}

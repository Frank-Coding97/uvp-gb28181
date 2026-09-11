package standalone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recoveryInterruptedProcessReady struct {
	Root      string `json:"root"`
	Operation string `json:"op"`
	Trust     string `json:"trust"`
	Purpose   string `json:"purpose"`
	Version   string `json:"version"`
	Phase     string `json:"phase"`
}

// This driver terminates the child OS process while it still owns the
// installation. It exercises recovery admission and filesystem publication;
// it does not claim that a real backend transaction was killed.
func TestRecoveryInterruptedCandidateProcessKill(t *testing.T) {
	for _, purpose := range []string{"bootstrap_db", "migrate_up", "db_check", "candidate_health"} {
		t.Run(purpose, func(t *testing.T) {
			ready, cmd := startRecoveryInterruptedChild(t, "candidate", purpose)
			root, op := ready.Root, ready.Operation
			paths, err := recoveryInterruptedPaths(root)
			require.NoError(t, err)
			beforeOuter, err := ReadMaintenanceJournal(root)
			require.NoError(t, err)
			require.Equal(t, MaintenanceCommitting, beforeOuter.Phase)
			permitRaw, err := readSecureConfigFile(maintenancePermitPath(root))
			require.NoError(t, err)

			_, err = AcquireInstanceLock(root)
			require.ErrorIs(t, err, ErrInstanceRunning)
			killRecoveryInterruptedChild(t, cmd)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)

			lock, err := AcquireInstanceLock(root)
			require.NoError(t, err)
			require.NoError(t, lock.Close())

			var calls []string
			result, err := restoreStoppedWithRunners(context.Background(), paths, op, ready.Trust,
				func(_ context.Context, staged Paths, operation, stagePurpose, version string) error {
					require.NotEqual(t, paths.DataDir, staged.DataDir)
					require.Equal(t, op, operation)
					require.Equal(t, beforeOuter.OldVersion, version)
					calls = append(calls, stagePurpose)
					return nil
				},
				func(_ context.Context, _, _, target, _ string, _ int) error {
					return os.WriteFile(filepath.Join(target, "interrupted-process-fixture"), []byte("restored"), 0600)
				})
			require.NoError(t, err)
			require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
			require.Equal(t, []string{"revoke_sessions", "db_check"}, calls)

			finalOuter, err := ReadMaintenanceJournal(root)
			require.NoError(t, err)
			require.Equal(t, MaintenanceAwaitingConfirmation, finalOuter.Phase)
			require.Equal(t, op, finalOuter.OperationID)
			progress, err := readRecoveryJournal(root)
			require.NoError(t, err)
			require.Equal(t, "pointer_restored", progress.Phase)
			require.NoError(t, checkRecoveryDirectoryState(context.Background(), root, progress, 6))
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
			current, err := releaseFileSHA256(filepath.Join(root, "current.json"))
			require.NoError(t, err)
			require.Equal(t, beforeOuter.OldCurrentSHA256, current)
			_, err = os.Lstat(maintenancePermitPath(root))
			require.ErrorIs(t, err, os.ErrNotExist)
			assertRetiredRecoveryPermit(t, root, permitRaw, op, purpose, ready.Version)
		})
	}
}

// A restoring-stage permit is retired without invoking backend or Redis code.
// The fixture only proves that an interrupted staging admission is resumable.
func TestRecoveryInterruptedRestoringProcessKill(t *testing.T) {
	for _, purpose := range []string{"revoke_sessions", "db_check"} {
		t.Run(purpose, func(t *testing.T) {
			ready, cmd := startRecoveryInterruptedChild(t, "restoring", purpose)
			root, op := ready.Root, ready.Operation
			beforeOuter, err := ReadMaintenanceJournal(root)
			require.NoError(t, err)
			require.Equal(t, MaintenanceRestoring, beforeOuter.Phase)
			beforeProgress, err := readRecoveryJournal(root)
			require.NoError(t, err)
			require.Equal(t, "staging", beforeProgress.Phase)
			permitRaw, err := readSecureConfigFile(maintenancePermitPath(root))
			require.NoError(t, err)
			beforeCurrent, err := os.ReadFile(filepath.Join(root, "current.json"))
			require.NoError(t, err)

			_, err = AcquireInstanceLock(root)
			require.ErrorIs(t, err, ErrInstanceRunning)
			killRecoveryInterruptedChild(t, cmd)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)

			lock, err := AcquireInstanceLock(root)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, lock.Close()) })
			result, err := admitInterruptedRecovery(context.Background(), root, op, ready.Trust, backupPublish)
			require.NoError(t, err)
			require.Equal(t, MaintenanceRestoring, result.Phase)

			afterOuter, err := ReadMaintenanceJournal(root)
			require.NoError(t, err)
			require.Equal(t, beforeOuter, afterOuter)
			afterProgress, err := readRecoveryJournal(root)
			require.NoError(t, err)
			require.Equal(t, beforeProgress, afterProgress)
			afterCurrent, err := os.ReadFile(filepath.Join(root, "current.json"))
			require.NoError(t, err)
			require.True(t, bytes.Equal(beforeCurrent, afterCurrent))
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
			_, err = os.Lstat(maintenancePermitPath(root))
			require.ErrorIs(t, err, os.ErrNotExist)
			assertRetiredRecoveryPermit(t, root, permitRaw, op, purpose, ready.Version)
			require.NoError(t, lock.Close())
			paths, err := recoveryInterruptedPaths(root)
			require.NoError(t, err)
			result, err = restoreStoppedWithRunners(context.Background(), paths, op, ready.Trust,
				func(context.Context, Paths, string, string, string) error { return nil },
				func(_ context.Context, _, _, target, _ string, _ int) error {
					return os.WriteFile(filepath.Join(target, "restored-after-kill"), []byte("fresh"), 0600)
				})
			require.NoError(t, err)
			require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
			assertRetiredRecoveryPermit(t, root, permitRaw, op, purpose, ready.Version)
		})
	}
}

// The callback has completed the permit rename, but the child is killed before
// admitInterruptedRecovery can persist its phase. A fresh admission must keep
// the archive and safely retry the phase normalization.
func TestRecoveryInterruptedPermitArchiveProcessKill(t *testing.T) {
	ready, cmd := startRecoveryInterruptedChild(t, "rename-after", "candidate_health")
	root, op := ready.Root, ready.Operation
	beforeCurrent, err := os.ReadFile(filepath.Join(root, "current.json"))
	require.NoError(t, err)
	_, err = AcquireInstanceLock(root)
	require.ErrorIs(t, err, ErrInstanceRunning)
	killRecoveryInterruptedChild(t, cmd)
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)

	before, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, MaintenanceCommitting, before.Phase)
	_, err = os.Lstat(maintenancePermitPath(root))
	require.ErrorIs(t, err, os.ErrNotExist)
	assertRetiredRecoveryPermitMetadata(t, root, op, ready.Purpose, ready.Version)

	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer lock.Close()
	result, err := admitInterruptedRecovery(context.Background(), root, op, ready.Trust, backupPublish)
	require.NoError(t, err)
	require.Equal(t, MaintenanceRestoreRequired, result.Phase)
	after, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, MaintenanceRestoreRequired, after.Phase)
	require.Equal(t, op, after.OperationID)
	afterCurrent, err := os.ReadFile(filepath.Join(root, "current.json"))
	require.NoError(t, err)
	require.True(t, bytes.Equal(beforeCurrent, afterCurrent))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	_, err = os.Lstat(maintenancePermitPath(root))
	require.ErrorIs(t, err, os.ErrNotExist)
	assertRetiredRecoveryPermitMetadata(t, root, op, ready.Purpose, ready.Version)
}

func TestRecoveryInterruptedCrashHelper(t *testing.T) {
	ready := os.Getenv("UVP_RECOVERY_INTERRUPTED_READY")
	mode := os.Getenv("UVP_RECOVERY_INTERRUPTED_MODE")
	purpose := os.Getenv("UVP_RECOVERY_INTERRUPTED_PURPOSE")
	if ready == "" || mode == "" || purpose == "" {
		t.Skip("requires isolated interrupted recovery crash driver")
	}

	owner := completedUpgradeFixture(t)
	root, op := owner.paths.InstallDir, owner.journal.OperationID
	switch mode {
	case "candidate":
		frame, err := IssueMaintenancePermit(root, op, purpose, owner.journal.CandidateVersion)
		clear(frame)
		require.NoError(t, err)
		writeRecoveryInterruptedReady(t, ready, recoveryInterruptedProcessReady{
			Root: root, Operation: op, Trust: owner.trust, Purpose: purpose,
			Version: owner.journal.CandidateVersion, Phase: mode,
		})
		time.Sleep(time.Hour)
	case "restoring":
		require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
		progress := recoveryJournal{
			Schema: 1, OperationID: op, BackupManifestSHA256: owner.journal.BackupManifestSHA256,
			OldVersion: owner.journal.OldVersion, OldCurrentSHA256: owner.journal.OldCurrentSHA256,
			ReleaseSetSHA256: owner.journal.ReleaseSetSHA256, Phase: "staging",
		}
		require.NoError(t, persistRecoveryJournal(root, nil, progress))
		require.NoError(t, advanceMaintenance(root, op, MaintenanceRestoreRequired, MaintenanceRestoring))
		frame, err := IssueMaintenancePermit(root, op, purpose, owner.journal.OldVersion)
		clear(frame)
		require.NoError(t, err)
		writeRecoveryInterruptedReady(t, ready, recoveryInterruptedProcessReady{
			Root: root, Operation: op, Trust: owner.trust, Purpose: purpose,
			Version: owner.journal.OldVersion, Phase: mode,
		})
		time.Sleep(time.Hour)
	case "rename-after":
		frame, err := IssueMaintenancePermit(root, op, purpose, owner.journal.CandidateVersion)
		clear(frame)
		require.NoError(t, err)
		_, err = admitInterruptedRecovery(context.Background(), root, op, owner.trust, func(source, target string) error {
			if err := backupPublish(source, target); err != nil {
				return err
			}
			writeRecoveryInterruptedReady(t, ready, recoveryInterruptedProcessReady{
				Root: root, Operation: op, Trust: owner.trust, Purpose: purpose,
				Version: owner.journal.CandidateVersion, Phase: mode,
			})
			time.Sleep(time.Hour)
			return nil
		})
		t.Fatalf("interrupted admission returned before parent termination: %v", err)
	default:
		t.Fatalf("unknown interrupted recovery crash mode %q", mode)
	}
	t.Fatal("interrupted recovery crash helper returned before parent termination")
}

func startRecoveryInterruptedChild(t *testing.T, mode, purpose string) (recoveryInterruptedProcessReady, *exec.Cmd) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	processRoot := t.TempDir()
	readyPath := filepath.Join(processRoot, "ready.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRecoveryInterruptedCrashHelper$", "-test.timeout=2m")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "TMP", "TEMP", "TMPDIR", "UVP_RECOVERY_INTERRUPTED_READY", "UVP_RECOVERY_INTERRUPTED_MODE", "UVP_RECOVERY_INTERRUPTED_PURPOSE":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env,
		"TMP="+processRoot,
		"TEMP="+processRoot,
		"TMPDIR="+processRoot,
		"UVP_RECOVERY_INTERRUPTED_READY="+readyPath,
		"UVP_RECOVERY_INTERRUPTED_MODE="+mode,
		"UVP_RECOVERY_INTERRUPTED_PURPOSE="+purpose,
	)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	var ready recoveryInterruptedProcessReady
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(readyPath)
		return err == nil && json.Unmarshal(raw, &ready) == nil && ready.Root != "" && ready.Operation != "" && ready.Trust != "" && ready.Purpose != "" && ready.Version != "" && ready.Phase != ""
	}, 10*time.Second, 5*time.Millisecond)
	relative, err := filepath.Rel(processRoot, ready.Root)
	require.NoError(t, err)
	require.NotEqual(t, "..", relative)
	require.False(t, strings.HasPrefix(relative, ".."+string(filepath.Separator)))
	return ready, cmd
}

func killRecoveryInterruptedChild(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
}

func writeRecoveryInterruptedReady(t *testing.T, path string, ready recoveryInterruptedProcessReady) {
	t.Helper()
	raw, err := json.Marshal(ready)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
}

func recoveryInterruptedPaths(root string) (Paths, error) {
	return ResolvePaths(PathOptions{
		InstallDir:    root,
		ConfigDir:     filepath.Join(root, "config"),
		ResourceDir:   filepath.Join(root, "resource"),
		WebDir:        filepath.Join(root, "web"),
		DataDir:       filepath.Join(root, "data"),
		RecordingsDir: filepath.Join(root, "recordings"),
	})
}

func assertRetiredRecoveryPermit(t *testing.T, root string, raw []byte, operation, purpose, version string) {
	t.Helper()
	digest := sha256.Sum256(raw)
	directory := filepath.Join(root, maintenanceDirName, "retired-permits")
	name := hex.EncodeToString(digest[:]) + ".json"
	archived, err := readSecureConfigFile(filepath.Join(directory, name))
	require.NoError(t, err)
	require.True(t, bytes.Equal(raw, archived))
	assertRecoveryPermitEnvelope(t, archived, operation, purpose, version)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, name, entries[0].Name())
}

func assertRetiredRecoveryPermitMetadata(t *testing.T, root, operation, purpose, version string) {
	t.Helper()
	directory := filepath.Join(root, maintenanceDirName, "retired-permits")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	name := entries[0].Name()
	require.Equal(t, ".json", filepath.Ext(name))
	_, err = hex.DecodeString(strings.TrimSuffix(name, ".json"))
	require.NoError(t, err)
	require.Len(t, strings.TrimSuffix(name, ".json"), sha256.Size*2)
	archived, err := readSecureConfigFile(filepath.Join(directory, name))
	require.NoError(t, err)
	digest := sha256.Sum256(archived)
	require.Equal(t, hex.EncodeToString(digest[:])+".json", name)
	assertRecoveryPermitEnvelope(t, archived, operation, purpose, version)
}

func assertRecoveryPermitEnvelope(t *testing.T, raw []byte, operation, purpose, version string) {
	t.Helper()
	var envelope maintenancePermitEnvelope
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.True(t, validMaintenancePermitEnvelope(envelope))
	require.Equal(t, operation, envelope.OperationID)
	require.Equal(t, purpose, envelope.Purpose)
	require.Equal(t, version, envelope.Version)
}

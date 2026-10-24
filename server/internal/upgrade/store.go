package upgrade

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

type TaskRequest struct {
	IdempotencyKey string
	Actor          string
	TargetDigest   string
}

// PreflightSnapshot binds approval to the installation state checked during
// preparation. A fresh independent snapshot must match before interruption;
// automatic execution additionally requires every compatibility field.
type PreflightSnapshot struct {
	CurrentVersion  string `json:"current_version,omitempty"`
	CurrentBuildID  string `json:"current_build_id,omitempty"`
	ConfigDigest    string `json:"config_digest,omitempty"`
	DatabaseDialect string `json:"database_dialect,omitempty"`
	MigrationDigest string `json:"migration_digest,omitempty"`
}

type TaskRecord struct {
	ReleaseNotes           string                    `json:"release_notes,omitempty"`
	PublishedAt            string                    `json:"published_at,omitempty"`
	TaskID                 string                    `json:"task_id"`
	IdempotencyKey         string                    `json:"idempotency_key"`
	Actor                  string                    `json:"actor"`
	TargetDigest           string                    `json:"target_digest"`
	Source                 string                    `json:"source,omitempty"`
	StagingPath            string                    `json:"staging_path,omitempty"`
	Version                string                    `json:"version,omitempty"`
	BuildID                string                    `json:"build_id,omitempty"`
	PreviousTarget         string                    `json:"previous_target,omitempty"`
	PreviousTargetCaptured bool                      `json:"previous_target_captured,omitempty"`
	BackupPath             string                    `json:"backup_path,omitempty"`
	Phase                  TaskPhase                 `json:"phase"`
	FailurePhase           TaskPhase                 `json:"failure_phase,omitempty"`
	Committed              bool                      `json:"committed"`
	Error                  string                    `json:"error,omitempty"`
	OutcomeReported        TaskPhase                 `json:"outcome_reported,omitempty"`
	Images                 []ImageArtifact           `json:"images,omitempty"`
	Compatibility          *CompatibilityDeclaration `json:"compatibility,omitempty"`
	PolicyVersion          uint64                    `json:"policy_version,omitempty"`
	PolicySnapshot         *AutoUpgradePolicy        `json:"policy_snapshot,omitempty"`
	Preflight              *PreflightSnapshot        `json:"preflight,omitempty"`
	UpdatedAt              string                    `json:"updated_at,omitempty"`
}

type TaskStore struct{ root string }

func OpenTaskStore(root string) (*TaskStore, error) {
	if root == "" {
		return nil, errors.New("task store root is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create task store: %w", err)
	}
	return &TaskStore{root: root}, nil
}

func (s *TaskStore) Close() error { return nil }

func (s *TaskStore) Submit(request TaskRequest) (TaskRecord, error) {
	if s == nil || s.root == "" {
		return TaskRecord{}, errors.New("task store is not initialized")
	}
	if request.IdempotencyKey == "" || request.Actor == "" || request.TargetDigest == "" {
		return TaskRecord{}, errors.New("idempotency key, actor and target digest are required")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "upgrade.lock"))
	if err != nil {
		return TaskRecord{}, ErrTaskBusy
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return TaskRecord{}, err
	}
	for _, record := range records {
		if record.IdempotencyKey != request.IdempotencyKey {
			continue
		}
		if record.Actor != request.Actor || record.TargetDigest != request.TargetDigest {
			return TaskRecord{}, ErrIdempotencyConflict
		}
		return record, nil
	}
	for _, record := range records {
		if !isTerminalPhase(record.Phase) || record.Phase == PhaseRecoveryRequired {
			return TaskRecord{}, ErrTaskBusy
		}
	}
	taskID, err := newTaskID()
	if err != nil {
		return TaskRecord{}, err
	}
	record := TaskRecord{TaskID: taskID, IdempotencyKey: request.IdempotencyKey, Actor: request.Actor, TargetDigest: request.TargetDigest, Phase: PhaseFetching, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.save(append(records, record)); err != nil {
		return TaskRecord{}, err
	}
	return record, nil
}

func (s *TaskStore) MarkPhase(taskID string, phase TaskPhase) error {
	if s == nil || s.root == "" {
		return errors.New("task store is not initialized")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "upgrade.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].TaskID != taskID {
			continue
		}
		state := TaskState{TaskID: records[i].TaskID, Phase: records[i].Phase, Committed: records[i].Committed}
		if err := state.Advance(phase); err != nil {
			return err
		}
		records[i].Phase, records[i].Committed = state.Phase, state.Committed
		records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return s.save(records)
	}
	return fmt.Errorf("task %q not found", taskID)
}

// UpdateMetadata records trusted preparation details without changing the
// state-machine phase. It is used to make recovery diagnostics durable.
func (s *TaskStore) UpdateMetadata(taskID string, update func(*TaskRecord) error) error {
	if s == nil || s.root == "" || update == nil {
		return errors.New("task store and metadata update are required")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "upgrade.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].TaskID != taskID {
			continue
		}
		if err := update(&records[i]); err != nil {
			return err
		}
		records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return s.save(records)
	}
	return fmt.Errorf("task %q not found", taskID)
}

func (s *TaskStore) List() ([]TaskRecord, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("task store is not initialized")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "upgrade.lock"))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	return s.load()
}

func (s *TaskStore) Get(taskID string) (TaskRecord, error) {
	if taskID == "" {
		return TaskRecord{}, errors.New("task ID is required")
	}
	records, err := s.List()
	if err != nil {
		return TaskRecord{}, err
	}
	for _, record := range records {
		if record.TaskID == taskID {
			return record, nil
		}
	}
	return TaskRecord{}, fmt.Errorf("task %q not found", taskID)
}

func (s *TaskStore) Cancel(taskID string) error {
	if taskID == "" {
		return errors.New("task ID is required")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "upgrade.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].TaskID != taskID {
			continue
		}
		switch records[i].Phase {
		case PhaseFetching, PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation:
			records[i].Phase = PhaseFailed
			records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return s.save(records)
		default:
			return fmt.Errorf("task %q cannot be cancelled after phase %s", taskID, records[i].Phase)
		}
	}
	return fmt.Errorf("task %q not found", taskID)
}

func (s *TaskStore) load() ([]TaskRecord, error) {
	path := filepath.Join(s.root, "tasks.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task index: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var records []TaskRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("decode task index: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode task index trailing data: %w", err)
	}
	for i := range records {
		records[i].Error = logging.SanitizeErrorDetail(records[i].Error)
	}
	return records, nil
}

func (s *TaskStore) save(records []TaskRecord) error {
	persisted := append([]TaskRecord(nil), records...)
	for i := range persisted {
		persisted[i].Error = logging.SanitizeErrorDetail(persisted[i].Error)
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal task index: %w", err)
	}
	path := filepath.Join(s.root, "tasks.json")
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create task index: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write task index: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync task index: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close task index: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace task index: %w", err)
	}
	dir, err := os.Open(s.root)
	if err != nil {
		return fmt.Errorf("open task store directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync task store directory: %w", err)
	}
	return nil
}

func isTerminalPhase(phase TaskPhase) bool {
	return phase == PhaseSucceeded || phase == PhaseFailed || phase == PhaseRolledBack || phase == PhaseRecoveryRequired
}

func newTaskID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate task ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

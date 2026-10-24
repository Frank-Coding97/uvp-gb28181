package upgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type TaskPhase string

const (
	PhaseFetching             TaskPhase = "fetching"
	PhaseVerifying            TaskPhase = "verifying"
	PhasePreparing            TaskPhase = "preparing"
	PhaseAwaitingConfirmation TaskPhase = "awaiting_confirmation"
	PhaseStopping             TaskPhase = "stopping"
	PhaseBackingUp            TaskPhase = "backing_up"
	PhaseMigrating            TaskPhase = "migrating"
	PhaseActivating           TaskPhase = "activating"
	PhaseValidating           TaskPhase = "validating"
	PhaseCommitting           TaskPhase = "committing"
	PhaseSucceeded            TaskPhase = "succeeded"
	PhaseFailed               TaskPhase = "failed"
	PhaseRolledBack           TaskPhase = "rolled_back"
	PhaseRecoveryRequired     TaskPhase = "recovery_required"
)

type TaskState struct {
	TaskID    string    `json:"task_id"`
	Phase     TaskPhase `json:"phase"`
	Committed bool      `json:"committed"`
}

func NewTaskState(taskID string) TaskState { return TaskState{TaskID: taskID, Phase: PhaseFetching} }

func (s *TaskState) Advance(next TaskPhase) error {
	if s == nil || s.TaskID == "" {
		return errors.New("task state and task ID are required")
	}
	if !allowedTaskTransition(s.Phase, next) {
		return fmt.Errorf("invalid task transition %s -> %s", s.Phase, next)
	}
	s.Phase = next
	if next == PhaseCommitting || next == PhaseSucceeded {
		s.Committed = true
	}
	return nil
}

func (s TaskState) IsCommitted() bool { return s.Committed }

func allowedTaskTransition(current, next TaskPhase) bool {
	if current == PhaseSucceeded || current == PhaseRolledBack {
		return false
	}
	if next == PhaseRolledBack {
		return current != PhaseSucceeded && current != PhaseRolledBack && current != PhaseFetching
	}
	if current == PhaseFailed {
		return next == PhaseRecoveryRequired
	}
	if current == PhaseRecoveryRequired {
		return next == PhaseRolledBack
	}
	if next == PhaseFailed || next == PhaseRecoveryRequired {
		return true
	}
	if current == PhaseFetching {
		return next == PhaseVerifying
	}
	if current == PhaseVerifying {
		return next == PhasePreparing
	}
	if current == PhasePreparing {
		return next == PhaseAwaitingConfirmation
	}
	if current == PhaseAwaitingConfirmation {
		return next == PhaseStopping
	}
	return map[TaskPhase]TaskPhase{PhaseStopping: PhaseBackingUp, PhaseBackingUp: PhaseMigrating, PhaseMigrating: PhaseActivating, PhaseActivating: PhaseValidating, PhaseValidating: PhaseCommitting, PhaseCommitting: PhaseSucceeded}[current] == next
}

func SaveTaskState(path string, state TaskState) error {
	if path == "" || state.TaskID == "" || state.Phase == "" {
		return errors.New("state path, task ID and phase are required")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal task state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create task state directory: %w", err)
	}
	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create task state temporary file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write task state: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync task state: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close task state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace task state: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open task state directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync task state directory: %w", err)
	}
	return nil
}

func LoadTaskState(path string) (TaskState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskState{}, fmt.Errorf("read task state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state TaskState
	if err := decoder.Decode(&state); err != nil {
		return TaskState{}, fmt.Errorf("decode task state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return TaskState{}, errors.New("task state contains multiple JSON values")
		}
		return TaskState{}, fmt.Errorf("decode task state trailing data: %w", err)
	}
	if state.TaskID == "" || state.Phase == "" {
		return TaskState{}, errors.New("task state identity is incomplete")
	}
	return state, nil
}

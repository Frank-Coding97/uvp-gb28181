package upgrade

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const ProgressURLPrefix = "/api/system/upgrade/progress/"
const progressRetention = 24 * time.Hour

// ProgressWatch is a task-scoped read capability, delivered only over the
// authenticated management API. The public directory contains no task index.
type ProgressWatch struct {
	TaskID string `json:"task_id"`
	URL    string `json:"url"`
}

type ProgressTask struct {
	TaskID       string    `json:"task_id"`
	Phase        TaskPhase `json:"phase"`
	FailurePhase TaskPhase `json:"failure_phase,omitempty"`
	Version      string    `json:"version,omitempty"`
	BuildID      string    `json:"build_id,omitempty"`
	TargetDigest string    `json:"target_digest"`
	Committed    bool      `json:"committed"`
	UpdatedAt    string    `json:"updated_at,omitempty"`
}

type ProgressSnapshot struct {
	Task       ProgressTask `json:"task"`
	ObservedAt string       `json:"observed_at"`
}

type ProgressPublisher struct {
	store *TaskStore
	root  string
	key   []byte
	mu    sync.Mutex
}

// EnableProgress runs before any task workers. It never changes the durable
// task schema, so an older helper can still recover the installation.
func (s *TaskStore) EnableProgress(root string) (*ProgressPublisher, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("progress directory must be absolute")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("progress directory must be a real directory")
	}
	if err := os.Chmod(root, 0755); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(s.root, "progress.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = writeFileAtomically(keyPath, key, 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid progress signing key")
	}
	p := &ProgressPublisher{store: s, root: root, key: key}
	if err := p.Refresh(); err != nil {
		return nil, err
	}
	s.progress = p
	return p, nil
}

func (p *ProgressPublisher) filename(taskID string) string {
	mac := hmac.New(sha256.New, p.key)
	_, _ = mac.Write([]byte("uvp-upgrade-progress-v1:" + taskID))
	return hex.EncodeToString(mac.Sum(nil)) + ".json"
}

func progressVisible(record TaskRecord) bool {
	if !isTerminalPhase(record.Phase) || record.Phase == PhaseRecoveryRequired {
		return true
	}
	updated, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	return err == nil && time.Since(updated) <= progressRetention
}

func (p *ProgressPublisher) Watch(taskID string) (ProgressWatch, error) {
	if !validTaskID(taskID) {
		return ProgressWatch{}, errors.New("invalid progress task ID")
	}
	record, err := p.store.Get(taskID)
	if err != nil {
		return ProgressWatch{}, err
	}
	if !progressVisible(record) {
		return ProgressWatch{}, errors.New("progress task expired")
	}
	if err := p.Refresh(); err != nil {
		return ProgressWatch{}, err
	}
	return ProgressWatch{TaskID: taskID, URL: ProgressURLPrefix + p.filename(taskID)}, nil
}

func (p *ProgressPublisher) publishLocked(records []TaskRecord) error {
	// The private key and recovery state must never be served by nginx. The
	// snapshot deliberately omits actor, paths, policy and raw error messages.
	info, err := os.Lstat(p.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("progress directory is unavailable")
	}
	for _, record := range records {
		path := filepath.Join(p.root, p.filename(record.TaskID))
		if !progressVisible(record) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		snapshot := ProgressSnapshot{Task: ProgressTask{
			TaskID: record.TaskID, Phase: record.Phase, FailurePhase: record.FailurePhase,
			Version: record.Version, BuildID: record.BuildID, TargetDigest: record.TargetDigest,
			Committed: record.Committed, UpdatedAt: record.UpdatedAt,
		}, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if err := writeFileAtomically(path, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (p *ProgressPublisher) publish(records []TaskRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.publishLocked(records)
}

// Refresh reads the authoritative atomic task index under the publishing lock,
// preventing a delayed heartbeat from overwriting a more recent phase.
func (p *ProgressPublisher) Refresh() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// TaskStore publishes an atomic rename. Do not take upgrade.lock here:
	// phase writes hold it while acquiring the publishing mutex.
	records, err := p.store.load()
	if err != nil {
		return err
	}
	return p.publishLocked(records)
}

func (p *ProgressPublisher) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	reported := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.Refresh(); err != nil {
				// Publication is display-only. A broken web directory cannot abort a
				// privileged recovery, and an old snapshot naturally loses freshness.
				if !reported {
					fmt.Fprintln(os.Stderr, "upgrade progress publication unavailable")
				}
				reported = true
			} else {
				reported = false
			}
		}
	}
}

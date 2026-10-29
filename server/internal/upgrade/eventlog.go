package upgrade

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

// UpgradeEvent is one durable line of the upgrade event log.
//
// PolicyVersion only exists so that event logs written before automatic
// upgrades were removed keep decoding. Nothing sets it today; do not repurpose
// it, and do not delete it while historical log files may still be read.
type UpgradeEvent struct {
	Event         string    `json:"event"`
	TaskID        string    `json:"task_id,omitempty"`
	PolicyVersion uint64    `json:"policy_version,omitempty"`
	ReleaseID     string    `json:"release_id,omitempty"`
	BuildID       string    `json:"build_id,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	At            time.Time `json:"at"`
}

type UpgradeEventSink interface {
	Append(context.Context, UpgradeEvent) error
}

type FileEventLog struct {
	path string
	mu   sync.Mutex
}

func OpenFileEventLog(path string) (*FileEventLog, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("event log path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return &FileEventLog{path: path}, nil
}

func (l *FileEventLog) Append(ctx context.Context, event UpgradeEvent) error {
	if l == nil || l.path == "" {
		return errors.New("event log is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	event.Reason = logging.SanitizeErrorDetail(event.Reason)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	lock, err := acquireUpgradeLockWait(l.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	// A crash may occur after append/fsync but before the task acknowledges
	// its outcome. Deduplicate by durable task identity during that retry.
	if event.TaskID != "" && isTerminalPhase(TaskPhase(event.Event)) {
		file, err := os.Open(l.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				var previous UpgradeEvent
				if err := json.Unmarshal(scanner.Bytes(), &previous); err != nil {
					return err
				}
				if previous.TaskID == event.TaskID && previous.Event == event.Event {
					return nil
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
		}
	}
	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (l *FileEventLog) Recent(limit int) ([]UpgradeEvent, error) {
	if l == nil || l.path == "" {
		return nil, errors.New("event log is not initialized")
	}
	if limit <= 0 {
		limit = 100
	}
	file, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	all := make([]UpgradeEvent, 0, limit)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event UpgradeEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		event.Reason = logging.SanitizeErrorDetail(event.Reason)
		all = append(all, event)
		if len(all) > limit {
			copy(all, all[len(all)-limit:])
			all = all[:limit]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

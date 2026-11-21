package upgrade

import (
	"io"
	"sync"
	"time"
)

// TaskTransferProgress is a cosmetic snapshot of the artifact download that
// belongs to the active preparation task. It is answered over the helper RPC
// only and is deliberately **not** written into TaskRecord:
//
//	tasks.json is decoded with DisallowUnknownFields, so a new field would make
//	an older helper (i.e. the binary you roll back to) fail to read the task
//	index it wrote itself, and the upgrade page would go dark after a rollback.
//
// The reporter lives in the helper process that runs the download and answers
// Status, so the numbers never have to cross a file or a schema boundary.
type TaskTransferProgress struct {
	TaskID string `json:"task_id"`
	// DownloadedBytes and TotalBytes are only meaningful while Phase claims a
	// preparation phase; the business side re-checks that before showing them.
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	UpdatedAt       string `json:"updated_at"`
}

const (
	// Report often enough that a healthy download is visibly moving with the
	// page's 5s polling, but never more than four small file writes a second.
	transferReportInterval = time.Second
	transferReportBytes    = 2 << 20
)

// transferReporter accumulates bytes written by the download and publishes a
// throttled snapshot. Publishing is best effort: a slow status reader must
// never slow the download down or fail it.
type transferReporter struct {
	taskID  string
	total   int64
	now     func() time.Time
	publish func(TaskTransferProgress)
	clear   func(string)

	mu         sync.Mutex
	downloaded int64
	published  int64
	lastAt     time.Time
}

func (r *transferReporter) Add(n int) {
	if r == nil || n <= 0 {
		return
	}
	r.mu.Lock()
	r.downloaded += int64(n)
	downloaded := r.downloaded
	due := r.lastAt.IsZero() ||
		downloaded-r.published >= transferReportBytes ||
		r.now().Sub(r.lastAt) >= transferReportInterval
	if !due {
		r.mu.Unlock()
		return
	}
	r.published, r.lastAt = downloaded, r.now()
	r.mu.Unlock()
	r.emit(downloaded)
}

// Finish publishes the final byte count even when the last chunk arrived inside
// the throttle window, then drops the snapshot so a prepared task never shows a
// stale full bar. It is safe to call twice.
func (r *transferReporter) Finish() {
	if r == nil {
		return
	}
	r.mu.Lock()
	downloaded := r.downloaded
	r.mu.Unlock()
	r.emit(downloaded)
	if r.clear != nil {
		r.clear(r.taskID)
	}
}

func (r *transferReporter) emit(downloaded int64) {
	if r == nil || r.publish == nil {
		return
	}
	r.publish(TaskTransferProgress{
		TaskID:          r.taskID,
		DownloadedBytes: downloaded,
		TotalBytes:      r.total,
		UpdatedAt:       r.now().UTC().Format(time.RFC3339Nano),
	})
}

// transferWriter tees download bytes into the reporter without changing the
// number of bytes the caller sees written.
type transferWriter struct {
	dst      io.Writer
	reporter *transferReporter
}

func (w *transferWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 {
		w.reporter.Add(n)
	}
	return n, err
}

func (s *ControlService) beginTransfer(taskID string, total int64) *transferReporter {
	if s == nil || taskID == "" || total <= 0 {
		return nil
	}
	return &transferReporter{
		taskID:  taskID,
		total:   total,
		now:     s.now,
		publish: s.setTransfer,
		clear:   s.clearTransfer,
	}
}

func (s *ControlService) setTransfer(progress TaskTransferProgress) {
	if s == nil {
		return
	}
	s.transferMu.Lock()
	s.transfer = &progress
	s.transferMu.Unlock()
}

// TransferFor returns the download snapshot only when it belongs to taskID, so
// a finished or unrelated task can never inherit a stale bar.
func (s *ControlService) TransferFor(taskID string) *TaskTransferProgress {
	if s == nil || taskID == "" {
		return nil
	}
	s.transferMu.Lock()
	defer s.transferMu.Unlock()
	if s.transfer == nil || s.transfer.TaskID != taskID {
		return nil
	}
	snapshot := *s.transfer
	return &snapshot
}

func (s *ControlService) clearTransfer(taskID string) {
	if s == nil {
		return
	}
	s.transferMu.Lock()
	if s.transfer != nil && (taskID == "" || s.transfer.TaskID == taskID) {
		s.transfer = nil
	}
	s.transferMu.Unlock()
}

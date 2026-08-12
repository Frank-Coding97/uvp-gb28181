package recording

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const (
	DownloadStatusReady     = "ready"
	DownloadStatusStreaming = "streaming"
	DownloadStatusCompleted = "completed"
	DownloadStatusFailed    = "failed"
	DownloadStatusCancelled = "cancelled"
	DownloadStatusExpired   = "expired"

	defaultDownloadReadyTTL       = time.Minute
	defaultDownloadTerminalTTL    = 30 * time.Minute
	defaultDownloadPerUserLimit   = 2
	defaultDownloadPerInstanceMax = 16
)

var (
	ErrDownloadNotFound      = errors.New("download task unavailable")
	ErrDownloadNotOwner      = errors.New("download task unavailable")
	ErrDownloadTicketInvalid = errors.New("download ticket invalid")
	ErrDownloadExpired       = errors.New("download task expired")
	ErrDownloadLimit         = errors.New("download concurrency limit reached")
	ErrDownloadState         = errors.New("download task state invalid")
)

type DownloadTaskView struct {
	TaskID     string     `json:"taskId"`
	FileID     string     `json:"fileId"`
	Status     string     `json:"status"`
	BytesSent  uint64     `json:"bytesSent"`
	TotalBytes *uint64    `json:"totalBytes,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	ErrorCode  string     `json:"errorCode,omitempty"`
}

type DownloadRegistryConfig struct {
	Now                  func() time.Time
	ReadyTTL             time.Duration
	TerminalTTL          time.Duration
	PerUserStreaming     int
	PerInstanceStreaming int
}

type downloadTask struct {
	taskID       string
	ticketDigest [sha256.Size]byte
	ownerUserID  uint
	fileID       string
	status       string
	bytesSent    uint64
	totalBytes   *uint64
	createdAt    time.Time
	startedAt    *time.Time
	finishedAt   *time.Time
	expiresAt    time.Time
	errorCode    string
	cancel       context.CancelFunc
	ctx          context.Context
}

type DownloadRegistry struct {
	mu                   sync.Mutex
	now                  func() time.Time
	readyTTL             time.Duration
	terminalTTL          time.Duration
	perUserStreaming     int
	perInstanceStreaming int
	tasks                map[string]*downloadTask
	streamingByUser      map[uint]int
	streamingTotal       int
}

func NewDownloadRegistry(config DownloadRegistryConfig) *DownloadRegistry {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ReadyTTL <= 0 {
		config.ReadyTTL = defaultDownloadReadyTTL
	}
	if config.TerminalTTL <= 0 {
		config.TerminalTTL = defaultDownloadTerminalTTL
	}
	if config.PerUserStreaming <= 0 {
		config.PerUserStreaming = defaultDownloadPerUserLimit
	}
	if config.PerInstanceStreaming <= 0 {
		config.PerInstanceStreaming = defaultDownloadPerInstanceMax
	}
	return &DownloadRegistry{
		now: config.Now, readyTTL: config.ReadyTTL, terminalTTL: config.TerminalTTL,
		perUserStreaming: config.PerUserStreaming, perInstanceStreaming: config.PerInstanceStreaming,
		tasks: make(map[string]*downloadTask), streamingByUser: make(map[uint]int),
	}
}

func (r *DownloadRegistry) Create(ownerUserID uint, fileID string) (DownloadTaskView, string, error) {
	if r == nil || ownerUserID == 0 || fileID == "" {
		return DownloadTaskView{}, "", ErrDownloadState
	}
	taskID, err := randomDownloadID()
	if err != nil {
		return DownloadTaskView{}, "", err
	}
	ticket, err := randomDownloadID()
	if err != nil {
		return DownloadTaskView{}, "", err
	}
	now := r.currentTime()
	task := &downloadTask{taskID: taskID, ticketDigest: sha256.Sum256([]byte(ticket)), ownerUserID: ownerUserID, fileID: fileID, status: DownloadStatusReady, createdAt: now, expiresAt: now.Add(r.readyTTL)}
	r.mu.Lock()
	r.expireLocked(now)
	r.tasks[taskID] = task
	r.mu.Unlock()
	return task.view(), ticket, nil
}

func (r *DownloadRegistry) Claim(taskID, ticket string) (DownloadTaskView, context.Context, context.CancelFunc, error) {
	if r == nil || taskID == "" || ticket == "" {
		return DownloadTaskView{}, nil, nil, ErrDownloadTicketInvalid
	}
	now := r.currentTime()
	digest := sha256.Sum256([]byte(ticket))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	task, ok := r.tasks[taskID]
	if !ok {
		return DownloadTaskView{}, nil, nil, ErrDownloadTicketInvalid
	}
	if task.status == DownloadStatusExpired {
		return DownloadTaskView{}, nil, nil, ErrDownloadExpired
	}
	if task.status != DownloadStatusReady || task.ticketDigest != digest {
		return DownloadTaskView{}, nil, nil, ErrDownloadTicketInvalid
	}
	if r.streamingTotal >= r.perInstanceStreaming || r.streamingByUser[task.ownerUserID] >= r.perUserStreaming {
		return DownloadTaskView{}, nil, nil, ErrDownloadLimit
	}
	task.ctx, task.cancel = context.WithCancel(context.Background())
	task.status = DownloadStatusStreaming
	task.ticketDigest = [sha256.Size]byte{}
	task.startedAt = &now
	task.expiresAt = time.Time{}
	r.streamingTotal++
	r.streamingByUser[task.ownerUserID]++
	return task.view(), task.ctx, task.cancel, nil
}

func (r *DownloadRegistry) Get(taskID string, ownerUserID uint) (DownloadTaskView, error) {
	if r == nil || taskID == "" || ownerUserID == 0 {
		return DownloadTaskView{}, ErrDownloadNotFound
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	task, ok := r.tasks[taskID]
	if !ok {
		return DownloadTaskView{}, ErrDownloadNotFound
	}
	if task.ownerUserID != ownerUserID {
		return DownloadTaskView{}, ErrDownloadNotOwner
	}
	return task.view(), nil
}

func (r *DownloadRegistry) Owner(taskID string) (uint, bool) {
	if r == nil || taskID == "" {
		return 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[taskID]
	if !ok || task.status != DownloadStatusStreaming {
		return 0, false
	}
	return task.ownerUserID, true
}

func (r *DownloadRegistry) Cancel(taskID string, ownerUserID uint) (DownloadTaskView, error) {
	if r == nil || taskID == "" || ownerUserID == 0 {
		return DownloadTaskView{}, ErrDownloadNotFound
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	task, ok := r.tasks[taskID]
	if !ok {
		return DownloadTaskView{}, ErrDownloadNotFound
	}
	if task.ownerUserID != ownerUserID {
		return DownloadTaskView{}, ErrDownloadNotOwner
	}
	if task.status == DownloadStatusCancelled {
		return task.view(), nil
	}
	if task.status != DownloadStatusReady && task.status != DownloadStatusStreaming {
		return DownloadTaskView{}, ErrDownloadState
	}
	r.finishLocked(task, DownloadStatusCancelled, "", now)
	return task.view(), nil
}

func (r *DownloadRegistry) Finish(taskID, status, errorCode string) {
	if r == nil || !downloadTerminalStatus(status) {
		return
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[taskID]
	if !ok || task.status != DownloadStatusStreaming {
		return
	}
	r.finishLocked(task, status, errorCode, now)
}

func (r *DownloadRegistry) SetTotal(taskID string, total uint64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if task := r.tasks[taskID]; task != nil && task.status == DownloadStatusStreaming {
		value := total
		task.totalBytes = &value
	}
}

func (r *DownloadRegistry) AddBytes(taskID string, written uint64) {
	if r == nil || written == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[taskID]
	if task == nil || task.status != DownloadStatusStreaming {
		return
	}
	task.bytesSent += written
	if task.totalBytes != nil && task.bytesSent > *task.totalBytes {
		task.bytesSent = *task.totalBytes
	}
}

func (r *DownloadRegistry) Expire(taskID string) error {
	if r == nil {
		return ErrDownloadNotFound
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[taskID]
	if !ok {
		return ErrDownloadNotFound
	}
	if task.status == DownloadStatusReady && !now.Before(task.expiresAt) {
		task.status = DownloadStatusExpired
		task.finishedAt = &now
		task.expiresAt = now.Add(r.terminalTTL)
		task.ticketDigest = [sha256.Size]byte{}
		return ErrDownloadExpired
	}
	if task.status == DownloadStatusExpired {
		return ErrDownloadExpired
	}
	return nil
}

func (r *DownloadRegistry) DebugString() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := ""
	for id, task := range r.tasks {
		result += id + ":" + task.status + ";"
	}
	return result
}

func (r *DownloadRegistry) currentTime() time.Time { return r.now().UTC() }

func (r *DownloadRegistry) expireLocked(now time.Time) {
	for id, task := range r.tasks {
		if task.status == DownloadStatusReady && !now.Before(task.expiresAt) {
			task.status = DownloadStatusExpired
			task.finishedAt = &now
			task.expiresAt = now.Add(r.terminalTTL)
			task.ticketDigest = [sha256.Size]byte{}
		}
		if downloadTerminalStatus(task.status) && !task.expiresAt.IsZero() && !now.Before(task.expiresAt) {
			delete(r.tasks, id)
		}
	}
}

func (r *DownloadRegistry) finishLocked(task *downloadTask, status, errorCode string, now time.Time) {
	if task.status == DownloadStatusStreaming {
		r.streamingTotal--
		r.streamingByUser[task.ownerUserID]--
		if r.streamingByUser[task.ownerUserID] == 0 {
			delete(r.streamingByUser, task.ownerUserID)
		}
	}
	if task.cancel != nil {
		task.cancel()
		task.cancel = nil
	}
	task.status, task.errorCode = status, errorCode
	task.finishedAt = &now
	task.expiresAt = now.Add(r.terminalTTL)
}

func (task *downloadTask) view() DownloadTaskView {
	if task == nil {
		return DownloadTaskView{}
	}
	return DownloadTaskView{TaskID: task.taskID, FileID: task.fileID, Status: task.status, BytesSent: task.bytesSent, TotalBytes: copyUint64(task.totalBytes), CreatedAt: task.createdAt, StartedAt: copyTime(task.startedAt), FinishedAt: copyTime(task.finishedAt), ExpiresAt: task.expiresAt, ErrorCode: task.errorCode}
}

func downloadTerminalStatus(status string) bool {
	return status == DownloadStatusCompleted || status == DownloadStatusFailed || status == DownloadStatusCancelled || status == DownloadStatusExpired
}

func randomDownloadID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

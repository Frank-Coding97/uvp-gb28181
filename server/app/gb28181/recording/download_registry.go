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

	defaultDownloadReadyTTL        = time.Minute
	defaultDownloadTerminalTTL     = 30 * time.Minute
	defaultDownloadPerUserLimit    = 2
	defaultDownloadPerInstanceMax  = 16
	defaultDownloadTerminalPerUser = 20
	downloadProgressWindow         = 5 * time.Second
	downloadProgressSampleInterval = time.Second
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
	TaskID              string     `json:"taskId"`
	FileID              string     `json:"fileId"`
	Status              string     `json:"status"`
	BytesSent           uint64     `json:"bytesSent"`
	TotalBytes          *uint64    `json:"totalBytes,omitempty"`
	SpeedBytesPerSecond *uint64    `json:"speedBytesPerSecond,omitempty"`
	ETASeconds          *uint64    `json:"etaSeconds,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	StartedAt           *time.Time `json:"startedAt,omitempty"`
	FinishedAt          *time.Time `json:"finishedAt,omitempty"`
	ExpiresAt           time.Time  `json:"expiresAt"`
	ErrorCode           string     `json:"errorCode,omitempty"`
}

type downloadProgressSample struct {
	at        time.Time
	bytesSent uint64
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
	progress     []downloadProgressSample
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
	closed               bool
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
	if r.closed {
		r.mu.Unlock()
		return DownloadTaskView{}, "", ErrDownloadState
	}
	r.expireLocked(now)
	// 未领取任务无界创建的防线:ready+streaming 任务总量与 streaming 共用
	// 同一实例/用户预算,创建时即拒绝,而不是等 Claim 才限制
	userActive, instanceActive := r.countActiveLocked()
	if userActive[ownerUserID] >= r.perUserStreaming || instanceActive >= r.perInstanceStreaming {
		r.mu.Unlock()
		return DownloadTaskView{}, "", ErrDownloadLimit
	}
	r.tasks[taskID] = task
	r.mu.Unlock()
	return task.viewAt(now), ticket, nil
}

// countActiveLocked 统计 ready+streaming 任务数(按用户与实例),调用方须持锁.
func (r *DownloadRegistry) countActiveLocked() (map[uint]int, int) {
	byUser := make(map[uint]int)
	total := 0
	for _, t := range r.tasks {
		if t.status == DownloadStatusReady || t.status == DownloadStatusStreaming {
			byUser[t.ownerUserID]++
			total++
		}
	}
	return byUser, total
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
	if r.closed {
		return DownloadTaskView{}, nil, nil, ErrDownloadState
	}
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
	task.progress = []downloadProgressSample{{at: now, bytesSent: 0}}
	r.streamingTotal++
	r.streamingByUser[task.ownerUserID]++
	return task.viewAt(now), task.ctx, task.cancel, nil
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
	return task.viewAt(now), nil
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
		return task.viewAt(now), nil
	}
	if task.status != DownloadStatusReady && task.status != DownloadStatusStreaming {
		return DownloadTaskView{}, ErrDownloadState
	}
	r.finishLocked(task, DownloadStatusCancelled, "", now)
	return task.viewAt(now), nil
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
	r.recordProgressLocked(task, r.currentTime())
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
		r.trimTerminalLocked(task.ownerUserID)
		return ErrDownloadExpired
	}
	if task.status == DownloadStatusExpired {
		return ErrDownloadExpired
	}
	return nil
}

// Close makes the in-process registry reject all future work and cancels every
// active transfer during a runtime stop. Tasks intentionally remain ephemeral.
func (r *DownloadRegistry) Close() {
	if r == nil {
		return
	}
	now := r.currentTime()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for _, task := range r.tasks {
		if task.status == DownloadStatusReady || task.status == DownloadStatusStreaming {
			r.finishLocked(task, DownloadStatusCancelled, "shutdown", now)
		}
	}
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
			r.trimTerminalLocked(task.ownerUserID)
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
	r.trimTerminalLocked(task.ownerUserID)
}

func (r *DownloadRegistry) trimTerminalLocked(ownerUserID uint) {
	terminal := make([]*downloadTask, 0, defaultDownloadTerminalPerUser+1)
	for _, task := range r.tasks {
		if task.ownerUserID == ownerUserID && downloadTerminalStatus(task.status) {
			terminal = append(terminal, task)
		}
	}
	for len(terminal) > defaultDownloadTerminalPerUser {
		oldest := 0
		for i := 1; i < len(terminal); i++ {
			if terminal[i].finishedAt.Before(*terminal[oldest].finishedAt) || (terminal[i].finishedAt.Equal(*terminal[oldest].finishedAt) && terminal[i].taskID < terminal[oldest].taskID) {
				oldest = i
			}
		}
		delete(r.tasks, terminal[oldest].taskID)
		terminal = append(terminal[:oldest], terminal[oldest+1:]...)
	}
}

func (r *DownloadRegistry) recordProgressLocked(task *downloadTask, now time.Time) {
	if len(task.progress) == 0 {
		task.progress = append(task.progress, downloadProgressSample{at: now, bytesSent: task.bytesSent})
		return
	}
	lastIndex := len(task.progress) - 1
	last := task.progress[lastIndex]
	if now.Before(last.at) {
		return
	}
	if len(task.progress) == 1 || now.Sub(last.at) >= downloadProgressSampleInterval {
		task.progress = append(task.progress, downloadProgressSample{at: now, bytesSent: task.bytesSent})
	}
	r.trimProgressLocked(task, now)
}

func (r *DownloadRegistry) trimProgressLocked(task *downloadTask, now time.Time) {
	cutoff := now.Add(-downloadProgressWindow)
	keepFrom := 0
	for index := 1; index < len(task.progress); index++ {
		if task.progress[index].at.After(cutoff) {
			break
		}
		keepFrom = index
	}
	if keepFrom > 0 {
		task.progress = task.progress[keepFrom:]
	}
}

func (task *downloadTask) viewAt(now time.Time) DownloadTaskView {
	if task == nil {
		return DownloadTaskView{}
	}
	view := DownloadTaskView{TaskID: task.taskID, FileID: task.fileID, Status: task.status, BytesSent: task.bytesSent, TotalBytes: copyUint64(task.totalBytes), CreatedAt: task.createdAt, StartedAt: copyTime(task.startedAt), FinishedAt: copyTime(task.finishedAt), ExpiresAt: task.expiresAt, ErrorCode: task.errorCode}
	if task.status != DownloadStatusStreaming {
		return view
	}
	view.SpeedBytesPerSecond, view.ETASeconds = task.transferRate(now)
	return view
}

func (task *downloadTask) transferRate(now time.Time) (*uint64, *uint64) {
	if len(task.progress) < 2 {
		return nil, nil
	}
	last := downloadProgressSample{at: now, bytesSent: task.bytesSent}
	latestStored := task.progress[len(task.progress)-1]
	if now.Before(latestStored.at) || now.Sub(latestStored.at) > downloadProgressWindow {
		return nil, nil
	}
	first := last
	for _, sample := range task.progress {
		if now.Sub(sample.at) <= downloadProgressWindow {
			first = sample
			break
		}
	}
	elapsed := last.at.Sub(first.at)
	if elapsed <= 0 || last.bytesSent <= first.bytesSent {
		return nil, nil
	}
	rate := uint64(float64(last.bytesSent-first.bytesSent) / elapsed.Seconds())
	if rate == 0 {
		return nil, nil
	}
	if task.totalBytes == nil {
		return &rate, nil
	}
	remaining := uint64(0)
	if *task.totalBytes > task.bytesSent {
		remaining = *task.totalBytes - task.bytesSent
	}
	eta := remaining / rate
	if remaining%rate != 0 {
		eta++
	}
	return &rate, &eta
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

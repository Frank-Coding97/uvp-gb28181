package devicecapture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

const maxImageBytes = 10 << 20

var (
	ErrNotFound     = errors.New("snapshot session not found")
	ErrInvalidImage = errors.New("invalid snapshot image")
)

type State string

const (
	StateCreating  State = "creating"
	StateWaiting   State = "waiting"
	StateReceiving State = "receiving"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

type File struct {
	Name       string    `json:"name"`
	Path       string    `json:"-"`
	Size       int64     `json:"size"`
	ReceivedAt time.Time `json:"receivedAt"`
}

type Session struct {
	ID            string    `json:"sessionId"`
	UploadToken   string    `json:"-"`
	OwnerID       string    `json:"-"`
	ChannelID     string    `json:"channelId"`
	ChannelCode   string    `json:"channelCode"`
	DeviceCode    string    `json:"deviceCode"`
	SnapNum       int       `json:"snapNum"`
	Interval      int       `json:"interval"`
	State         State     `json:"state"`
	Files         []File    `json:"files"`
	NotifiedCount int       `json:"notifiedCount"`
	Error         string    `json:"error"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	notified      map[string]struct{}
}

type CreateRequest struct {
	OwnerID, ChannelID, ChannelCode, DeviceCode string
	SnapNum, Interval                           int
}

type Registry struct {
	mu       sync.RWMutex
	root     string
	sessions map[string]*Session
	byToken  map[string]string
}

func NewRegistry(root string) *Registry {
	return &Registry{root: root, sessions: make(map[string]*Session), byToken: make(map[string]string)}
}

func (r *Registry) Create(request CreateRequest) Session {
	now := time.Now()
	session := &Session{
		ID: uuid.NewString(), UploadToken: uuid.NewString(), OwnerID: request.OwnerID,
		ChannelID: request.ChannelID, ChannelCode: request.ChannelCode, DeviceCode: request.DeviceCode,
		SnapNum: request.SnapNum, Interval: request.Interval, State: StateCreating,
		CreatedAt: now, UpdatedAt: now, notified: make(map[string]struct{}),
	}
	r.mu.Lock()
	r.sessions[session.ID] = session
	r.byToken[session.UploadToken] = session.ID
	r.pruneLocked()
	r.mu.Unlock()
	return cloneSession(session)
}

func (r *Registry) MarkWaiting(id string) {
	r.updateState(id, StateWaiting, "")
}

func (r *Registry) MarkFailed(id string, err error) {
	message := "抓拍配置下发失败"
	if err != nil {
		message = err.Error()
	}
	r.updateState(id, StateFailed, message)
}

func (r *Registry) updateState(id string, state State, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if session := r.sessions[id]; session != nil {
		session.State, session.Error, session.UpdatedAt = state, message, time.Now()
	}
}

func (r *Registry) GetForOwner(id, ownerID string) (Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session := r.sessions[id]
	if session == nil || session.OwnerID != ownerID {
		return Session{}, false
	}
	copy := cloneSession(session)
	return copy, true
}

func (r *Registry) Upload(token, filename, contentType string, source io.Reader) (File, error) {
	r.mu.RLock()
	sessionID := r.byToken[strings.TrimSpace(token)]
	r.mu.RUnlock()
	if sessionID == "" {
		return File{}, ErrNotFound
	}
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" || !strings.HasSuffix(strings.ToLower(filename), ".jpg") && !strings.HasSuffix(strings.ToLower(filename), ".jpeg") {
		return File{}, ErrInvalidImage
	}
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/jpeg") && !strings.HasPrefix(strings.ToLower(contentType), "application/octet-stream") {
		return File{}, ErrInvalidImage
	}
	payload, err := io.ReadAll(io.LimitReader(source, maxImageBytes+1))
	if err != nil || len(payload) < 4 || len(payload) > maxImageBytes || !bytes.HasPrefix(payload, []byte{0xff, 0xd8}) || !bytes.HasSuffix(payload, []byte{0xff, 0xd9}) {
		return File{}, ErrInvalidImage
	}
	directory := filepath.Join(r.root, "gb-device-snapshots", sessionID)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return File{}, err
	}
	path := filepath.Join(directory, filename)
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		return File{}, err
	}
	file := File{Name: filename, Path: path, Size: int64(len(payload)), ReceivedAt: time.Now()}
	r.mu.Lock()
	session := r.sessions[sessionID]
	if session == nil {
		r.mu.Unlock()
		return File{}, ErrNotFound
	}
	replaced := false
	for index := range session.Files {
		if session.Files[index].Name == filename {
			session.Files[index] = file
			replaced = true
			break
		}
	}
	if !replaced {
		session.Files = append(session.Files, file)
	}
	session.State, session.UpdatedAt = StateReceiving, time.Now()
	r.completeLocked(session)
	r.mu.Unlock()
	return file, nil
}

func (r *Registry) OnSnapshotNotify(_ context.Context, senderDeviceID string, body []byte) error {
	notify, err := manscdp.ParseSnapshotNotify(body)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[notify.SessionID]
	if session == nil {
		return ErrNotFound
	}
	senderDeviceID = strings.TrimSpace(senderDeviceID)
	if senderDeviceID != "" && senderDeviceID != session.DeviceCode && senderDeviceID != session.ChannelCode {
		return fmt.Errorf("snapshot notify sender mismatch")
	}
	if notify.DeviceID != session.DeviceCode && notify.DeviceID != session.ChannelCode {
		return fmt.Errorf("snapshot notify device mismatch")
	}
	if _, exists := session.notified[notify.SnapshotID]; !exists {
		session.notified[notify.SnapshotID] = struct{}{}
		session.NotifiedCount++
	}
	session.UpdatedAt = time.Now()
	r.completeLocked(session)
	return nil
}

func (r *Registry) Content(token, filename string) (File, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sessionID := r.byToken[strings.TrimSpace(token)]
	session := r.sessions[sessionID]
	if session == nil {
		return File{}, ErrNotFound
	}
	filename = filepath.Base(filename)
	for _, file := range session.Files {
		if file.Name == filename {
			return file, nil
		}
	}
	return File{}, ErrNotFound
}

func (r *Registry) completeLocked(session *Session) {
	if len(session.Files) >= session.SnapNum && session.NotifiedCount >= session.SnapNum {
		session.State = StateCompleted
	}
}

func (r *Registry) pruneLocked() {
	if len(r.sessions) <= 256 {
		return
	}
	items := make([]*Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		items = append(items, session)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	for _, session := range items[:len(items)-256] {
		delete(r.sessions, session.ID)
		delete(r.byToken, session.UploadToken)
	}
}

func cloneSession(source *Session) Session {
	copy := *source
	copy.Files = append([]File(nil), source.Files...)
	copy.notified = nil
	return copy
}

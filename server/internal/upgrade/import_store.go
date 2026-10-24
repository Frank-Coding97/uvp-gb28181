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
	"strings"
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

// ImportPhase is the durable state of an uploaded offline package before it
// becomes a normal upgrade task. Import verification is deliberately separate
// from TaskPhase because the package digest is not trusted until verification
// has completed.
type ImportPhase string

const (
	ImportPhaseQueued    ImportPhase = "queued"
	ImportPhaseVerifying ImportPhase = "verifying"
	ImportPhaseSucceeded ImportPhase = "succeeded"
	ImportPhaseFailed    ImportPhase = "failed"
)

// ImportRecord is persisted by the helper and returned through its local
// protocol. InboxToken is an opaque file name, never a filesystem path.
type ImportRecord struct {
	ImportID   string          `json:"import_id"`
	InboxToken string          `json:"inbox_token,omitempty"`
	Phase      ImportPhase     `json:"phase"`
	Staged     *StagedArtifact `json:"staged,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

type ImportStore struct{ root string }

func OpenImportStore(root string) (*ImportStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("import store root is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("create import store: %w", err)
	}
	return &ImportStore{root: filepath.Clean(root)}, nil
}

func (s *ImportStore) Submit(token string) (ImportRecord, error) {
	if s == nil || s.root == "" {
		return ImportRecord{}, errors.New("import store is not initialized")
	}
	if !validInboxToken(token) {
		return ImportRecord{}, errors.New("upgrade inbox token is invalid")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "imports.lock"))
	if err != nil {
		return ImportRecord{}, err
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return ImportRecord{}, err
	}
	for _, record := range records {
		if record.InboxToken == token {
			return record, nil
		}
	}
	id, err := newImportID()
	if err != nil {
		return ImportRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := ImportRecord{ImportID: id, InboxToken: token, Phase: ImportPhaseQueued, CreatedAt: now, UpdatedAt: now}
	if err := s.save(append(records, record)); err != nil {
		return ImportRecord{}, err
	}
	return record, nil
}

func (s *ImportStore) Get(importID string) (ImportRecord, error) {
	if !validImportID(importID) {
		return ImportRecord{}, errors.New("import ID is invalid")
	}
	records, err := s.List()
	if err != nil {
		return ImportRecord{}, err
	}
	for _, record := range records {
		if record.ImportID == importID || record.InboxToken == importID {
			return record, nil
		}
	}
	return ImportRecord{}, fmt.Errorf("import %q not found", importID)
}

func (s *ImportStore) List() ([]ImportRecord, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("import store is not initialized")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "imports.lock"))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	return s.load()
}

func (s *ImportStore) Update(importID string, update func(*ImportRecord) error) (ImportRecord, error) {
	if s == nil || s.root == "" || update == nil {
		return ImportRecord{}, errors.New("import store and update are required")
	}
	if !validImportID(importID) {
		return ImportRecord{}, errors.New("import ID is invalid")
	}
	lock, err := acquireUpgradeLockWait(filepath.Join(s.root, "imports.lock"))
	if err != nil {
		return ImportRecord{}, err
	}
	defer lock.Close()
	records, err := s.load()
	if err != nil {
		return ImportRecord{}, err
	}
	for i := range records {
		if records[i].ImportID != importID {
			continue
		}
		if err := update(&records[i]); err != nil {
			return ImportRecord{}, err
		}
		records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.save(records); err != nil {
			return ImportRecord{}, err
		}
		return records[i], nil
	}
	return ImportRecord{}, fmt.Errorf("import %q not found", importID)
}

func (s *ImportStore) MarkPhase(importID string, phase ImportPhase) (ImportRecord, error) {
	if !validImportPhase(phase) {
		return ImportRecord{}, errors.New("import phase is invalid")
	}
	return s.Update(importID, func(record *ImportRecord) error {
		if !allowedImportTransition(record.Phase, phase) {
			return fmt.Errorf("invalid import transition %s -> %s", record.Phase, phase)
		}
		record.Phase = phase
		return nil
	})
}

func (s *ImportStore) load() ([]ImportRecord, error) {
	path := filepath.Join(s.root, "imports.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read import index: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var records []ImportRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("decode import index: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("import index contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode import index trailing data: %w", err)
	}
	for i := range records {
		if !validImportID(records[i].ImportID) || !validInboxToken(records[i].InboxToken) || !validImportPhase(records[i].Phase) {
			return nil, errors.New("import index contains an invalid record")
		}
		records[i].Error = logging.SanitizeErrorDetail(records[i].Error)
	}
	return records, nil
}

func (s *ImportStore) save(records []ImportRecord) error {
	persisted := append([]ImportRecord(nil), records...)
	for i := range persisted {
		persisted[i].Error = logging.SanitizeErrorDetail(persisted[i].Error)
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("marshal import index: %w", err)
	}
	path := filepath.Join(s.root, "imports.json")
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create import index: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write import index: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync import index: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close import index: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace import index: %w", err)
	}
	directory, err := os.Open(s.root)
	if err != nil {
		return fmt.Errorf("open import store directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync import store directory: %w", err)
	}
	return nil
}

func isTerminalImportPhase(phase ImportPhase) bool {
	return phase == ImportPhaseSucceeded || phase == ImportPhaseFailed
}

func validImportPhase(phase ImportPhase) bool {
	switch phase {
	case ImportPhaseQueued, ImportPhaseVerifying, ImportPhaseSucceeded, ImportPhaseFailed:
		return true
	default:
		return false
	}
}

func allowedImportTransition(current, next ImportPhase) bool {
	if isTerminalImportPhase(current) {
		return false
	}
	if next == ImportPhaseFailed {
		return true
	}
	if current == ImportPhaseQueued {
		return next == ImportPhaseVerifying
	}
	return current == ImportPhaseVerifying && next == ImportPhaseSucceeded
}

func validImportID(value string) bool { return validTaskID(value) }

func newImportID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate import ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

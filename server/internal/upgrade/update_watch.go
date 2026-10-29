package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultUpdateCheckInterval is the production cadence of the background
// version check. The check only asks the release source for the newest signed
// manifest: it never downloads, stages, creates a task, or applies anything.
// Automatic upgrades were removed, so this must stay a read-only probe.
const DefaultUpdateCheckInterval = 6 * time.Hour

// DefaultUpdateCheckDelay keeps the first probe away from process startup,
// where the preflight subprocess would compete with the business runtime.
const DefaultUpdateCheckDelay = time.Minute

// maxWatchedReleaseNotes bounds what is persisted for the sidebar popover.
// The full changelog stays in the signed manifest and is fetched on demand.
const maxWatchedReleaseNotes = 2000

// AutoCheckPreferenceFilename is the shared switch file. The business process
// owns it; the helper only reads it before each scheduled probe. Keeping the
// preference in the helper state directory means it survives a helper
// replacement and needs no protocol round trip.
const AutoCheckPreferenceFilename = "auto-check.json"

// AutoCheckPreference is the administrator's "check for updates on a
// schedule" choice. Scheduled checks ship enabled, so the zero value must
// never be mistaken for "disabled".
type AutoCheckPreference struct {
	Enabled bool `json:"enabled"`
}

// ReadAutoCheckPreference reports whether scheduled checks are allowed. A
// missing, empty, or unreadable file means enabled: a broken preference is a
// storage problem, not an instruction to stop checking.
func ReadAutoCheckPreference(path string) bool {
	if strings.TrimSpace(path) == "" {
		// No shared file configured: keep the default-on behaviour so an
		// installation that predates the switch keeps checking.
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return true
	}
	var preference AutoCheckPreference
	if err := json.Unmarshal(data, &preference); err != nil {
		return true
	}
	return preference.Enabled
}

// WriteAutoCheckPreference persists the switch atomically. Only the business
// process calls this; the helper never writes it, so a probe can never
// overwrite an administrator decision.
func WriteAutoCheckPreference(path string, enabled bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("auto check preference path is required")
	}
	data, err := json.Marshal(AutoCheckPreference{Enabled: enabled})
	if err != nil {
		return fmt.Errorf("marshal auto check preference: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auto check directory: %w", err)
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create auto check preference: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write auto check preference: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync auto check preference: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close auto check preference: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace auto check preference: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

// UpdateWatchState is the durable outcome of the last version check. The
// helper writes it so the business process can answer "is there a newer
// release?" without contacting the release source on every page load.
type UpdateWatchState struct {
	CheckedAt      time.Time `json:"checked_at"`
	CurrentVersion string    `json:"current_version,omitempty"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	LatestBuildID  string    `json:"latest_build_id,omitempty"`
	PublishedAt    string    `json:"published_at,omitempty"`
	ReleaseNotes   string    `json:"release_notes,omitempty"`
	HasUpdate      bool      `json:"has_update"`
	// Error keeps only the last failure. A failed check must never clear a
	// previously discovered update, otherwise an offline machine would lose a
	// badge it had already earned.
	Error string `json:"error,omitempty"`
}

// UpdateWatchStore persists UpdateWatchState under the helper state root.
type UpdateWatchStore struct {
	path  string
	mu    sync.Mutex
	state UpdateWatchState
}

func OpenUpdateWatchStore(path string) (*UpdateWatchStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("update watch state path is required")
	}
	store := &UpdateWatchStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read update watch state: %w", err)
		}
		return store, nil
	}
	if len(data) == 0 {
		return store, nil
	}
	// A corrupt or unreadable cache must not prevent the helper from starting;
	// the next check rewrites it from scratch.
	var state UpdateWatchState
	if err := json.Unmarshal(data, &state); err != nil {
		return store, nil
	}
	store.state = state
	return store, nil
}

func (s *UpdateWatchStore) Snapshot() UpdateWatchState {
	if s == nil {
		return UpdateWatchState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *UpdateWatchStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Record stores the outcome of one check. A failure only refreshes the error
// and timestamp; it keeps whatever release was previously discovered.
func (s *UpdateWatchStore) Record(result *ControlCheckResult, checkErr error, now time.Time) error {
	if s == nil {
		return errors.New("update watch store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state
	next.CheckedAt = now.UTC()
	if checkErr != nil {
		next.Error = truncateWatchText(checkErr.Error(), 512)
		return s.persist(next)
	}
	next.Error = ""
	if result == nil {
		return s.persist(next)
	}
	next.CurrentVersion = result.CurrentVersion
	next.LatestVersion = strings.TrimSpace(result.Manifest.Version)
	next.LatestBuildID = strings.TrimSpace(result.Manifest.BuildID)
	next.PublishedAt = strings.TrimSpace(result.Manifest.PublishedAt)
	next.ReleaseNotes = truncateWatchText(result.Manifest.ReleaseNotes, maxWatchedReleaseNotes)
	next.HasUpdate = result.HasUpdate && next.LatestVersion != ""
	return s.persist(next)
}

func (s *UpdateWatchStore) persist(next UpdateWatchState) error {
	data, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("marshal update watch state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create update watch directory: %w", err)
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create update watch state: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write update watch state: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync update watch state: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close update watch state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace update watch state: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("sync update watch directory: %w", err)
	}
	s.state = next
	return nil
}

// UpdateWatcher runs the background version probe. It reuses the same
// ControlService.Check path as a manual "check for updates", so the release
// signature, channel, and deployment target rules stay identical.
type UpdateWatcher struct {
	Service *ControlService
	Store   *UpdateWatchStore
	// PreferencePath points at the shared schedule switch file. Empty means
	// "always enabled", which is the shipped default.
	PreferencePath string
	// Interval defaults to DefaultUpdateCheckInterval.
	Interval time.Duration
	// SkipInitialDelay runs the first probe immediately instead of waiting
	// DefaultUpdateCheckDelay. Tests use it; production keeps the delay so the
	// probe does not compete with the business runtime at startup.
	SkipInitialDelay bool
	Now              func() time.Time
}

func (w *UpdateWatcher) now() time.Time {
	if w != nil && w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Run blocks until ctx is cancelled. Errors are recorded in the store instead
// of stopping the loop: an offline deployment must keep retrying quietly.
func (w *UpdateWatcher) Run(ctx context.Context) error {
	if w == nil || w.Service == nil || w.Store == nil {
		return errors.New("update watcher is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultUpdateCheckInterval
	}
	delay := DefaultUpdateCheckDelay
	if w.SkipInitialDelay {
		delay = 0
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	w.CheckOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.CheckOnce(ctx)
		}
	}
}

// CheckOnce probes once. It never returns an error: the outcome, including a
// failure reason, belongs in the persisted state.
func (w *UpdateWatcher) CheckOnce(ctx context.Context) {
	if w == nil || w.Service == nil || w.Store == nil {
		return
	}
	if !w.Service.OnlineCheckEnabled() {
		// Offline installations have no release source; recording a permanent
		// error here would surface a warning nobody can act on.
		return
	}
	if !ReadAutoCheckPreference(w.PreferencePath) {
		// The administrator turned the schedule off. A manual check still goes
		// through ControlService.Check, so this only silences the timer.
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := w.Service.Check(ctx)
	if err != nil {
		_ = w.Store.Record(nil, err, w.now())
		return
	}
	_ = w.Store.Record(&result, nil, w.now())
}

func truncateWatchText(value string, maximum int) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= maximum {
		return trimmed
	}
	// Cut on a rune boundary so the persisted JSON stays valid UTF-8.
	runes := []rune(trimmed)
	if len(runes) <= maximum {
		return trimmed
	}
	return string(runes[:maximum])
}

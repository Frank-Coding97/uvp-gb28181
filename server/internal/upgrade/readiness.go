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

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

type GateMode string

const (
	GateNormal    GateMode = "normal"
	GateCandidate GateMode = "candidate"
	GateBlocked   GateMode = "blocked"
	GateRecovery  GateMode = "recovery"
)

type GateState struct {
	Mode      GateMode `json:"mode"`
	TaskID    string   `json:"task_id,omitempty"`
	BuildID   string   `json:"build_id,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

const UpdaterGatePathEnv = "UVP_UPDATER_GATE_PATH"

type GateStore struct {
	path          string
	fileMode      os.FileMode
	requireParent bool
}

// GateStateFromEnv reads the updater gate used by the application process.
// UVP_UPDATER_GATE_PATH is the managed-install boundary: it selects a
// root-written, business-read-only gate and must already exist. The legacy
// state-directory fallback is retained only for development installations.
func GateStateFromEnv() (GateState, error) {
	if path, configured, err := gatePathFromEnv(); err != nil {
		return GateState{}, err
	} else if configured {
		store, err := OpenSharedGateStore(path)
		if err != nil {
			return GateState{}, err
		}
		return store.load(true, true)
	}
	root := strings.TrimSpace(os.Getenv("UVP_UPDATER_STATE"))
	if root == "" {
		return GateState{Mode: GateNormal}, nil
	}
	store, err := OpenGateStore(filepath.Join(root, "gate.json"))
	if err != nil {
		return GateState{}, err
	}
	return store.Load()
}

func gatePathFromEnv() (string, bool, error) {
	value, configured := os.LookupEnv(UpdaterGatePathEnv)
	if !configured {
		return "", false, nil
	}
	path := strings.TrimSpace(value)
	if path == "" {
		return "", true, errors.New("configured updater gate path must not be empty")
	}
	if !filepath.IsAbs(path) {
		return "", true, errors.New("configured updater gate path must be absolute")
	}
	return filepath.Clean(path), true, nil
}

// CandidateModeFromEnv reports whether the process is running for a staged
// release validation. Callers should fail closed when the read itself errors.
func CandidateModeFromEnv() (bool, error) {
	state, err := GateStateFromEnv()
	if err != nil {
		return false, err
	}
	return state.Mode == GateCandidate, nil
}

// WaitForNormalGate keeps candidate-only startup effects paused until the
// updater has persisted a normal gate after its readiness decision. Gate read
// failures and every non-normal state fail closed by continuing to wait.
func WaitForNormalGate(ctx context.Context, interval time.Duration, start func()) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		state, err := GateStateFromEnv()
		if err == nil && state.Mode == GateNormal {
			if start != nil {
				start()
			}
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// NewBusinessRuntimeStarter groups the business effects that must remain
// dormant while a candidate release is under updater control. Its returned
// function is safe to call again after a delayed gate transition.
func NewBusinessRuntimeStarter(
	ctx context.Context,
	initializeJobs func() error,
	startUpgrade func(context.Context),
	startJobs func(),
	runMaintenance func(context.Context),
	startGB func(),
) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if initializeJobs != nil {
				if err := initializeJobs(); err != nil {
					panic(fmt.Errorf("initialize business jobs: %w", err))
				}
			}
			if startUpgrade != nil {
				startUpgrade(ctx)
			}
			if startJobs != nil {
				startJobs()
			}
			if runMaintenance != nil {
				go runMaintenance(ctx)
			}
			if startGB != nil {
				startGB()
			}
		})
	}
}

// ValidateStartupGate is the process startup fail-closed boundary. A previous
// failed activation or rollback must keep the business process offline until
// an operator repairs or explicitly rolls back the deployment.
func ValidateStartupGate(state GateState) error {
	switch state.Mode {
	case "", GateNormal, GateCandidate:
		return nil
	case GateBlocked, GateRecovery:
		if state.Reason == "" {
			return fmt.Errorf("upgrade gate is %s", state.Mode)
		}
		return fmt.Errorf("upgrade gate is %s: %s", state.Mode, state.Reason)
	default:
		return fmt.Errorf("upgrade gate mode %q is invalid", state.Mode)
	}
}

func OpenGateStore(path string) (*GateStore, error) {
	return openGateStore(path, 0600, false)
}

// OpenSharedGateStore opens a root-written gate that the business account may
// read but never write. Its parent is deliberately not created here: deployment
// must provision its owner and group before the updater starts.
func OpenSharedGateStore(path string) (*GateStore, error) {
	store, err := openGateStore(path, 0640, true)
	if err != nil {
		return nil, err
	}
	if _, err := store.sharedParent(); err != nil {
		return nil, err
	}
	return store, nil
}

func openGateStore(path string, fileMode os.FileMode, requireParent bool) (*GateStore, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("gate path must be absolute")
	}
	return &GateStore{path: filepath.Clean(path), fileMode: fileMode, requireParent: requireParent}, nil
}

func (s *GateStore) Load() (GateState, error) {
	return s.load(false, false)
}

func (s *GateStore) sharedParent() (os.FileInfo, error) {
	if s == nil || s.path == "" {
		return nil, errors.New("gate store is not initialized")
	}
	parent := filepath.Dir(s.path)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("shared gate parent is not provisioned: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("stat shared gate parent: %w", err)
	}
	if err := verifySharedGateParent(info); err != nil {
		return nil, err
	}
	return info, nil
}

func (s *GateStore) load(requireFile, verifySharedPermissions bool) (GateState, error) {
	if s == nil || s.path == "" {
		return GateState{}, errors.New("gate store is not initialized")
	}
	if verifySharedPermissions {
		info, err := os.Lstat(s.path)
		if errors.Is(err, os.ErrNotExist) {
			return GateState{}, fmt.Errorf("configured updater gate is missing: %s", s.path)
		}
		if err != nil {
			return GateState{}, err
		}
		if err := verifySharedGateFile(info); err != nil {
			return GateState{}, err
		}
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if requireFile {
			return GateState{}, fmt.Errorf("configured updater gate is missing: %s", s.path)
		}
		return GateState{Mode: GateNormal}, nil
	}
	if err != nil {
		return GateState{}, err
	}
	var state GateState
	if err := json.Unmarshal(data, &state); err != nil {
		return GateState{}, err
	}
	if state.Mode == "" {
		return GateState{}, errors.New("gate mode is missing")
	}
	state.Reason = logging.SanitizeErrorDetail(state.Reason)
	return state, nil
}

func (s *GateStore) Save(state GateState) error {
	if s == nil || s.path == "" {
		return errors.New("gate store is not initialized")
	}
	if state.Mode == "" {
		return errors.New("gate mode is required")
	}
	state.Reason = logging.SanitizeErrorDetail(state.Reason)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	parent := filepath.Dir(s.path)
	var parentInfo os.FileInfo
	if s.requireParent {
		parentInfo, err = s.sharedParent()
		if err != nil {
			return err
		}
	} else if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(parent, "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	fileMode := s.fileMode
	if fileMode == 0 {
		fileMode = 0600
	}
	if s.requireParent {
		if err := inheritSharedGateGroup(tmp, parentInfo); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	if err := syncGateDirectory(parent); err != nil {
		return fmt.Errorf("sync gate directory: %w", err)
	}
	return nil
}

func syncGateDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

type CandidateCheck func(context.Context, TaskRecord) error

type CandidateValidator struct {
	Checks   []CandidateCheck
	Attempts int
	Interval time.Duration
}

func (v CandidateValidator) Validate(ctx context.Context, record TaskRecord) error {
	if len(v.Checks) == 0 {
		return errors.New("candidate checks are not configured")
	}
	attempts := v.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		for index, check := range v.Checks {
			if check == nil {
				return fmt.Errorf("candidate check %d is nil", index)
			}
			if err := check(ctx, record); err != nil {
				return fmt.Errorf("candidate check %d failed: %w", index, err)
			}
		}
		if attempt+1 < attempts && v.Interval > 0 {
			timer := time.NewTimer(v.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

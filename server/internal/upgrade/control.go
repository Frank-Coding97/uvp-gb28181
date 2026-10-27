package upgrade

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var errControlStagedArtifactNotFound = errors.New("verified staged artifact was not found")

// ControlService owns all helper-side, non-disruptive upgrade operations. It
// receives only typed identifiers from the local RPC layer and never accepts
// a command, deployment path, or release key from the business process.
type ControlService struct {
	Store       *TaskStore
	ImportStore *ImportStore
	PolicyStore *PolicyStore
	EventLog    *FileEventLog

	// InboxRoot is a narrow handoff directory for a multipart upload. It is
	// distinct from StagingRoot: Import reopens the token with no symlink
	// following checks, validates the signed archive, then writes only to the
	// helper-owned staging root.
	InboxRoot    string
	StagingRoot  string
	SequencePath string

	Target        Target
	TrustedKeys   map[string]ed25519.PublicKey
	ReleaseSource string
	HTTPClient    *http.Client

	// CheckRelease is a test seam and an optional registration adapter. A
	// production helper normally uses ReleaseSource and TrustedKeys below.
	CheckRelease func(context.Context) (Manifest, error)
	Preflight    func(context.Context) (PreflightSnapshot, error)
	Now          func() time.Time

	prepareMu  sync.Mutex
	sequenceMu sync.Mutex

	workerMu       sync.Mutex
	prepareCancel  context.CancelFunc
	prepareWake    chan struct{}
	preparePending map[string]struct{}
	prepareDone    chan struct{}
	importPending  map[string]struct{}
}

// ControlCreateTaskRequest is the fixed business payload used to create a
// normal, administrator-confirmed upgrade task.
type ControlCreateTaskRequest struct {
	IdempotencyKey string
	Actor          string
	TargetDigest   string
}

type ControlCheckResult struct {
	Manifest       Manifest `json:"manifest"`
	Artifact       Artifact `json:"artifact"`
	CurrentVersion string   `json:"current_version"`
	HasUpdate      bool     `json:"has_update"`
}

type ControlStatus struct {
	CurrentVersion          string         `json:"current_version,omitempty"`
	CurrentBuildID          string         `json:"current_build_id,omitempty"`
	Deployment              string         `json:"deployment"`
	ReleaseSourceConfigured bool           `json:"release_source_configured"`
	OnlineCheck             bool           `json:"online_check"`
	OfflineImport           bool           `json:"offline_import"`
	ActiveTask              *TaskRecord    `json:"active_task,omitempty"`
	Tasks                   []TaskRecord   `json:"tasks"`
	Events                  []UpgradeEvent `json:"events"`
}

// Start starts the helper-owned preparation worker. Preparation never crosses
// the service stop boundary, but it may download and verify a large release,
// so it must outlive individual local RPC requests.
func (s *ControlService) Start(ctx context.Context) error {
	return s.Recover(ctx)
}

// Recover requeues durable preparation tasks after a helper restart. The
// workflow executor owns recovery after the stop boundary; this method only
// resumes fetching, verifying, and staging work that is safe while the
// business process remains online.
func (s *ControlService) Recover(ctx context.Context) error {
	if err := controlContext(ctx); err != nil {
		return err
	}
	if s == nil || s.Store == nil {
		return errors.New("upgrade task store is not configured")
	}
	if err := s.startPreparationWorker(ctx); err != nil {
		return err
	}
	if s.ImportStore != nil {
		imports, err := s.ImportStore.List()
		if err != nil {
			return err
		}
		for _, record := range imports {
			if !isTerminalImportPhase(record.Phase) {
				s.enqueueImport(record.ImportID)
			}
		}
	}
	records, err := s.Store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if isControlPreparationPhase(record.Phase) {
			s.enqueuePreparation(record.TaskID)
		}
	}
	return nil
}

// Close stops the in-process preparation worker and waits until it has
// observed cancellation. A cancellation is deliberately not persisted as a
// task failure: the next helper instance revalidates the durable task.
func (s *ControlService) Close() {
	if s == nil {
		return
	}
	s.workerMu.Lock()
	cancel, done := s.prepareCancel, s.prepareDone
	s.workerMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *ControlService) ListTasks(ctx context.Context) ([]TaskRecord, error) {
	if err := controlContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.Store == nil {
		return nil, errors.New("upgrade task store is not configured")
	}
	return s.Store.List()
}

func (s *ControlService) RecentEvents(ctx context.Context, limit int) ([]UpgradeEvent, error) {
	if err := controlContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.EventLog == nil {
		return nil, errors.New("upgrade event log is not configured")
	}
	if limit <= 0 {
		limit = 50
	}
	return s.EventLog.Recent(limit)
}

// Events is the business-facing name used by controller clients. The daemon
// adapter satisfies its narrower UpdaterControl interface through RecentEvents.
func (s *ControlService) Events(ctx context.Context, limit int) ([]UpgradeEvent, error) {
	return s.RecentEvents(ctx, limit)
}

func (s *ControlService) LoadPolicy(ctx context.Context) (AutoUpgradePolicy, error) {
	if err := controlContext(ctx); err != nil {
		return AutoUpgradePolicy{}, err
	}
	if s == nil || s.PolicyStore == nil {
		return AutoUpgradePolicy{}, errors.New("automatic upgrade policy store is not configured")
	}
	return s.PolicyStore.Load()
}

// SavePolicy allocates the version inside the helper and invalidates stale
// policy tasks before a maintenance window can use their old approval.
func (s *ControlService) SavePolicy(ctx context.Context, policy AutoUpgradePolicy, actor string) (AutoUpgradePolicy, error) {
	if err := controlContext(ctx); err != nil {
		return AutoUpgradePolicy{}, err
	}
	if s == nil || s.PolicyStore == nil || s.Store == nil {
		return AutoUpgradePolicy{}, errors.New("automatic upgrade control is not configured")
	}
	if !validProtocolText(actor, 128) {
		return AutoUpgradePolicy{}, errors.New("policy actor is invalid")
	}
	current, err := s.PolicyStore.Load()
	if err != nil {
		return AutoUpgradePolicy{}, err
	}
	target := s.target()
	// Deployment, OS, and architecture are properties of the registered
	// helper instance. Never let a socket caller submit a policy that can be
	// replayed against another target after registration changes.
	policy.Deployment = target.Deployment
	policy.OS = target.OS
	policy.Arch = target.Arch
	if policy.RetainVersions == 0 {
		policy.RetainVersions = current.RetainVersions
		if policy.RetainVersions == 0 {
			policy.RetainVersions = 2
		}
	}
	policy.CreatedBy = actor
	updated, err := s.PolicyStore.SaveNext(policy)
	if err != nil {
		return AutoUpgradePolicy{}, err
	}
	if err := s.cancelStalePolicyTasks(ctx, updated.PolicyVersion); err != nil {
		return AutoUpgradePolicy{}, err
	}
	return updated, nil
}

func (s *ControlService) UpdatePolicy(ctx context.Context, policy AutoUpgradePolicy, actor string) (AutoUpgradePolicy, error) {
	return s.SavePolicy(ctx, policy, actor)
}

func (s *ControlService) Status(ctx context.Context) (ControlStatus, error) {
	records, err := s.ListTasks(ctx)
	if err != nil {
		return ControlStatus{}, err
	}
	events, err := s.RecentEvents(ctx, 50)
	if err != nil {
		return ControlStatus{}, err
	}
	status := ControlStatus{
		Deployment:              s.target().Deployment,
		ReleaseSourceConfigured: s != nil && strings.HasPrefix(strings.TrimSpace(s.ReleaseSource), "https://"),
		OnlineCheck:             s != nil && strings.HasPrefix(strings.TrimSpace(s.ReleaseSource), "https://") && len(s.TrustedKeys) > 0,
		OfflineImport:           s != nil && len(s.TrustedKeys) > 0,
		Tasks:                   records,
		Events:                  events,
		ActiveTask:              activeTaskRecord(records),
	}
	if s != nil && s.Preflight != nil {
		if snapshot, preflightErr := s.Preflight(ctx); preflightErr == nil {
			status.CurrentVersion, status.CurrentBuildID = snapshot.CurrentVersion, snapshot.CurrentBuildID
		}
	}
	return status, nil
}

// Check verifies the registered channel and selects only the artifact matching
// the registered deployment target. It does not download or modify a task.
func (s *ControlService) Check(ctx context.Context) (ControlCheckResult, error) {
	if err := controlContext(ctx); err != nil {
		return ControlCheckResult{}, err
	}
	manifest, err := s.checkedManifest(ctx)
	if err != nil {
		return ControlCheckResult{}, err
	}
	artifact, err := manifest.ArtifactFor(s.target())
	if err != nil {
		return ControlCheckResult{}, err
	}
	preflight, err := s.collectPreflight(ctx)
	if err != nil {
		return ControlCheckResult{}, err
	}
	return ControlCheckResult{
		Manifest:       manifest,
		Artifact:       artifact,
		CurrentVersion: preflight.CurrentVersion,
		HasUpdate:      isNewerRelease(manifest, preflight.CurrentVersion),
	}, nil
}

// CheckManifest exposes the same registered, signed manifest check used by
// the control RPC. The helper's automatic scheduler uses this callback so its
// online path cannot drift from the controller-visible check semantics.
func (s *ControlService) CheckManifest(ctx context.Context) (Manifest, error) {
	if err := controlContext(ctx); err != nil {
		return Manifest{}, err
	}
	return s.checkedManifest(ctx)
}

// PrepareOnlineRelease downloads and verifies one artifact selected from a
// previously checked signed manifest. It is deliberately helper-owned so the
// unattended scheduler can stage a release without the business process or a
// long-lived HTTP request.
func (s *ControlService) PrepareOnlineRelease(ctx context.Context, manifest Manifest, artifact Artifact) (StagedArtifact, error) {
	if err := controlContext(ctx); err != nil {
		return StagedArtifact{}, err
	}
	if s == nil {
		return StagedArtifact{}, errors.New("upgrade control is not configured")
	}
	target := s.target()
	registeredArtifact, err := manifest.ArtifactFor(target)
	if err != nil {
		return StagedArtifact{}, err
	}
	if !sameArtifactDigest(registeredArtifact, artifact) {
		return StagedArtifact{}, errors.New("automatic artifact does not match the checked signed manifest")
	}
	preflight, err := s.collectPreflight(ctx)
	if err != nil {
		return StagedArtifact{}, err
	}
	if err := validateReleaseUpgrade(manifest, preflight.CurrentVersion); err != nil {
		return StagedArtifact{}, err
	}
	if staged, findErr := s.findStagedArtifact(artifact.SHA256); findErr == nil {
		return s.verifyStagedArtifact(ctx, staged, artifact.SHA256, s.StagingRoot, preflight)
	} else if !errors.Is(findErr, errControlStagedArtifactNotFound) {
		return StagedArtifact{}, findErr
	}
	if err := s.downloadArtifact(ctx, manifest, artifact); err != nil {
		return StagedArtifact{}, err
	}
	staged, err := s.findStagedArtifact(artifact.SHA256)
	if err != nil {
		return StagedArtifact{}, err
	}
	return s.verifyStagedArtifact(ctx, staged, artifact.SHA256, s.StagingRoot, preflight)
}

// Import consumes one opaque upload token from InboxRoot. The caller cannot
// choose the destination path; it can only reference a file name accepted by
// validInboxToken. The raw input is removed after the helper has opened it.
func (s *ControlService) Import(ctx context.Context, token string) (StagedArtifact, error) {
	return s.importArchive(ctx, token, false)
}

func (s *ControlService) importArchive(ctx context.Context, token string, preserveOnCancel bool) (StagedArtifact, error) {
	if err := controlContext(ctx); err != nil {
		return StagedArtifact{}, err
	}
	if s == nil || len(s.TrustedKeys) == 0 {
		return StagedArtifact{}, errors.New("trusted release public keys are not configured")
	}
	if !validInboxToken(token) {
		return StagedArtifact{}, errors.New("upgrade inbox token is invalid")
	}
	path, err := s.inboxPath(token)
	if err != nil {
		return StagedArtifact{}, err
	}
	removeInput := !preserveOnCancel
	defer func() {
		if removeInput {
			_ = os.Remove(path)
		}
	}()
	file, err := openControlRegularFile(path, maxArtifactDownloadSize)
	if err != nil {
		return StagedArtifact{}, err
	}
	defer file.Close()
	if err := os.MkdirAll(s.StagingRoot, 0700); err != nil {
		return StagedArtifact{}, fmt.Errorf("create helper staging root: %w", err)
	}
	staged, err := ImportArchive(ctx, file, s.StagingRoot, s.TrustedKeys, s.target())
	if err != nil {
		if preserveOnCancel && ctx.Err() != nil {
			removeInput = false
		}
		return StagedArtifact{}, err
	}
	if err := SaveStagedMetadata(staged.Path+".json", staged); err != nil {
		_ = os.Remove(staged.Path)
		return StagedArtifact{}, err
	}
	return staged, nil
}

// SubmitImport persists an offline import record and returns without reading or
// validating the archive. The helper worker owns verification so it survives
// the business API request and can resume after helper restarts.
func (s *ControlService) SubmitImport(ctx context.Context, token string) (ImportRecord, error) {
	if err := controlContext(ctx); err != nil {
		return ImportRecord{}, err
	}
	if s == nil || s.ImportStore == nil {
		return ImportRecord{}, errors.New("upgrade import store is not configured")
	}
	if _, err := s.inboxPath(token); err != nil {
		return ImportRecord{}, err
	}
	record, err := s.ImportStore.Submit(token)
	if err != nil {
		return ImportRecord{}, err
	}
	if err := s.startPreparationWorker(context.WithoutCancel(ctx)); err != nil {
		return ImportRecord{}, err
	}
	s.enqueueImport(record.ImportID)
	return record, nil
}

func (s *ControlService) GetImport(ctx context.Context, importID string) (ImportRecord, error) {
	if err := controlContext(ctx); err != nil {
		return ImportRecord{}, err
	}
	if s == nil || s.ImportStore == nil {
		return ImportRecord{}, errors.New("upgrade import store is not configured")
	}
	return s.ImportStore.Get(importID)
}

func (s *ControlService) prepareImport(ctx context.Context, importID string) {
	if s == nil || s.ImportStore == nil || importID == "" {
		return
	}
	record, err := s.ImportStore.Get(importID)
	if err != nil || isTerminalImportPhase(record.Phase) {
		return
	}
	if record.Phase == ImportPhaseQueued {
		record, err = s.ImportStore.MarkPhase(importID, ImportPhaseVerifying)
		if err != nil {
			return
		}
	}
	if record.Phase != ImportPhaseVerifying {
		return
	}
	staged, err := s.importArchive(ctx, record.InboxToken, true)
	if err != nil {
		if controlWorkerCanceled(ctx, err) {
			return
		}
		failed, updateErr := s.ImportStore.Update(importID, func(current *ImportRecord) error {
			current.Phase = ImportPhaseFailed
			current.Error = err.Error()
			current.Staged = nil
			return nil
		})
		if updateErr == nil {
			s.removeInboxToken(record.InboxToken)
		}
		if updateErr == nil && s.EventLog != nil {
			_ = s.EventLog.Append(context.WithoutCancel(ctx), UpgradeEvent{Event: "import_failed", TaskID: failed.ImportID, Reason: err.Error(), At: s.now()})
		}
		return
	}
	completed, err := s.ImportStore.Update(importID, func(current *ImportRecord) error {
		current.Phase = ImportPhaseSucceeded
		current.Error = ""
		current.Staged = &staged
		return nil
	})
	if err == nil {
		s.removeInboxToken(record.InboxToken)
	}
	if err == nil && s.EventLog != nil {
		_ = s.EventLog.Append(context.WithoutCancel(ctx), UpgradeEvent{Event: "import_succeeded", TaskID: completed.ImportID, ReleaseID: staged.ReleaseID, BuildID: staged.BuildID, At: s.now()})
	}
}

func (s *ControlService) removeInboxToken(token string) {
	path, err := s.inboxPath(token)
	if err != nil {
		return
	}
	if err := os.Remove(path); err == nil {
		_ = syncDirectory(s.InboxRoot)
	}
}

// CreateTask authorizes a trusted artifact before it creates a durable task.
// Artifact download and preparation continue in the helper after the bounded
// RPC response has returned.
func (s *ControlService) CreateTask(ctx context.Context, request ControlCreateTaskRequest) (TaskRecord, error) {
	if err := controlContext(ctx); err != nil {
		return TaskRecord{}, err
	}
	if s == nil || s.Store == nil {
		return TaskRecord{}, errors.New("upgrade task store is not configured")
	}
	if !validProtocolText(request.IdempotencyKey, 256) || !validProtocolText(request.Actor, 128) || !validDigest(request.TargetDigest) {
		return TaskRecord{}, errors.New("upgrade task request is invalid")
	}
	preflight, err := s.collectPreflight(ctx)
	if err != nil {
		return TaskRecord{}, err
	}
	if err := s.validateTarget(ctx, request.TargetDigest, preflight); err != nil {
		return TaskRecord{}, err
	}
	record, err := s.Store.Submit(TaskRequest{IdempotencyKey: request.IdempotencyKey, Actor: request.Actor, TargetDigest: strings.ToLower(request.TargetDigest)})
	if err != nil {
		return TaskRecord{}, err
	}
	if record.Phase == PhaseFetching {
		if err := s.startPreparationWorker(context.WithoutCancel(ctx)); err != nil {
			return TaskRecord{}, err
		}
		s.enqueuePreparation(record.TaskID)
	}
	return record, nil
}

// PrepareTask implements the existing local protocol action. StagingPath is
// accepted only inside the registered inbox, then independently validated and
// copied by the later workflow verifier before any service stop.
func (s *ControlService) PrepareTask(ctx context.Context, request PrepareTaskRequest) (TaskRecord, error) {
	if err := controlContext(ctx); err != nil {
		return TaskRecord{}, err
	}
	if err := request.validate(); err != nil {
		return TaskRecord{}, err
	}
	if s == nil || s.Store == nil {
		return TaskRecord{}, errors.New("upgrade task store is not configured")
	}
	preflight, err := s.collectPreflight(ctx)
	if err != nil {
		return TaskRecord{}, err
	}
	staged, err := s.loadStagedArtifact(request.StagingPath, s.InboxRoot)
	if err != nil {
		return TaskRecord{}, err
	}
	staged, err = s.verifyStagedArtifact(ctx, staged, request.TargetDigest, s.InboxRoot, preflight)
	if err != nil {
		return TaskRecord{}, err
	}
	record, err := s.Store.Submit(TaskRequest{IdempotencyKey: request.IdempotencyKey, Actor: request.Actor, TargetDigest: strings.ToLower(request.TargetDigest)})
	if err != nil {
		return TaskRecord{}, err
	}
	if record.Phase != PhaseFetching {
		return record, nil
	}
	return s.persistPreparedTask(ctx, record.TaskID, staged, request.Source, preflight)
}

func (s *ControlService) GetTask(ctx context.Context, taskID string) (TaskRecord, error) {
	if err := controlContext(ctx); err != nil {
		return TaskRecord{}, err
	}
	if s == nil || s.Store == nil {
		return TaskRecord{}, errors.New("upgrade task store is not configured")
	}
	return s.Store.Get(taskID)
}

func (s *ControlService) CancelTask(ctx context.Context, taskID string) (TaskRecord, error) {
	if err := controlContext(ctx); err != nil {
		return TaskRecord{}, err
	}
	if s == nil || s.Store == nil {
		return TaskRecord{}, errors.New("upgrade task store is not configured")
	}
	if err := s.Store.Cancel(taskID); err != nil {
		return TaskRecord{}, err
	}
	return s.Store.Get(taskID)
}

func (s *ControlService) prepareCreatedTask(ctx context.Context, taskID string) {
	if s == nil || s.Store == nil || taskID == "" {
		return
	}
	s.prepareMu.Lock()
	defer s.prepareMu.Unlock()
	record, err := s.Store.Get(taskID)
	if err != nil || !isControlPreparationPhase(record.Phase) {
		return
	}
	preflight, err := s.collectPreflight(ctx)
	if err != nil {
		if !controlWorkerCanceled(ctx, err) {
			s.failPendingTask(taskID, err)
		}
		return
	}
	staged, source, err := s.prepareArtifact(ctx, record.TargetDigest, preflight)
	if err != nil {
		if !controlWorkerCanceled(ctx, err) {
			s.failPendingTask(taskID, err)
		}
		return
	}
	if _, err := s.persistPreparedTask(ctx, taskID, staged, source, preflight); err != nil {
		if !controlWorkerCanceled(ctx, err) {
			s.failPendingTask(taskID, err)
		}
	}
}

func (s *ControlService) persistPreparedTask(ctx context.Context, taskID string, staged StagedArtifact, source string, preflight PreflightSnapshot) (TaskRecord, error) {
	announced := false
	for {
		if err := controlContext(ctx); err != nil {
			return TaskRecord{}, err
		}
		record, err := s.Store.Get(taskID)
		if err != nil {
			return TaskRecord{}, err
		}
		switch record.Phase {
		case PhaseFetching:
			if err := s.Store.MarkPhase(taskID, PhaseVerifying); err != nil {
				return TaskRecord{}, err
			}
		case PhaseVerifying:
			if err := s.persistPreparationMetadata(taskID, staged, source, preflight); err != nil {
				return TaskRecord{}, err
			}
			if err := s.Store.MarkPhase(taskID, PhasePreparing); err != nil {
				return TaskRecord{}, err
			}
		case PhasePreparing:
			// A prior helper can have crashed after the verifying transition but
			// before metadata or the next phase was durably written. Rewriting
			// the same trusted values is safe and completes that interrupted step.
			if err := s.persistPreparationMetadata(taskID, staged, source, preflight); err != nil {
				return TaskRecord{}, err
			}
			if err := s.Store.MarkPhase(taskID, PhaseAwaitingConfirmation); err != nil {
				return TaskRecord{}, err
			}
			announced = true
		case PhaseAwaitingConfirmation:
			if announced && s.EventLog != nil {
				if err := s.EventLog.Append(ctx, UpgradeEvent{Event: "downloaded", TaskID: record.TaskID, ReleaseID: staged.ReleaseID, BuildID: record.BuildID, At: s.now()}); err != nil {
					return TaskRecord{}, err
				}
			}
			return record, nil
		default:
			return TaskRecord{}, fmt.Errorf("task %q is no longer eligible for preparation from phase %s", taskID, record.Phase)
		}
	}
}

func (s *ControlService) persistPreparationMetadata(taskID string, staged StagedArtifact, source string, preflight PreflightSnapshot) error {
	return s.Store.UpdateMetadata(taskID, func(record *TaskRecord) error {
		record.Source = source
		record.StagingPath = staged.Path
		record.Version = staged.Version
		record.BuildID = staged.BuildID
		record.ReleaseNotes, record.PublishedAt = staged.ReleaseNotes, staged.PublishedAt
		record.Images = append([]ImageArtifact(nil), staged.Images...)
		record.Compatibility = CloneCompatibility(staged.Compatibility)
		snapshot := preflight
		record.Preflight = &snapshot
		return nil
	})
}

func (s *ControlService) startPreparationWorker(parent context.Context) error {
	if err := controlContext(parent); err != nil {
		return err
	}
	if s == nil || s.Store == nil {
		return errors.New("upgrade task store is not configured")
	}
	s.workerMu.Lock()
	if s.prepareCancel != nil {
		s.workerMu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	wake := make(chan struct{}, 1)
	done := make(chan struct{})
	s.prepareCancel = cancel
	s.prepareWake = wake
	s.preparePending = make(map[string]struct{})
	s.importPending = make(map[string]struct{})
	s.prepareDone = done
	s.workerMu.Unlock()

	go s.runPreparationWorker(ctx, done)
	return nil
}

func (s *ControlService) enqueuePreparation(taskID string) {
	if s == nil || taskID == "" {
		return
	}
	s.workerMu.Lock()
	wake := s.prepareWake
	if wake == nil || s.preparePending == nil {
		s.workerMu.Unlock()
		return
	}
	s.preparePending[taskID] = struct{}{}
	s.workerMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *ControlService) enqueueImport(importID string) {
	if s == nil || importID == "" {
		return
	}
	s.workerMu.Lock()
	wake := s.prepareWake
	if wake == nil || s.importPending == nil {
		s.workerMu.Unlock()
		return
	}
	s.importPending[importID] = struct{}{}
	s.workerMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *ControlService) runPreparationWorker(ctx context.Context, done chan struct{}) {
	defer func() {
		s.workerMu.Lock()
		if s.prepareDone == done {
			s.prepareCancel = nil
			s.prepareWake = nil
			s.preparePending = nil
			s.importPending = nil
			s.prepareDone = nil
		}
		close(done)
		s.workerMu.Unlock()
	}()
	for {
		if taskID, ok := s.nextPreparationTask(); ok {
			s.prepareCreatedTask(ctx, taskID)
			continue
		}
		if importID, ok := s.nextImport(); ok {
			s.prepareImport(ctx, importID)
			continue
		}
		s.workerMu.Lock()
		wake := s.prepareWake
		s.workerMu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

func (s *ControlService) nextImport() (string, bool) {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	for importID := range s.importPending {
		delete(s.importPending, importID)
		return importID, true
	}
	return "", false
}

func (s *ControlService) nextPreparationTask() (string, bool) {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	for taskID := range s.preparePending {
		delete(s.preparePending, taskID)
		return taskID, true
	}
	return "", false
}

func (s *ControlService) prepareArtifact(ctx context.Context, digest string, preflight PreflightSnapshot) (StagedArtifact, string, error) {
	staged, err := s.findStagedArtifact(digest)
	if err == nil {
		verified, verifyErr := s.verifyStagedArtifact(ctx, staged, digest, s.StagingRoot, preflight)
		return verified, "offline", verifyErr
	}
	if !errors.Is(err, errControlStagedArtifactNotFound) {
		return StagedArtifact{}, "", err
	}
	manifest, err := s.checkedManifest(ctx)
	if err != nil {
		return StagedArtifact{}, "", err
	}
	artifact, err := manifest.ArtifactFor(s.target())
	if err != nil {
		return StagedArtifact{}, "", err
	}
	if !strings.EqualFold(artifact.SHA256, digest) {
		return StagedArtifact{}, "", errors.New("task digest does not match the trusted release artifact")
	}
	if err := validateReleaseUpgrade(manifest, preflight.CurrentVersion); err != nil {
		return StagedArtifact{}, "", err
	}
	if err := s.downloadArtifact(ctx, manifest, artifact); err != nil {
		return StagedArtifact{}, "", err
	}
	staged, err = s.findStagedArtifact(digest)
	if err != nil {
		return StagedArtifact{}, "", err
	}
	verified, err := s.verifyStagedArtifact(ctx, staged, digest, s.StagingRoot, preflight)
	return verified, "online", err
}

func (s *ControlService) validateTarget(ctx context.Context, digest string, preflight PreflightSnapshot) error {
	if staged, err := s.findStagedArtifact(digest); err == nil {
		_, err = s.verifyStagedArtifact(ctx, staged, digest, s.StagingRoot, preflight)
		return err
	} else if !errors.Is(err, errControlStagedArtifactNotFound) {
		return err
	}
	manifest, err := s.checkedManifest(ctx)
	if err != nil {
		return fmt.Errorf("trusted release source check failed: %w", err)
	}
	artifact, err := manifest.ArtifactFor(s.target())
	if err != nil {
		return err
	}
	if !strings.EqualFold(artifact.SHA256, digest) {
		return errors.New("task digest does not match the trusted release artifact")
	}
	return validateReleaseUpgrade(manifest, preflight.CurrentVersion)
}

func (s *ControlService) findStagedArtifact(digest string) (StagedArtifact, error) {
	if !validDigest(digest) {
		return StagedArtifact{}, errors.New("invalid staged artifact digest")
	}
	root := filepath.Clean(s.StagingRoot)
	if root == "." || !filepath.IsAbs(root) {
		return StagedArtifact{}, errors.New("helper staging root is invalid")
	}
	paths, err := filepath.Glob(filepath.Join(root, strings.ToLower(digest)+"-*.json"))
	if err != nil {
		return StagedArtifact{}, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return StagedArtifact{}, fmt.Errorf("%w: %s", errControlStagedArtifactNotFound, digest)
	}
	if len(paths) != 1 {
		return StagedArtifact{}, errors.New("multiple staged artifacts have the same digest")
	}
	return s.loadStagedArtifact(strings.TrimSuffix(paths[0], ".json"), root)
}

func (s *ControlService) loadStagedArtifact(path, root string) (StagedArtifact, error) {
	if filepath.Clean(root) == "." || !filepath.IsAbs(root) || !filepath.IsAbs(path) || !isWithin(root, path) {
		return StagedArtifact{}, errors.New("staged artifact is outside registered helper roots")
	}
	metadataPath := path + ".json"
	metadata, err := openControlRegularFile(metadataPath, 2*maxManifestEnvelopeSize)
	if err != nil {
		return StagedArtifact{}, err
	}
	defer metadata.Close()
	data, err := io.ReadAll(io.LimitReader(metadata, 2*maxManifestEnvelopeSize+1))
	if err != nil {
		return StagedArtifact{}, err
	}
	if len(data) > 2*maxManifestEnvelopeSize {
		return StagedArtifact{}, errors.New("staged artifact metadata exceeds size limit")
	}
	var staged StagedArtifact
	if err := decodeStrictJSON(data, &staged); err != nil {
		return StagedArtifact{}, err
	}
	if filepath.Clean(staged.Path) != filepath.Clean(path) {
		return StagedArtifact{}, errors.New("staged artifact metadata path does not match its location")
	}
	return staged, nil
}

func (s *ControlService) verifyStagedArtifact(ctx context.Context, staged StagedArtifact, digest, root string, preflight PreflightSnapshot) (StagedArtifact, error) {
	if err := controlContext(ctx); err != nil {
		return StagedArtifact{}, err
	}
	if !strings.EqualFold(staged.SHA256, digest) {
		return StagedArtifact{}, errors.New("staged artifact digest does not match task digest")
	}
	target := s.target()
	if staged.Deployment != target.Deployment || staged.OS != target.OS || staged.Arch != target.Arch {
		return StagedArtifact{}, errors.New("staged artifact target does not match registered deployment")
	}
	if len(s.TrustedKeys) == 0 {
		return StagedArtifact{}, errors.New("trusted release public keys are not configured")
	}
	manifest, err := VerifySignedManifest(staged.ManifestEnvelope, s.TrustedKeys)
	if err != nil {
		return StagedArtifact{}, fmt.Errorf("verify staged artifact manifest: %w", err)
	}
	artifact, err := manifest.ArtifactFor(target)
	if err != nil {
		return StagedArtifact{}, err
	}
	if !strings.EqualFold(artifact.SHA256, digest) || !strings.EqualFold(staged.SHA256, artifact.SHA256) || staged.Size != artifact.Size {
		return StagedArtifact{}, errors.New("staged artifact differs from the signed release")
	}
	if staged.Deployment != artifact.Deployment || staged.OS != artifact.OS || staged.Arch != artifact.Arch {
		return StagedArtifact{}, errors.New("staged artifact target differs from the signed release")
	}
	if err := verifyControlStagedFile(staged, root); err != nil {
		return StagedArtifact{}, err
	}
	if err := validateReleaseUpgrade(manifest, preflight.CurrentVersion); err != nil {
		return StagedArtifact{}, err
	}
	verified := staged
	verified.ReleaseNotes, verified.PublishedAt = manifest.ReleaseNotes, manifest.PublishedAt
	verified.Filename = artifact.Filename
	verified.Size = artifact.Size
	verified.SHA256 = artifact.SHA256
	verified.ReleaseID = manifest.ReleaseID
	verified.Version = manifest.Version
	verified.BuildID = manifest.BuildID
	verified.Deployment = artifact.Deployment
	verified.OS = artifact.OS
	verified.Arch = artifact.Arch
	verified.Images = append([]ImageArtifact(nil), artifact.Images...)
	verified.Compatibility = CloneCompatibility(manifest.Compatibility)
	verified.ManifestEnvelope = append([]byte(nil), manifest.Envelope...)
	return verified, nil
}

func verifyControlStagedFile(staged StagedArtifact, root string) error {
	if staged.Path == "" || !filepath.IsAbs(staged.Path) || !isWithin(root, staged.Path) {
		return errors.New("staged artifact path is outside the helper root")
	}
	info, err := os.Lstat(staged.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("staged artifact is not a regular file")
	}
	file, err := os.Open(staged.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return errors.New("staged artifact changed while opening")
	}
	if staged.Size < 0 || staged.Size > maxArtifactDownloadSize {
		return errors.New("staged artifact size is invalid")
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil || written != staged.Size {
		return errors.New("staged artifact size does not match metadata")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), staged.SHA256) {
		return errors.New("staged artifact digest does not match metadata")
	}
	return nil
}

func (s *ControlService) checkedManifest(ctx context.Context) (Manifest, error) {
	if s == nil {
		return Manifest{}, errors.New("upgrade control is not configured")
	}
	if s.CheckRelease != nil {
		return s.CheckRelease(ctx)
	}
	source := strings.TrimSpace(s.ReleaseSource)
	if source == "" || len(s.TrustedKeys) == 0 {
		return Manifest{}, errors.New("trusted release source is not configured")
	}
	channelEnvelope, err := FetchSignedManifest(ctx, s.HTTPClient, source)
	if err != nil {
		return Manifest{}, err
	}
	manifest, channel, err := FetchChannelRelease(ctx, s.HTTPClient, source, channelEnvelope, s.TrustedKeys, s.now())
	if err != nil {
		return Manifest{}, err
	}
	if err := s.acceptSequence(channel.Channel, channel.Sequence, channelEnvelope); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (s *ControlService) acceptSequence(channel string, sequence uint64, envelope []byte) error {
	if s == nil || channel == "" || sequence == 0 || len(envelope) == 0 {
		return errors.New("trusted channel sequence is incomplete")
	}
	path := strings.TrimSpace(s.SequencePath)
	if path == "" && s.Store != nil {
		path = filepath.Join(s.Store.root, "sequences.json")
	}
	if !filepath.IsAbs(path) {
		return errors.New("trusted channel sequence path is invalid")
	}
	s.sequenceMu.Lock()
	defer s.sequenceMu.Unlock()
	state := map[string]controlSequenceState{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := decodeStrictJSON(data, &state); err != nil {
			return errors.New("trusted channel sequence state is corrupt")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	digest := sha256HexForControl(envelope)
	if previous, ok := state[channel]; ok {
		if sequence < previous.Sequence {
			return errors.New("channel sequence is older than the accepted sequence")
		}
		if sequence == previous.Sequence && digest != previous.Digest {
			return errors.New("channel sequence points to a different payload")
		}
		if sequence == previous.Sequence {
			return nil
		}
	}
	state[channel] = controlSequenceState{Sequence: sequence, Digest: digest}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeControlFile(path, encoded)
}

type controlSequenceState struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

func (s *ControlService) downloadArtifact(ctx context.Context, manifest Manifest, artifact Artifact) error {
	if len(manifest.Envelope) == 0 {
		return errors.New("verified manifest envelope is required before staging an online artifact")
	}
	if err := os.MkdirAll(s.StagingRoot, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(s.StagingRoot, ".download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := DownloadArtifactWithRedirects(ctx, s.HTTPClient, artifact, temporary, DefaultReleaseRedirectHosts); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	destination := filepath.Join(s.StagingRoot, strings.ToLower(artifact.SHA256)+"-"+filepath.Base(artifact.Filename))
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	if err := syncDirectory(s.StagingRoot); err != nil {
		return err
	}
	staged := StagedArtifact{
		Path: destination, Filename: artifact.Filename, Size: artifact.Size, SHA256: artifact.SHA256,
		ReleaseID: manifest.ReleaseID, Version: manifest.Version, BuildID: manifest.BuildID,
		ReleaseNotes: manifest.ReleaseNotes, PublishedAt: manifest.PublishedAt,
		Deployment: artifact.Deployment, OS: artifact.OS, Arch: artifact.Arch,
		Images: append([]ImageArtifact(nil), artifact.Images...), Compatibility: CloneCompatibility(manifest.Compatibility),
		ManifestEnvelope: append([]byte(nil), manifest.Envelope...),
	}
	if err := SaveStagedMetadata(destination+".json", staged); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func (s *ControlService) collectPreflight(ctx context.Context) (PreflightSnapshot, error) {
	if s == nil || s.Preflight == nil {
		return PreflightSnapshot{}, errors.New("independent upgrade preflight is not configured")
	}
	snapshot, err := s.Preflight(ctx)
	if err != nil {
		return PreflightSnapshot{}, err
	}
	if snapshot.CurrentVersion == "" || snapshot.CurrentBuildID == "" || !validDigest(snapshot.ConfigDigest) || !validDigest(snapshot.MigrationDigest) || NormalizeDatabaseDialect(snapshot.DatabaseDialect) == "" {
		return PreflightSnapshot{}, errors.New("independent upgrade preflight returned incomplete installation evidence")
	}
	return snapshot, nil
}

func (s *ControlService) cancelStalePolicyTasks(ctx context.Context, policyVersion uint64) error {
	records, err := s.Store.List()
	if err != nil {
		return err
	}
	var failures []error
	for _, record := range records {
		if record.Actor != "policy" || record.PolicyVersion == policyVersion || !isControlPendingPhase(record.Phase) {
			continue
		}
		if err := s.Store.Cancel(record.TaskID); err != nil {
			failures = append(failures, err)
			continue
		}
		const reason = "automatic upgrade policy changed before maintenance"
		if err := s.Store.UpdateMetadata(record.TaskID, func(task *TaskRecord) error {
			task.Error = reason
			return nil
		}); err != nil {
			failures = append(failures, err)
			continue
		}
		if s.EventLog != nil {
			if err := s.EventLog.Append(ctx, UpgradeEvent{Event: "blocked", TaskID: record.TaskID, PolicyVersion: policyVersion, Reason: reason, At: s.now()}); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (s *ControlService) failPendingTask(taskID string, cause error) {
	if s == nil || s.Store == nil || taskID == "" || cause == nil {
		return
	}
	if err := s.Store.Cancel(taskID); err != nil {
		return
	}
	_ = s.Store.UpdateMetadata(taskID, func(record *TaskRecord) error {
		record.Error = cause.Error()
		return nil
	})
}

func (s *ControlService) inboxPath(token string) (string, error) {
	root := filepath.Clean(s.InboxRoot)
	if !filepath.IsAbs(root) || root == "." {
		return "", errors.New("upgrade inbox root is invalid")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("upgrade inbox root is not a real directory")
	}
	path := filepath.Join(root, token)
	if !isWithin(root, path) {
		return "", errors.New("upgrade inbox token escapes its root")
	}
	return path, nil
}

func openControlRegularFile(path string, maximum int64) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("helper input is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, statErr := file.Stat()
	if statErr != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || (maximum >= 0 && after.Size() > maximum) {
		_ = file.Close()
		return nil, errors.New("helper input changed while opening")
	}
	return file, nil
}

func (s *ControlService) target() Target {
	if s == nil {
		return Target{}
	}
	target := s.Target
	if target.OS == "" {
		target.OS = runtime.GOOS
	}
	if target.Arch == "" {
		target.Arch = runtime.GOARCH
	}
	if target.UpdaterProtocol == 0 {
		target.UpdaterProtocol = UpdaterProtocolVersion
	}
	return target
}

func (s *ControlService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func sameArtifactDigest(left, right Artifact) bool {
	return strings.EqualFold(left.SHA256, right.SHA256) && left.Size == right.Size && left.Deployment == right.Deployment && left.OS == right.OS && left.Arch == right.Arch && left.Filename == right.Filename && left.URL == right.URL
}

func controlContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("upgrade control context is required")
	}
	return ctx.Err()
}

func validInboxToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func isControlPendingPhase(phase TaskPhase) bool {
	switch phase {
	case PhaseFetching, PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation:
		return true
	default:
		return false
	}
}

func isControlPreparationPhase(phase TaskPhase) bool {
	switch phase {
	case PhaseFetching, PhaseVerifying, PhasePreparing:
		return true
	default:
		return false
	}
}

func controlWorkerCanceled(ctx context.Context, err error) bool {
	return (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func isNewerRelease(manifest Manifest, current string) bool {
	return current == "" || (manifest.Version != current && IsVersionAtLeast(manifest.Version, current))
}

func validateReleaseUpgrade(manifest Manifest, current string) error {
	if current == "" {
		return errors.New("current release version is missing")
	}
	if !isNewerRelease(manifest, current) {
		return fmt.Errorf("signed release version %s is not newer than current version %s", manifest.Version, current)
	}
	if manifest.MinVersion != "" && !IsVersionAtLeast(current, manifest.MinVersion) {
		return fmt.Errorf("current version %s is below signed minimum version %s", current, manifest.MinVersion)
	}
	return nil
}

func sha256HexForControl(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeControlFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

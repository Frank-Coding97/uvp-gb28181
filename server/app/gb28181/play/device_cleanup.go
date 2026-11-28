package play

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const deviceCleanupTimeout = 5 * time.Second

// DeviceCleanupStatus describes the evidence returned by a process-local
// live cleanup attempt.  Settled only means that all matching in-memory
// generations reached a media-terminal stop; it never advances the durable
// device cleanup watermark.
type DeviceCleanupStatus string

const (
	DeviceCleanupSettled           DeviceCleanupStatus = "settled"
	DeviceCleanupPending           DeviceCleanupStatus = "pending"
	DeviceCleanupUnknown           DeviceCleanupStatus = "unknown"
	DeviceCleanupNoTrackedEvidence DeviceCleanupStatus = "no-tracked-evidence"
)

var ErrDeviceCleanupEpochInvalid = errors.New("device cleanup target epoch must be positive")

// DeviceCleanupReport is intentionally an evidence report, not a completion
// receipt.  Unknown/recovered entries and an empty process-local scan remain
// visible to the caller so a higher-level durable reconciler cannot mistake
// this coordinator for the source of truth.
type DeviceCleanupReport struct {
	DeviceID          string
	TargetEpoch       int64
	Status            DeviceCleanupStatus
	Settled           int
	Pending           int
	Unknown           int
	Newer             int
	NoTrackedEvidence bool
	Warnings          []string
}

type deviceCleanupKey struct {
	deviceID    string
	targetEpoch int64
}

type deviceCleanupJob struct {
	key    deviceCleanupKey
	done   chan struct{}
	report DeviceCleanupReport
}

type deviceCleanupCandidate struct {
	key   coordinatorKey
	entry *coordinatorEntry
}

type deviceCleanupCandidateResult struct {
	settled bool
	pending bool
	unknown bool
	newer   bool
	warning error
}

// ClearDeviceBefore runs the process-local, known-only live cleanup for one
// device.  The caller context controls only how long this caller waits.  The
// actual stop job owns a single absolute five-second deadline and concurrent
// callers for the same target join its done channel instead of duplicating
// media stops.
func (c *Coordinator) ClearDeviceBefore(ctx context.Context, deviceID string, targetEpoch int64) (DeviceCleanupReport, error) {
	if c == nil || ctx == nil || targetEpoch <= 0 || strings.TrimSpace(deviceID) == "" {
		return DeviceCleanupReport{
			DeviceID:    deviceID,
			TargetEpoch: targetEpoch,
			Status:      DeviceCleanupUnknown,
			Unknown:     1,
		}, ErrDeviceCleanupEpochInvalid
	}
	if err := ctx.Err(); err != nil {
		return DeviceCleanupReport{DeviceID: deviceID, TargetEpoch: targetEpoch, Status: DeviceCleanupPending, Pending: 1}, err
	}

	key := deviceCleanupKey{deviceID: deviceID, targetEpoch: targetEpoch}
	c.mu.Lock()
	if c.cleanupJobs == nil {
		c.cleanupJobs = make(map[deviceCleanupKey]*deviceCleanupJob)
	}
	job := c.cleanupJobs[key]
	if job == nil {
		job = &deviceCleanupJob{
			key:    key,
			done:   make(chan struct{}),
			report: DeviceCleanupReport{DeviceID: deviceID, TargetEpoch: targetEpoch, Status: DeviceCleanupPending},
		}
		c.cleanupJobs[key] = job
		go c.runDeviceCleanup(job)
	}
	done := job.done
	c.mu.Unlock()

	if err := waitFor(ctx, done); err != nil {
		// The background job is deliberately left registered.  A later caller
		// joins its exact completion instead of starting a duplicate stop.
		report := c.cleanupJobSnapshot(job)
		report.Status = DeviceCleanupPending
		if report.Pending == 0 {
			report.Pending = 1
		}
		return report, err
	}
	return c.cleanupJobSnapshot(job), nil
}

func (c *Coordinator) cleanupJobSnapshot(job *deviceCleanupJob) DeviceCleanupReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	report := job.report
	report.Warnings = append([]string(nil), report.Warnings...)
	return report
}

func (c *Coordinator) runDeviceCleanup(job *deviceCleanupJob) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), deviceCleanupTimeout)
	defer cancel()

	candidates := c.snapshotDeviceCleanupCandidates(job.key.deviceID)
	results := make(chan deviceCleanupCandidateResult, len(candidates))
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- c.clearDeviceCandidate(cleanupCtx, job.key.targetEpoch, candidate)
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	report := DeviceCleanupReport{
		DeviceID:    job.key.deviceID,
		TargetEpoch: job.key.targetEpoch,
	}
	for result := range results {
		switch {
		case result.pending:
			report.Pending++
		case result.unknown:
			report.Unknown++
		case result.newer:
			report.Newer++
		case result.settled:
			report.Settled++
		}
		if result.warning != nil {
			report.Warnings = append(report.Warnings, result.warning.Error())
		}
	}
	if len(candidates) == 0 {
		report.NoTrackedEvidence = true
	}
	report.Status = deviceCleanupStatus(report)

	c.mu.Lock()
	job.report = report
	close(job.done)
	if c.cleanupJobs[job.key] == job {
		delete(c.cleanupJobs, job.key)
	}
	c.mu.Unlock()
}

func deviceCleanupStatus(report DeviceCleanupReport) DeviceCleanupStatus {
	if report.Pending > 0 {
		return DeviceCleanupPending
	}
	if report.Unknown > 0 || report.Newer > 0 {
		return DeviceCleanupUnknown
	}
	if report.NoTrackedEvidence || report.Settled == 0 {
		return DeviceCleanupNoTrackedEvidence
	}
	return DeviceCleanupSettled
}

func (c *Coordinator) snapshotDeviceCleanupCandidates(deviceID string) []deviceCleanupCandidate {
	c.mu.Lock()
	defer c.mu.Unlock()
	candidates := make([]deviceCleanupCandidate, 0)
	for key, entry := range c.entries {
		if key.deviceID == deviceID && entry != nil {
			candidates = append(candidates, deviceCleanupCandidate{key: key, entry: entry})
		}
	}
	return candidates
}

func (c *Coordinator) clearDeviceCandidate(ctx context.Context, targetEpoch int64, candidate deviceCleanupCandidate) deviceCleanupCandidateResult {
	wasStopping := false
	for {
		c.mu.Lock()
		entry := c.entries[candidate.key]
		if entry != candidate.entry {
			if entry == nil && wasStopping && candidate.entry.state == LiveStateIdle &&
				candidate.entry.operationEpoch > 0 && candidate.entry.operationEpoch < targetEpoch &&
				stopReachedMediaTerminal(candidate.entry.err) {
				warning := terminalWarning(candidate.entry.err)
				c.mu.Unlock()
				// The exact entry completed and was removed while this caller
				// joined its stop. A replacement is intentionally never touched.
				return deviceCleanupCandidateResult{settled: true, warning: warning}
			}
			c.mu.Unlock()
			return deviceCleanupCandidateResult{unknown: true}
		}
		if entry.operationEpoch >= targetEpoch {
			c.mu.Unlock()
			return deviceCleanupCandidateResult{newer: true}
		}
		if entry.operationEpoch <= 0 && entry.state != LiveStateStarting {
			c.mu.Unlock()
			return deviceCleanupCandidateResult{unknown: true}
		}
		if wasStopping && entry.state != LiveStateStopping {
			warning := entry.err
			c.mu.Unlock()
			return deviceCleanupCandidateResult{pending: true, warning: warning}
		}
		switch entry.state {
		case LiveStateStarting, LiveStateStopping:
			done := entry.done
			wasStopping = wasStopping || entry.state == LiveStateStopping
			c.mu.Unlock()
			if err := waitFor(ctx, done); err != nil {
				return deviceCleanupCandidateResult{pending: true, warning: err}
			}
			continue
		case LiveStateReady, LiveStateCleanupPending:
			failureState := entry.state
			entry.state = LiveStateStopping
			entry.done = make(chan struct{})
			result := entry.result
			c.mu.Unlock()

			err := c.runCleanupStop(ctx, candidate.key, candidate.entry, failureState, result)
			if stopReachedMediaTerminal(err) {
				return deviceCleanupCandidateResult{settled: true, warning: terminalWarning(err)}
			}
			return deviceCleanupCandidateResult{pending: true, warning: err}
		default:
			c.mu.Unlock()
			return deviceCleanupCandidateResult{unknown: true}
		}
	}
}

func (c *Coordinator) runCleanupStop(ctx context.Context, key coordinatorKey, entry *coordinatorEntry, failureState LiveState, result *Result) error {
	if c.stop == nil || result == nil {
		err := ErrPlayAuthorizationUnavailable
		c.finishStop(key, entry, err, failureState)
		return err
	}
	stopDone := make(chan error, 1)
	go func() {
		stopDone <- c.stop(ctx, result)
	}()
	select {
	case err := <-stopDone:
		c.finishStop(key, entry, err, failureState)
		return err
	case <-ctx.Done():
		// Keep the exact entry in Stopping until the real stop returns.  A
		// later clear will join that done channel rather than issue a second
		// close for the same pointer.
		go func() {
			err := <-stopDone
			c.finishStop(key, entry, err, failureState)
		}()
		return ctx.Err()
	}
}

// ClearDeviceBefore reports current-process live evidence only. It does not
// infer recovery completeness, drain other media subsystems, or acknowledge a
// durable device watermark. The aggregation owner must first drain old leases.
func (s *Service) ClearDeviceBefore(ctx context.Context, deviceID string, targetEpoch int64) (DeviceCleanupReport, error) {
	if s == nil {
		return DeviceCleanupReport{DeviceID: deviceID, TargetEpoch: targetEpoch, Status: DeviceCleanupUnknown, Unknown: 1}, ErrPlayAuthorizationUnavailable
	}
	return s.coordinator().ClearDeviceBefore(ctx, deviceID, targetEpoch)
}

func terminalWarning(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("media terminal with cleanup warning: %w", err)
}

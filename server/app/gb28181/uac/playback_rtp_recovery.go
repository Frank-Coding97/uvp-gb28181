package uac

import (
	"context"
	"errors"
	"reflect"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// This controller outlives restartable scan workers. The shared barrier remains
// the authority; this bounded table retains only the creator's opaque handles.
type playbackRTPCleanup struct {
	ctx      context.Context
	cancel   context.CancelFunc
	store    *playauth.DeviceOperationIntentStore
	barrier  *playauth.DeviceOperationBarrier
	resolver playauth.RTPCleanupResolver
	owners   map[string]*playbackRTPOwner // Protected by playbackIntentMu.
}

type playbackRTPPhase uint8

const (
	playbackRTPPrepare playbackRTPPhase = iota
	playbackRTPRun
	playbackRTPQuiesce
)

type playbackRTPOwner struct {
	id            playauth.DeviceOperationIntentIdentity
	key           string
	work          *playauth.RTPRecoveryWork
	phase         playbackRTPPhase // Same-device scan serialization; shutdown joins scans.
	prepareFailed bool
}

// ConfigurePlaybackRTPCleanup binds startup-only recovery dependencies to this
// UAC, before starting its scanner. ctx is the UAC lifetime, not a worker/page.
// Omitting this configuration preserves the existing SIP-only recovery path.
func (u *UAC) ConfigurePlaybackRTPCleanup(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, resolver playauth.RTPCleanupResolver) error {
	if u == nil || ctx == nil || ctx.Err() != nil || store == nil || barrier == nil || resolver == nil {
		return ErrPlaybackUnavailable
	}
	v := reflect.ValueOf(resolver)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return ErrPlaybackUnavailable
		}
	}
	u.playbackIntentMu.Lock()
	defer u.playbackIntentMu.Unlock()
	if !u.reservePlaybackBarrierLocked(barrier) || u.playbackRTPCleanup != nil || u.playbackRecoveryWorker != nil || len(u.playbackRecoveryScans) != 0 {
		return ErrPlaybackUnavailable
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	u.playbackRTPCleanup = &playbackRTPCleanup{ctx: ownerCtx, cancel: cancel, store: store, barrier: barrier, resolver: resolver, owners: make(map[string]*playbackRTPOwner)}
	return nil
}

func (u *UAC) playbackRTPDependenciesMatchLocked(store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier) bool {
	c := u.playbackRTPCleanup
	return c == nil || (c.store == store && c.barrier == barrier && c.ctx.Err() == nil)
}

// Resume lease holders before SIP/new RTP waits on the same device barrier.
// Attempted batches are not reserved again in this page. A failed Prepare
// that has safely Quiesced can re-enter after SIP, avoiding sweep starvation.
func (u *UAC) resumePlaybackRTP(ctx context.Context, devicePK, beforeEpoch int64) (map[string]bool, error) {
	u.playbackIntentMu.Lock()
	c := u.playbackRTPCleanup
	var entries []*playbackRTPOwner
	if c != nil {
		for _, e := range c.owners {
			if e.id.DevicePK == devicePK && e.id.DeviceEpoch < beforeEpoch {
				entries = append(entries, e)
			}
		}
	}
	u.playbackIntentMu.Unlock()
	seen := make(map[string]bool, len(entries))
	var result error
	for _, e := range entries {
		seen[e.key] = true
		result = errors.Join(result, u.runPlaybackRTPOwner(ctx, c, e))
		if e.prepareFailed {
			u.playbackIntentMu.Lock()
			retained := c.owners[e.key] == e
			u.playbackIntentMu.Unlock()
			if !retained {
				delete(seen, e.key)
			}
		}
	}
	return seen, result
}

func (u *UAC) recoverPlaybackRTP(ctx context.Context, id playauth.DeviceOperationIntentIdentity, seen map[string]bool) error {
	u.playbackIntentMu.Lock()
	c := u.playbackRTPCleanup
	u.playbackIntentMu.Unlock()
	if c == nil {
		return nil
	}
	if !playbackIntentKind(id.Kind) || id.TargetScope != "channel" {
		return ErrPlaybackCleanupUnknown
	}
	loaded, err := c.store.LoadRTPResourceSteps(ctx, id)
	if err != nil {
		return err
	}
	var result error
	for _, step := range loaded.Steps {
		key := id.OperationID + ":" + step.Identity.StepID
		if step.State != playauth.RTPStepMayHaveDispatched || seen[key] {
			continue
		}
		seen[key] = true
		u.playbackIntentMu.Lock()
		if u.playbackShuttingDown || c.ctx.Err() != nil {
			u.playbackIntentMu.Unlock()
			return errors.Join(result, ErrPlaybackUnavailable)
		}
		entry := c.owners[key]
		if entry == nil {
			if len(c.owners) >= maxPlaybackIntentOperations {
				u.playbackIntentMu.Unlock()
				return errors.Join(result, ErrPlaybackUnavailable)
			}
			// Reserve performs no SQL/network and must precede publication.
			// The UAC lock makes publication atomic with shutdown admission.
			work, reserveErr := c.barrier.ReserveRTPCleanup(c.ctx, c.store, id, step.Identity.StepID)
			if reserveErr != nil {
				u.playbackIntentMu.Unlock()
				result = errors.Join(result, reserveErr)
				continue
			}
			entry = &playbackRTPOwner{id: id, key: key, work: work}
			c.owners[key] = entry
		}
		u.playbackIntentMu.Unlock()
		if entry.id != id {
			result = errors.Join(result, ErrPlaybackUnavailable)
			continue
		}
		result = errors.Join(result, u.runPlaybackRTPOwner(ctx, c, entry))
	}
	return result
}

func (u *UAC) runPlaybackRTPOwner(ctx context.Context, c *playbackRTPCleanup, e *playbackRTPOwner) error {
	var result error
	if e.phase == playbackRTPPrepare {
		if err := e.work.Prepare(ctx); err != nil {
			// Even a pre-CAS cancellation follows safe Quiesce. Never guess
			// whether Prepare committed and repeat an authorization attempt.
			result = err
			e.prepareFailed = true
			e.phase = playbackRTPQuiesce
		} else {
			e.phase = playbackRTPRun
		}
	}
	if e.phase == playbackRTPRun {
		result = e.work.Run(ctx, c.resolver)
		d, err := e.work.BatchDisposition(ctx)
		if err != nil {
			return errors.Join(result, err)
		}
		if d != playauth.RTPRecoveryReadyToQuiesce {
			return result
		}
		e.phase = playbackRTPQuiesce
	}
	if err := e.work.Quiesce(ctx); err != nil {
		return errors.Join(result, err)
	}
	u.playbackIntentMu.Lock()
	if c.owners[e.key] == e {
		delete(c.owners, e.key)
	}
	u.playbackIntentMu.Unlock()
	return result
}

// Called only after shutdown has sealed admission and actually joined scans.
// Re-snapshot here, not before joining, to include late-published owners.
func (u *UAC) shutdownPlaybackRTP(ctx context.Context) error {
	u.playbackIntentMu.Lock()
	c := u.playbackRTPCleanup
	var entries []*playbackRTPOwner
	if c != nil {
		for _, e := range c.owners {
			entries = append(entries, e)
		}
	}
	u.playbackIntentMu.Unlock()
	var result error
	for _, e := range entries {
		e.phase = playbackRTPQuiesce
		result = errors.Join(result, u.runPlaybackRTPOwner(ctx, c, e))
	}
	return result
}

func playbackRecoverySIPBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := playbackTeardownTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining < budget {
			budget = remaining
		}
	}
	return context.WithTimeout(ctx, budget)
}

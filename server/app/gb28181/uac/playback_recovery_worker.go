package uac

import (
	"context"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// PlaybackRecoveryTick contains scheduling counters only. RoundEnded means
// reaching a finite device cursor boundary, never coverage or completion.
type PlaybackRecoveryTick struct {
	Devices, Scanned, Cancelled, Pending, Invalid int
	RoundEnded                                    bool
}

// One shared operation watermark avoids an unbounded per-device cursor map.
// Each device gets one page per finite PK round. The minimum nonempty page
// cursor advances the next round without skipping another device's records;
// an empty round resets it to catch smaller, randomly generated late IDs.
type playbackRecoveryWorker struct {
	devices                    *playauth.DeviceCleanupStore
	recoverPage                func(context.Context, int64, string, int64, string, int) (PlaybackRecoveryPage, error)
	deviceLimit                int
	intentLimit                int
	deviceBudget               time.Duration
	work                       chan struct{}
	deviceAfter, deviceThrough int64 // Protected by work, including all Tick I/O.
	operationAfter, minNext    string
	cancel                     context.CancelFunc // Set before publishing the running worker.
	done                       chan struct{}
}

func newPlaybackRecoveryWorker(u *UAC, devices *playauth.DeviceCleanupStore, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier) *playbackRecoveryWorker {
	return &playbackRecoveryWorker{
		devices: devices, deviceLimit: 16, intentLimit: 16, deviceBudget: playbackTeardownTimeout,
		work: make(chan struct{}, 1),
		recoverPage: func(ctx context.Context, pk int64, code string, epoch int64, after string, limit int) (PlaybackRecoveryPage, error) {
			return u.RecoverPlaybackIntents(ctx, store, barrier, pk, code, epoch, after, limit)
		},
	}
}

func (w *playbackRecoveryWorker) Tick(ctx context.Context) (PlaybackRecoveryTick, error) {
	var result PlaybackRecoveryTick
	if w == nil || w.devices == nil || w.recoverPage == nil || ctx == nil {
		return result, ErrPlaybackUnavailable
	}
	select {
	case w.work <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	}
	defer func() { <-w.work }()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, w.deviceBudget)
	defer cancelDiscovery()
	if w.deviceThrough == 0 {
		upper, err := w.devices.PendingUpperBound(discoveryCtx)
		if err != nil {
			return result, err
		}
		w.deviceThrough = upper
		if upper == 0 {
			w.endRound(&result)
			return result, nil
		}
	}
	page, firstErr := w.devices.DiscoverPending(discoveryCtx, w.deviceAfter, w.deviceThrough, w.deviceLimit)
	cancelDiscovery() // Metadata reads end before any per-device network work.
	if firstErr != nil && page.NextPK == 0 {
		return result, firstErr // No query result: preserve the current cursor.
	}
	result.Invalid = page.Invalid
	for _, target := range page.Targets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		callCtx, cancel := context.WithTimeout(ctx, w.deviceBudget)
		p, err := w.recoverPage(callCtx, target.DevicePK, target.DeviceID, target.AccessEpoch, w.operationAfter, w.intentLimit)
		cancel()
		result.Devices++
		result.Scanned += p.Scanned
		result.Cancelled += p.Cancelled
		result.Pending += p.Pending
		// Unknown is a normal dispatched-page outcome. Actual cursor progress
		// remains useful even when an attempt/coverage is still unresolved.
		if p.Scanned > 0 && p.NextAfter > w.operationAfter && (w.minNext == "" || p.NextAfter < w.minNext) {
			w.minNext = p.NextAfter
		}
		if firstErr == nil {
			firstErr = err
		}
		w.deviceAfter = target.DevicePK
	}
	// This also advances past malformed rows without granting them authority.
	if page.NextPK > w.deviceAfter {
		w.deviceAfter = page.NextPK
	}
	if page.NextPK == 0 || w.deviceAfter >= w.deviceThrough {
		w.endRound(&result)
	}
	return result, firstErr
}

func (w *playbackRecoveryWorker) endRound(result *PlaybackRecoveryTick) {
	w.operationAfter = w.minNext // Empty/error-only round means uncertain rescan.
	w.deviceAfter, w.deviceThrough, w.minNext = 0, 0, ""
	result.RoundEnded = true
}

// StartPlaybackRecovery starts compensation only. Root must inject its single
// UAC and barrier and stop this loop before shutting down SIP/DB dependencies.
// report runs synchronously and must return promptly. The stop function may
// time out; retry it to join the SAME worker rather than starting a replacement.
// Stopping does not close detached observation or discard uncertain owners.
func (u *UAC) StartPlaybackRecovery(ctx context.Context, devices *playauth.DeviceCleanupStore, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, report func(PlaybackRecoveryTick, error)) (func(context.Context) error, error) {
	if ctx == nil || ctx.Err() != nil || u == nil || u.client == nil || u.client.TxRequester != nil || devices == nil || store == nil || barrier == nil {
		return nil, ErrPlaybackUnavailable
	}
	w := newPlaybackRecoveryWorker(u, devices, store, barrier)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	u.playbackIntentMu.Lock()
	if !u.reservePlaybackBarrierLocked(barrier) || !u.playbackRTPDependenciesMatchLocked(store, barrier) || u.playbackRecoveryWorker != nil {
		u.playbackIntentMu.Unlock()
		cancel()
		return nil, ErrPlaybackUnavailable
	}
	u.playbackRecoveryWorker = w
	u.playbackIntentMu.Unlock()
	go func() {
		defer func() {
			cancel()
			u.playbackIntentMu.Lock()
			if u.playbackRecoveryWorker == w {
				u.playbackRecoveryWorker = nil
			}
			u.playbackIntentMu.Unlock()
			close(done)
		}()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
				result, err := w.Tick(runCtx)
				if report != nil {
					report(result, err)
				}
				timer.Reset(time.Second)
			}
		}
	}()
	return func(waitCtx context.Context) error {
		if waitCtx == nil {
			return ErrPlaybackUnavailable
		}
		cancel()
		select {
		case <-done:
			return nil
		case <-waitCtx.Done():
			return errors.Join(ErrPlaybackCleanupUnknown, waitCtx.Err())
		}
	}, nil
}

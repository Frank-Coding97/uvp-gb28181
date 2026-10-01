// Package devicecleanup owns the device-level aggregation decision behind the
// durable media cleanup barrier.
//
// playauth.DeviceCleanupStore is only a compare-and-swap. An ownership transfer
// raises access_epoch and leaves cleanup_completed_epoch behind, and every
// media admission for that device is refused until the two are equal again
// (playauth/device_security.go, fail-closed, with no administrator exemption).
// Nothing else advances that watermark, so without this reconciler a single
// transfer makes the device permanently unable to play.
//
// This package is the missing owner. A device is provably clean for its current
// access_epoch only when one pass proves all of:
//
//  1. no durable device operation intent older than that epoch is still
//     unsettled (an exact head read of a monotonically shrinking set);
//  2. the current process's live media for the device has stopped and the
//     platform's durable stream projection no longer lists the device;
//  3. the same durable intent read still holds after that drain, so a late
//     intent or a fresh transfer cannot be masked by the earlier read.
//
// Anything unproven leaves the barrier closed. The CAS inside Complete is the
// final guard: it refuses a target epoch that is no longer current.
package devicecleanup

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const (
	// DefaultInterval is fast enough that an operator sees a transferred device
	// play again within seconds, and only a pending device costs real work.
	DefaultInterval = 5 * time.Second
	defaultBatch    = 50
	maxBatch        = 200
)

var ErrReconcilerUnavailable = errors.New("device cleanup reconciler unavailable")

// LiveMedia drains this process's live media for one device. An implementation
// must stop the real sessions rather than report identifiers, and must return
// nil only when the process-local stop reached media terminal. An unknown or
// replaced owner is an error, never a silent success.
type LiveMedia interface {
	SettleDeviceBefore(ctx context.Context, deviceID string, targetEpoch int64) error
}

// StreamProjection reports the devices the platform durably believes are
// streaming right now (gb_channel.stream_id, kept honest by the periodic
// stream reconciler). It is the cross-process half of the live evidence: a
// process-local drain cannot speak for a session another instance still owns.
type StreamProjection interface {
	ListPlayingChannels(ctx context.Context) (models.GbChannelList, error)
}

// Stats is one reconciliation pass.
type Stats struct {
	Examined  int // pending devices inspected
	Completed int // barriers advanced by this pass
	Pending   int // devices still unproven, or taken over by a newer transfer
	Failed    int // dependency failures; the barrier stayed closed
}

type Reconciler struct {
	devices  *playauth.DeviceCleanupStore
	intents  *playauth.DeviceOperationIntentStore
	live     LiveMedia
	streams  StreamProjection
	interval time.Duration
	batch    int
	report   func(Stats, error)

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	running bool
}

type Option func(*Reconciler)

func WithInterval(interval time.Duration) Option {
	return func(r *Reconciler) {
		if interval > 0 {
			r.interval = interval
		}
	}
}

func WithBatch(batch int) Option {
	return func(r *Reconciler) {
		if batch > 0 {
			if batch > maxBatch {
				batch = maxBatch
			}
			r.batch = batch
		}
	}
}

// WithReport observes every pass synchronously. It must return promptly.
func WithReport(report func(Stats, error)) Option {
	return func(r *Reconciler) {
		if report != nil {
			r.report = report
		}
	}
}

// New requires every evidence source. A missing live media authority or stream
// projection cannot be defaulted: without them the reconciler could only guess,
// and guessing is what leaves the barrier closed forever.
func New(devices *playauth.DeviceCleanupStore, intents *playauth.DeviceOperationIntentStore, live LiveMedia, streams StreamProjection, options ...Option) (*Reconciler, error) {
	if devices == nil || intents == nil || isNilInterface(live) || isNilInterface(streams) {
		return nil, ErrReconcilerUnavailable
	}
	r := &Reconciler{
		devices: devices, intents: intents, live: live, streams: streams,
		interval: DefaultInterval, batch: defaultBatch,
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r, nil
}

// Start runs one pass immediately and then every interval until Stop. A
// reconciler is single-owner: a second Start on the same value is refused.
func (r *Reconciler) Start(ctx context.Context) error {
	if r == nil || ctx == nil || ctx.Err() != nil {
		return ErrReconcilerUnavailable
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrReconcilerUnavailable
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.running, r.cancel, r.done = true, cancel, done
	r.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			r.mu.Lock()
			r.running, r.cancel, r.done = false, nil, nil
			r.mu.Unlock()
			close(done)
		}()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
				stats, err := r.Tick(runCtx)
				if r.report != nil {
					r.report(stats, err)
				}
				timer.Reset(r.interval)
			}
		}
	}()
	return nil
}

// Stop cancels the loop and joins it. A timeout returns the wait error while
// the loop keeps draining in the background; retrying joins the same loop.
func (r *Reconciler) Stop(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrReconcilerUnavailable
	}
	r.mu.Lock()
	cancel, done, running := r.cancel, r.done, r.running
	r.mu.Unlock()
	if !running {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Tick reconciles one bounded page of pending devices. It never aborts the
// page on one device's failure: a poisoned device must not starve the rest.
func (r *Reconciler) Tick(ctx context.Context) (Stats, error) {
	var stats Stats
	if r == nil || ctx == nil || ctx.Err() != nil || r.devices == nil || r.intents == nil ||
		isNilInterface(r.live) || isNilInterface(r.streams) {
		return stats, ErrReconcilerUnavailable
	}
	pending, err := r.devices.ListPending(ctx, r.batch)
	if err != nil {
		return stats, err
	}
	var firstErr error
	for _, state := range pending {
		if err := ctx.Err(); err != nil {
			return stats, errors.Join(firstErr, err)
		}
		stats.Examined++
		switch err := r.reconcileDevice(ctx, state); {
		case err == nil:
			stats.Completed++
		case isNotYetProven(err):
			stats.Pending++
		default:
			stats.Failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return stats, firstErr
}

// isNotYetProven separates "the device is not clean yet" from "a dependency
// failed". Both keep the barrier closed, but only the latter is worth an error
// log: an unsettled intent or a live session is the expected steady state.
func isNotYetProven(err error) bool {
	return errors.Is(err, playauth.ErrDeviceCleanupPending) ||
		errors.Is(err, playauth.ErrDeviceCleanupStaleTarget) ||
		errors.Is(err, playauth.ErrDeviceCleanupRevoked) ||
		errors.Is(err, playauth.ErrDeviceCleanupMismatch) ||
		errors.Is(err, playauth.ErrDeviceIntentRevoked) ||
		errors.Is(err, context.DeadlineExceeded)
}

func (r *Reconciler) reconcileDevice(ctx context.Context, state playauth.DeviceCleanupState) error {
	if strings.TrimSpace(state.DeviceID) == "" || state.AccessEpoch <= 0 {
		return ErrReconcilerUnavailable
	}
	unsettled, err := r.intents.HasUnsettledBefore(ctx, state.DeviceID, state.AccessEpoch)
	if err != nil {
		return err
	}
	if unsettled {
		return playauth.ErrDeviceCleanupPending
	}
	if err := r.settleLive(ctx, state); err != nil {
		return err
	}
	// Re-read after the drain. A transfer that committed during it must not be
	// masked by the earlier read; Complete's CAS would refuse its own target,
	// but a silently stale pass still wastes an operator's transfer.
	unsettled, err = r.intents.HasUnsettledBefore(ctx, state.DeviceID, state.AccessEpoch)
	if err != nil {
		return err
	}
	if unsettled {
		return playauth.ErrDeviceCleanupPending
	}
	return r.devices.Complete(ctx, state.DeviceID, state.AccessEpoch)
}

// isNilInterface rejects a typed nil dependency, which would otherwise satisfy
// an interface check and turn a missing authority into an assumed-clean device.
func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (r *Reconciler) settleLive(ctx context.Context, state playauth.DeviceCleanupState) error {
	if err := r.live.SettleDeviceBefore(ctx, state.DeviceID, state.AccessEpoch); err != nil {
		return err
	}
	playing, err := r.streams.ListPlayingChannels(ctx)
	if err != nil {
		return err
	}
	for _, channel := range playing {
		if channel != nil && channel.DeviceID == state.DeviceID {
			return fmt.Errorf("%w: 设备在流投影中仍有活动流 %s", playauth.ErrDeviceCleanupPending, channel.ChannelID)
		}
	}
	return nil
}

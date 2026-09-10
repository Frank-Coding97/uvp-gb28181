package workrecording

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// AbandonedStartGrace is how long a channel claim may stay in starting before
// it is treated as an abandoned reservation.
//
// A live start cannot exceed workOperationLimit: Service.Start bounds the whole
// reserve → prepare → Start sequence with one deadline, and every recorder call
// inherits it. A recording has already left starting, and a job waiting to be
// stopped sits in stopping. So a claim still in starting well past that deadline
// can only be a reservation whose start was interrupted.
const AbandonedStartGrace = 5 * time.Minute

// AbandonedStartInterval is how often the reconciler looks for abandoned
// reservations. It is deliberately longer than one sweep needs to be, because
// reclaiming is a repair path rather than a latency-sensitive one.
const AbandonedStartInterval = time.Minute

// ReclaimAbandonedStarts releases channel claims that a start reserved but never
// bound, once they are old enough to prove that start is gone.
//
// This path exists because a claim left in starting is otherwise permanent: the
// engine's own readers cannot clear it. Observe returns starting unchanged, Get
// can only fold the job back to unknown, and the only writers that reach
// AbortStarting need the job to be reachable through the work-order API. A start
// that failed before its ledger row was attached has no such entry, so without
// this sweep the channel stays reserved and every later recording on it fails
// with ErrOwnerConflict.
//
// Reclaiming goes through Stop, the same audited path an operator uses, so the
// claim is yielded with the exact version fence and the job is left in a
// terminal state with the cause recorded. A claim that Stop cannot take over is
// reported, never forced.
func (s *Service) ReclaimAbandonedStarts(ctx context.Context, olderThan time.Duration) (int, error) {
	if s == nil || s.db == nil || s.recorder == nil || ctx == nil || olderThan <= 0 {
		return 0, ErrInvalidRequest
	}
	cutoff := s.now().Add(-olderThan)
	var claims []models.GbRecorderClaim
	if err := s.db.WithContext(ctx).
		Where("owner_kind = ? AND state = ? AND updated_at < ?", OwnerWork, StateStarting, cutoff).
		Order("updated_at").
		Find(&claims).Error; err != nil {
		return 0, err
	}

	reclaimed := 0
	var failures []error
	for _, claim := range claims {
		if claim.ChannelID == 0 || !validJobID(claim.OwnerID) {
			failures = append(failures, fmt.Errorf("废弃占用缺少可识别的归属: %s", claim.ResourceKey))
			continue
		}
		if _, err := s.findJob(ctx, claim.OwnerID); err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				failures = append(failures, fmt.Errorf("查询作业 %s 失败: %w", claim.OwnerID, err))
				continue
			}
			// The job row is gone, so there is no ledger entry left to drive;
			// the claim itself is the only thing that can be released.
			handle := RecorderHandle{ChannelID: claim.ChannelID, Owner: Owner{Kind: OwnerWork, ID: claim.OwnerID}, Version: claim.Version}
			if err := s.recorder.AbortStarting(ctx, handle); err != nil {
				failures = append(failures, fmt.Errorf("回收通道 %d 的废弃占用失败: %w", claim.ChannelID, err))
				continue
			}
			reclaimed++
			continue
		}
		// Stop re-reads the claim under the channel lock, so a claim that moved
		// on since the scan is skipped instead of being forced.
		if _, err := s.Stop(ctx, claim.OwnerID); err != nil {
			failures = append(failures, fmt.Errorf("回收通道 %d 的废弃占用失败: %w", claim.ChannelID, err))
			continue
		}
		reclaimed++
	}
	return reclaimed, errors.Join(failures...)
}

// AbandonedStartReconciler runs ReclaimAbandonedStarts on a fixed interval.
// It exists because the engine has no other way back from an interrupted start,
// so leaving it out means one failed start blocks a channel indefinitely.
type AbandonedStartReconciler struct {
	service  *Service
	grace    time.Duration
	interval time.Duration

	stop chan struct{}
	done chan struct{}
}

// NewAbandonedStartReconciler builds the reconciler. A nil service, a zero
// interval or a zero grace disables it, so bootstrap can wire it unconditionally.
func NewAbandonedStartReconciler(service *Service, interval, grace time.Duration) *AbandonedStartReconciler {
	if service == nil || interval <= 0 || grace <= 0 {
		return nil
	}
	return &AbandonedStartReconciler{
		service:  service,
		grace:    grace,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start runs the sweep loop until Stop is called.
func (r *AbandonedStartReconciler) Start(ctx context.Context) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Reclaiming is best effort: a failure here must not stop the
				// loop, because the next tick is the retry.
				_, _ = r.service.ReclaimAbandonedStarts(ctx, r.grace)
			}
		}
	}()
}

// Stop waits for the running sweep to finish. It is safe to call on a nil
// reconciler and safe to call more than once.
func (r *AbandonedStartReconciler) Stop() {
	if r == nil {
		return
	}
	select {
	case <-r.stop:
		return
	default:
	}
	close(r.stop)
	<-r.done
}

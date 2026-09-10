package recording

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	defaultReconcileInterval = 30 * time.Second
	defaultChannelTimeout    = 10 * time.Second
)

type Reconciler struct {
	repo           Repository
	service        *Service
	interval       time.Duration
	channelTimeout time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewReconciler(repo Repository, service *Service, interval, channelTimeout time.Duration) *Reconciler {
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	if channelTimeout <= 0 {
		channelTimeout = defaultChannelTimeout
	}
	return &Reconciler{repo: repo, service: service, interval: interval, channelTimeout: channelTimeout}
}

func (r *Reconciler) RunOnce(ctx context.Context) error {
	enabled, err := r.repo.ListEnabledChannels(ctx)
	if err != nil {
		return err
	}
	unfinished, err := r.repo.ListUnfinishedSessions(ctx)
	if err != nil {
		return err
	}

	channelIDs := make([]uint, 0, len(enabled)+len(unfinished))
	seen := make(map[uint]struct{}, cap(channelIDs))
	for _, channel := range enabled {
		seen[channel.ID] = struct{}{}
		channelIDs = append(channelIDs, channel.ID)
	}
	for _, session := range unfinished {
		if _, ok := seen[session.ChannelID]; ok {
			continue
		}
		seen[session.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, session.ChannelID)
	}

	var errs []error
	for _, channelID := range channelIDs {
		channelCtx, cancel := context.WithTimeout(ctx, r.channelTimeout)
		err := r.service.ReconcileChannel(channelCtx, channelID)
		cancel()
		if err != nil {
			errs = append(errs, err)
			app.Log(channelCtx).Named("recording.reconcile").Warn("云端录像通道对账失败",
				zap.String("event", "recording.reconcile.channel_failed"),
				zap.Uint("channelId", channelID), zap.Error(err))
		}
	}
	return errors.Join(errs...)
}

func (r *Reconciler) Start(parent context.Context) {
	r.mu.Lock()
	if r.done != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()

	go func() {
		defer close(done)
		_ = r.RunOnce(ctx)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = r.RunOnce(ctx)
			}
		}
	}()
}

func (r *Reconciler) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	<-done
}

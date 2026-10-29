// Package limit implements only per-process request budgets. Persistent media
// quotas and replay protection must never use these in-memory counters.
package limit

import (
	"errors"
	"golang.org/x/time/rate"
	"sync"
	"time"
)

var ErrRateLimited = errors.New("openapi rate limited")
var ErrInvalidConfiguration = errors.New("invalid openapi rate configuration")

type buckets struct {
	general, play *rate.Limiter
	last          time.Time
}
type Manager struct {
	mu      sync.Mutex
	now     func() time.Time
	lastGC  time.Time
	clients map[int64]*buckets
}

func New(now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{now: now, clients: make(map[int64]*buckets)}
}

// Allow is called only for an authenticated, authorized numeric client ID.
// Never call it using an unverified claimed AK. Configuration comes from the
// current client row, after the platform's management capacity validation.
func (m *Manager) Allow(clientID int64, perSecond, burst int, play bool) (time.Duration, error) {
	if clientID <= 0 || perSecond <= 0 || burst <= 0 {
		return 0, ErrInvalidConfiguration
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if now.Sub(m.lastGC) >= time.Minute {
		for id, b := range m.clients {
			if now.Sub(b.last) > 10*time.Minute {
				delete(m.clients, id)
			}
		}
		m.lastGC = now
	}
	b := m.clients[clientID]
	if b == nil {
		b = &buckets{general: rate.NewLimiter(rate.Limit(perSecond), burst), play: rate.NewLimiter(1, 2)}
		m.clients[clientID] = b
	}
	b.last = now
	b.general.SetLimitAt(now, rate.Limit(perSecond))
	b.general.SetBurstAt(now, burst)
	general := b.general.ReserveN(now, 1)
	retry := general.DelayFrom(now)
	var playback *rate.Reservation
	if play {
		playback = b.play.ReserveN(now, 1)
		if delay := playback.DelayFrom(now); delay > retry {
			retry = delay
		}
	}
	if retry > 0 {
		general.CancelAt(now)
		if playback != nil {
			playback.CancelAt(now)
		}
		return retry, ErrRateLimited
	}
	return 0, nil
}

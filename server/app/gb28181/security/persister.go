package security

import (
	"context"
	"sync"
	"time"
)

const securityEventQueueCapacity = 4096

type eventPersister struct {
	store Store
	clock Clock
	queue chan Event
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newEventPersister(store Store, clock Clock) *eventPersister {
	p := &eventPersister{store: store, clock: clock, queue: make(chan Event, securityEventQueueCapacity), stop: make(chan struct{}), done: make(chan struct{})}
	go p.run()
	return p
}

func (p *eventPersister) Enqueue(event Event) bool {
	select {
	case p.queue <- event:
		return true
	default:
		return false
	}
}

func (p *eventPersister) Close(ctx context.Context) error {
	p.once.Do(func() { close(p.stop) })
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *eventPersister) run() {
	defer close(p.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	pending := make(map[string]EventAggregate)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		items := make([]EventAggregate, 0, len(pending))
		for _, item := range pending {
			items = append(items, item)
		}
		if p.store.IncrementEvents(context.Background(), items) == nil {
			clear(pending)
		}
	}
	for {
		select {
		case event := <-p.queue:
			p.aggregate(pending, event)
		case <-ticker.C:
			flush()
		case <-p.stop:
			for {
				select {
				case event := <-p.queue:
					p.aggregate(pending, event)
				default:
					flush()
					return
				}
			}
		}
	}
}

func (p *eventPersister) aggregate(pending map[string]EventAggregate, event Event) {
	ip, err := ValidateSource(event.SourceIP)
	if err != nil {
		return
	}
	at := event.Occurred
	if at.IsZero() {
		at = p.clock.Now()
	}
	bucket := at.Truncate(time.Minute)
	key := aggregateKey(bucket, ip.String(), event.DeviceID, riskScopeForEvent(event), event.Transport, event.Method, event.Reason, event.Action)
	item, ok := pending[key]
	if !ok {
		item = EventAggregate{BucketAt: bucket, SourceIP: ip.String(), DeviceID: event.DeviceID, RiskScope: riskScopeForEvent(event), Transport: event.Transport, Method: event.Method, Reason: event.Reason, Action: event.Action, FirstSeenAt: at}
	}
	item.Count++
	item.ScoreDelta += int64(event.Score)
	item.LastSeenAt = at
	pending[key] = item
}

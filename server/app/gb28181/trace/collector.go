package trace

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

type Event struct {
	OccurredAt time.Time
	Direction  Direction
	Transport  string
	LocalAddr  string
	RemoteAddr string
	Raw        []byte
	Malformed  bool
	ParseError string
}

type StoredEvent struct {
	OccurredAt time.Time
	Direction  Direction
	Transport  string
	LocalAddr  string
	RemoteAddr string
	Malformed  bool
	ParseError string
	Payload    EncryptedPayload
}

type Store interface {
	InsertBatch(context.Context, []StoredEvent) error
}

type Collector struct {
	mu      sync.RWMutex
	queue   chan Event
	closed  bool
	dropped atomic.Uint64
	onDrop  func()
}

func NewCollector(capacity int, onDrop func()) *Collector {
	if capacity <= 0 {
		capacity = 8192
	}
	return &Collector{queue: make(chan Event, capacity), onDrop: onDrop}
}

func (c *Collector) Submit(event Event) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return false
	}
	event.Raw = append([]byte(nil), event.Raw...)
	select {
	case c.queue <- event:
		return true
	default:
		c.dropped.Add(1)
		if c.onDrop != nil {
			c.onDrop()
		}
		return false
	}
}

func (c *Collector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.queue)
}

func (c *Collector) Dropped() uint64 { return c.dropped.Load() }
func (c *Collector) Depth() int      { return len(c.queue) }
func (c *Collector) Capacity() int   { return cap(c.queue) }

func (c *Collector) Drop(count uint64) {
	c.dropped.Add(count)
}

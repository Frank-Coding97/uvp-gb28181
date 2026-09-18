package play

import "sync"

type sourceConsumerLease struct {
	generation uint64
	count      int
}

// SourceLeaseRegistry tracks non-browser consumers by stream generation.
// A delayed release from an older generation can never remove the current one.
type SourceLeaseRegistry struct {
	mu      sync.Mutex
	streams map[string]map[string]sourceConsumerLease
}

func NewSourceLeaseRegistry() *SourceLeaseRegistry {
	return &SourceLeaseRegistry{streams: make(map[string]map[string]sourceConsumerLease)}
}

type SourceLease struct {
	registry   *SourceLeaseRegistry
	streamID   string
	generation uint64
	consumer   string
	once       sync.Once
}

func (r *SourceLeaseRegistry) Acquire(streamID string, generation uint64, consumer string) *SourceLease {
	lease := &SourceLease{registry: r, streamID: streamID, generation: generation, consumer: consumer}
	if r == nil || streamID == "" || consumer == "" {
		return lease
	}
	r.mu.Lock()
	consumers := r.streams[streamID]
	if consumers == nil {
		consumers = make(map[string]sourceConsumerLease)
		r.streams[streamID] = consumers
	}
	entry := consumers[consumer]
	if entry.generation == generation {
		entry.count++
	} else {
		entry = sourceConsumerLease{generation: generation, count: 1}
	}
	consumers[consumer] = entry
	r.mu.Unlock()
	return lease
}

func (l *SourceLease) Release() error {
	if l == nil || l.registry == nil {
		return nil
	}
	l.once.Do(func() {
		r := l.registry
		r.mu.Lock()
		defer r.mu.Unlock()
		consumers := r.streams[l.streamID]
		entry, exists := consumers[l.consumer]
		if !exists || entry.generation != l.generation {
			return
		}
		entry.count--
		if entry.count > 0 {
			consumers[l.consumer] = entry
			return
		}
		delete(consumers, l.consumer)
		if len(consumers) == 0 {
			delete(r.streams, l.streamID)
		}
	})
	return nil
}

func (r *SourceLeaseRegistry) HasLease(streamID string) bool {
	return r.LeaseCount(streamID) > 0
}

func (r *SourceLeaseRegistry) LeaseCount(streamID string) int {
	if r == nil || streamID == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, entry := range r.streams[streamID] {
		total += entry.count
	}
	return total
}

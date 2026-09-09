package play

import (
	"errors"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

var (
	ErrLivePinned            = errors.New("live generation is pinned by a recorder operation")
	ErrLiveGenerationChanged = errors.New("live generation changed or is not ready")
)

// PinIfCurrent atomically checks the exact generation and prevents coordinator
// teardown until release. Pins are short lived around an external recorder
// write/readback; the recording owner's durable claim and source lease protect
// the longer recording lifetime. Direct ZLM close paths must not bypass the
// coordinator while pins exist. Remote source loss itself cannot be prevented.
func (c *Coordinator) PinIfCurrent(ref stream.LiveRef) (func(), error) {
	if ref.StreamID == "" || ref.SSRC == "" || ref.Generation == 0 {
		return nil, ErrLiveGenerationChanged
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if entry.state != LiveStateReady || !resultMatchesRef(entry.result, ref) {
			continue
		}
		entry.pins++
		var once sync.Once
		return func() { once.Do(func() { c.mu.Lock(); entry.pins--; c.mu.Unlock() }) }, nil
	}
	return nil, ErrLiveGenerationChanged
}

func (s *Service) PinLiveGeneration(ref stream.LiveRef) (func(), error) {
	return s.coordinator().PinIfCurrent(ref)
}

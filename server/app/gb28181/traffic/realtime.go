package traffic

import (
	"sync"
	"time"
)

type RealtimeSnapshot struct {
	DeviceCode                     string    `json:"deviceCode"`
	ChannelCode                    string    `json:"channelCode"`
	NodeID                         int64     `json:"nodeId"`
	Stream                         string    `json:"stream"`
	InProgressUpstreamBytes        uint64    `json:"inProgressUpstreamBytes"`
	UpstreamBytesPerSecond         uint64    `json:"upstreamBytesPerSecond"`
	ReaderCount                    int       `json:"readerCount"`
	EstimatedDownstreamBytesPerSec uint64    `json:"estimatedDownstreamBytesPerSec"`
	SampledAt                      time.Time `json:"sampledAt"`
}

type RealtimeStore struct {
	mu   sync.RWMutex
	ttl  time.Duration
	data map[string]RealtimeSnapshot
}

func NewRealtimeStore(ttl time.Duration) *RealtimeStore {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &RealtimeStore{ttl: ttl, data: make(map[string]RealtimeSnapshot)}
}

func (s *RealtimeStore) Put(snapshot RealtimeSnapshot) {
	if s == nil || snapshot.ChannelCode == "" {
		return
	}
	s.mu.Lock()
	s.data[snapshot.ChannelCode] = snapshot
	s.mu.Unlock()
}

func (s *RealtimeStore) Get(channelCode string, now time.Time) (RealtimeSnapshot, bool) {
	if s == nil {
		return RealtimeSnapshot{}, false
	}
	s.mu.RLock()
	snapshot, ok := s.data[channelCode]
	s.mu.RUnlock()
	if !ok || now.Sub(snapshot.SampledAt) > s.ttl {
		return RealtimeSnapshot{}, false
	}
	return snapshot, true
}

func (s *RealtimeStore) List(deviceCode, channelCode string, now time.Time) []RealtimeSnapshot {
	if s == nil {
		return []RealtimeSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]RealtimeSnapshot, 0)
	for _, snapshot := range s.data {
		if now.Sub(snapshot.SampledAt) > s.ttl || (deviceCode != "" && snapshot.DeviceCode != deviceCode) ||
			(channelCode != "" && snapshot.ChannelCode != channelCode) {
			continue
		}
		result = append(result, snapshot)
	}
	return result
}

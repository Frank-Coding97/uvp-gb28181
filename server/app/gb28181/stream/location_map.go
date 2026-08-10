package stream

import "sync"

// LiveRef identifies one generation of a live stream.
type LiveRef struct {
	StreamID   string
	SSRC       string
	Generation uint64
	NodeID     int64
}

// LocationMap 流位置表:streamID -> current live generation
// 用途:多 ZLM 节点场景下,一条流被 scheduler 分到某节点后绑定到这里;
// BYE / Hook 回调需要找到对应节点才能 closeRtpServer。
//
// 并发安全:RWMutex 保护,1000 goroutine 并发 Bind/Lookup/Unbind 无 race。
type LocationMap struct {
	mu      sync.RWMutex
	streams map[string]LiveRef
}

// NewLocationMap 构造
func NewLocationMap() *LocationMap {
	return &LocationMap{streams: make(map[string]LiveRef)}
}

// Bind 绑定流到节点(同一 streamID 多次 Bind 覆盖,最后写胜出)
func (m *LocationMap) Bind(streamID string, nodeID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.streams[streamID]; ok && current.Generation > 0 {
		return
	}
	m.streams[streamID] = LiveRef{StreamID: streamID, NodeID: nodeID}
}

// BindCurrent installs a generation only when it is newer than the current
// binding. Repeating the exact same binding is idempotent.
func (m *LocationMap) BindCurrent(ref LiveRef) bool {
	if ref.StreamID == "" || ref.SSRC == "" || ref.Generation == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.streams[ref.StreamID]
	if ok {
		if current.Generation > ref.Generation {
			return false
		}
		if current.Generation == ref.Generation {
			return current == ref
		}
	}
	m.streams[ref.StreamID] = ref
	return true
}

// Lookup 查询流所在节点;不存在返 (0, false)
func (m *LocationMap) Lookup(streamID string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ref, ok := m.streams[streamID]
	return ref.NodeID, ok
}

func (m *LocationMap) LookupCurrent(streamID string) (LiveRef, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ref, ok := m.streams[streamID]
	return ref, ok
}

// Unbind 解绑(BYE / 流结束时调,避免内存泄漏)
func (m *LocationMap) Unbind(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, streamID)
}

// UnbindIfCurrent removes exactly one matching live generation.
func (m *LocationMap) UnbindIfCurrent(ref LiveRef) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.streams[ref.StreamID]
	if !ok || current != ref {
		return false
	}
	delete(m.streams, ref.StreamID)
	return true
}

// Size 当前活跃流数(运维/测试用)
func (m *LocationMap) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}

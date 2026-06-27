package stream

import "sync"

// LocationMap 流位置表:streamID → nodeID
// 用途:多 ZLM 节点场景下,一条流被 scheduler 分到某节点后绑定到这里;
// BYE / Hook 回调需要找到对应节点才能 closeRtpServer。
//
// 并发安全:RWMutex 保护,1000 goroutine 并发 Bind/Lookup/Unbind 无 race。
type LocationMap struct {
	mu      sync.RWMutex
	streams map[string]int64
}

// NewLocationMap 构造
func NewLocationMap() *LocationMap {
	return &LocationMap{streams: make(map[string]int64)}
}

// Bind 绑定流到节点(同一 streamID 多次 Bind 覆盖,最后写胜出)
func (m *LocationMap) Bind(streamID string, nodeID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streams[streamID] = nodeID
}

// Lookup 查询流所在节点;不存在返 (0, false)
func (m *LocationMap) Lookup(streamID string) (int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.streams[streamID]
	return id, ok
}

// Unbind 解绑(BYE / 流结束时调,避免内存泄漏)
func (m *LocationMap) Unbind(streamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, streamID)
}

// Size 当前活跃流数(运维/测试用)
func (m *LocationMap) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}

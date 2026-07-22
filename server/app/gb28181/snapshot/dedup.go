package snapshot

import (
	"sync"
	"time"
)

// dedupCache 单机内存幂等:同一 key 在 TTL 内只允许通过一次。
//
// 用于降压 ZLM getSnap —— 同一通道 30s 内多次播放触发时,复用一张快照即可。
// 多实例部署下每个实例独立幂等,极端场景多抓一次可以接受(不加 Redis 依赖)。
type dedupCache struct {
	mu   sync.Mutex
	data map[string]time.Time
	ttl  time.Duration
	now  func() time.Time
}

// newDedup 构造 dedup 缓存
func newDedup(ttl time.Duration) *dedupCache {
	return &dedupCache{
		data: make(map[string]time.Time),
		ttl:  ttl,
		now:  time.Now,
	}
}

// CheckAndMark 原子的 check-then-set:
//   - key 在 TTL 内已被 Mark → 返回 false(阻挡)
//   - 否则记录当前时间戳并返回 true(放行)
func (d *dedupCache) CheckAndMark(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if last, ok := d.data[key]; ok && now.Sub(last) < d.ttl {
		return false
	}
	d.data[key] = now
	return true
}

// evictExpired 清理过期项(可选,定期调,避免 map 无限增长)
func (d *dedupCache) evictExpired() {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for k, t := range d.data {
		if now.Sub(t) >= d.ttl {
			delete(d.data, k)
		}
	}
}

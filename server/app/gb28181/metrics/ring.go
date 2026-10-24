package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Ring 24h × 60s 双精度环形缓冲(每秒一个桶,记 msg 总数 + fail 总数)。
//
// 设计:
//   - 固定 size 桶,环形复用,跨秒时把"该秒之前的旧桶"清零再写
//   - 每桶用 atomic.Int64 累加,Incr 端无锁
//   - Snapshot 端持读锁(slot 移动需要写锁)
type Ring struct {
	mu       sync.RWMutex
	size     int
	buckets  []bucket
	lastSlot int64 // 最近一次写入的 unix 秒
	clock    func() time.Time
}

type bucket struct {
	t    int64 // 该桶代表的 unix 秒,用于读取时判断是否过期
	msg  atomic.Int64
	fail atomic.Int64
}

// Sample 降采样后的一个数据点
type Sample struct {
	T    int64 // 起始 unix 秒
	Msg  int64
	Fail int64
}

// NewRing 创建 size 个秒桶的环形缓冲
func NewRing(size int) *Ring {
	if size <= 0 {
		size = 86400 // 默认 24h
	}
	r := &Ring{
		size:    size,
		buckets: make([]bucket, size),
		clock:   time.Now,
	}
	return r
}

// SetClock 注入时钟(测试用)
func (r *Ring) SetClock(f func() time.Time) {
	r.mu.Lock()
	r.clock = f
	r.mu.Unlock()
}

// Incr 在当前秒桶累加 msg/fail
func (r *Ring) Incr(t time.Time, msg int, fail int) {
	sec := t.Unix()
	r.mu.Lock()
	idx := int(sec % int64(r.size))
	b := &r.buckets[idx]
	if b.t != sec {
		// 该桶代表的是旧秒,清零再用
		b.t = sec
		b.msg.Store(0)
		b.fail.Store(0)
	}
	if sec > r.lastSlot {
		r.lastSlot = sec
	}
	r.mu.Unlock()
	if msg > 0 {
		b.msg.Add(int64(msg))
	}
	if fail > 0 {
		b.fail.Add(int64(fail))
	}
}

// Snapshot 取最近 window 时长内的样本,按 precision 降采样。
// 返回按时间正序的样本数组。precision <= 0 时退化为 1s 精度。
func (r *Ring) Snapshot(window time.Duration, precision time.Duration) []Sample {
	if precision <= 0 {
		precision = time.Second
	}
	if window <= 0 {
		return nil
	}
	r.mu.RLock()
	now := r.clock().Unix()
	winSec := int64(window / time.Second)
	if winSec <= 0 {
		winSec = 1
	}
	precSec := int64(precision / time.Second)
	if precSec <= 0 {
		precSec = 1
	}
	startSec := now - winSec + 1

	// 临时收集 window 内的有效桶(按 unix 秒)
	type secBucket struct {
		t    int64
		msg  int64
		fail int64
	}
	tmp := make(map[int64]secBucket, winSec)
	for sec := startSec; sec <= now; sec++ {
		idx := int(sec % int64(r.size))
		b := &r.buckets[idx]
		if b.t != sec {
			continue // 桶现在写的是别的秒(更早或更晚),视为该秒无数据
		}
		tmp[sec] = secBucket{t: sec, msg: b.msg.Load(), fail: b.fail.Load()}
	}
	r.mu.RUnlock()

	// 按 precision 聚合
	bucketCount := winSec / precSec
	if winSec%precSec != 0 {
		bucketCount++
	}
	out := make([]Sample, 0, bucketCount)
	for i := int64(0); i < bucketCount; i++ {
		bStart := startSec + i*precSec
		bEnd := bStart + precSec
		if bEnd > now+1 {
			bEnd = now + 1
		}
		var sumMsg, sumFail int64
		for sec := bStart; sec < bEnd; sec++ {
			if sb, ok := tmp[sec]; ok {
				sumMsg += sb.msg
				sumFail += sb.fail
			}
		}
		out = append(out, Sample{T: bStart, Msg: sumMsg, Fail: sumFail})
	}
	return out
}

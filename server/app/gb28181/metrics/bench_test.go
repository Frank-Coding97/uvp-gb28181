package metrics

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkAggregator_BeginEnd 测装饰器开销
// AC-P3 / plan §6 R-5:埋点 P99 ΔRT < 1ms,等同 ns/op 远低于 1_000_000
func BenchmarkAggregator_BeginEnd(b *testing.B) {
	a := NewAggregator()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cid := "c" + strconv.Itoa(i)
		a.Begin(Transaction{Kind: TxRegister, CallID: cid, CSeq: "1", StartedAt: time.Unix(1700000000, 0)})
		a.End(cid, "1", 200, true)
	}
}

// BenchmarkAggregator_Snapshot 测快照合成开销
// AC-P3:卡片接口 P95 < 200ms,聚合器 Snapshot 自身远低于该阈值
func BenchmarkAggregator_Snapshot(b *testing.B) {
	a := NewAggregator()
	// 预热:1000 笔事务
	for i := 0; i < 1000; i++ {
		cid := "warm" + strconv.Itoa(i)
		a.Begin(Transaction{Kind: TxKeepalive, CallID: cid, CSeq: "1", StartedAt: time.Now()})
		a.End(cid, "1", 200, true)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.Snapshot(60*time.Minute, time.Minute)
	}
}

// BenchmarkRing_Incr 测 ring 写入开销
func BenchmarkRing_Incr(b *testing.B) {
	r := NewRing(86400)
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Incr(now, 1, 0)
	}
}

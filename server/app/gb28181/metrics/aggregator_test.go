package metrics

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestAgg(now time.Time) *Aggregator {
	return newAggregatorWithClock(func() time.Time { return now })
}

// 把 (Begin, End) 简化成一行
func runTx(a *Aggregator, kind TxKind, callID, cseq string, statusCode int, success bool) {
	a.Begin(Transaction{Kind: kind, CallID: callID, CSeq: cseq, StartedAt: a.clock()})
	a.End(callID, cseq, statusCode, success)
}

// T1.3-U1: 单事务 happy path
func TestAgg_SingleTxSuccess(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	runTx(a, TxRegister, "c1", "1", 200, true)

	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayTotal != 1 {
		t.Errorf("TodayTotal=%d, want 1", snap.TodayTotal)
	}
	reg := snap.Transactions[0]
	if reg.Kind != TxRegister || reg.TodayCount != 1 || reg.SuccessRate != 1.0 {
		t.Errorf("REGISTER stat = %+v, want count=1 rate=1.0", reg)
	}
}

// T1.3-U2: 单事务失败
func TestAgg_SingleTxFail(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	runTx(a, TxRegister, "c1", "1", 401, false)

	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayAbnormal != 1 {
		t.Errorf("Abnormal=%d, want 1", snap.TodayAbnormal)
	}
	reg := snap.Transactions[0]
	if reg.SuccessRate != 0 {
		t.Errorf("rate=%v, want 0", reg.SuccessRate)
	}
}

// T1.3-U3: 配对 map TTL 清理
func TestAgg_PairTTLCleanup(t *testing.T) {
	now := time.Unix(1700000000, 0)
	a := newTestAgg(now)
	a.Begin(Transaction{Kind: TxRegister, CallID: "c1", CSeq: "1", StartedAt: now})
	if a.PairMapSize() != 1 {
		t.Fatalf("pre cleanup size = %d, want 1", a.PairMapSize())
	}
	// 把时钟拨到 31s 之后,触发 TTL 清理(TTL=30s)
	a.SetClock(func() time.Time { return now.Add(31 * time.Second) })
	cleaned := a.CleanupExpiredPairs(30 * time.Second)
	if cleaned != 1 {
		t.Errorf("cleaned=%d, want 1", cleaned)
	}
	if a.PairMapSize() != 0 {
		t.Errorf("post cleanup size = %d, want 0", a.PairMapSize())
	}
}

// T1.3-U4: 配对错配,B 被丢弃,A 仍在 map,不 crash
func TestAgg_MismatchEnd(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	a.Begin(Transaction{Kind: TxRegister, CallID: "A", CSeq: "1", StartedAt: a.clock()})
	a.End("B", "1", 200, true) // 错的 callID
	if a.PairMapSize() != 1 {
		t.Errorf("A should still be in map, size=%d want 1", a.PairMapSize())
	}
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayTotal != 0 {
		t.Errorf("mismatched End should not count, total=%d", snap.TodayTotal)
	}
}

// T1.3-U5: 8 类事务全跑通
func TestAgg_AllEightKinds(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	for i, k := range AllTxKinds {
		runTx(a, k, "c"+strconv.Itoa(i), "1", 200, true)
	}
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayTotal != 8 {
		t.Errorf("total=%d, want 8", snap.TodayTotal)
	}
	for i, st := range snap.Transactions {
		if st.Kind != AllTxKinds[i] {
			t.Errorf("snap.Transactions[%d].Kind=%v, want %v", i, st.Kind, AllTxKinds[i])
		}
		if st.TodayCount != 1 {
			t.Errorf("%v count=%d, want 1", st.Kind, st.TodayCount)
		}
	}
}

// T1.3-U6: 健康度满分
func TestAgg_HealthFullScore(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	for i, k := range []TxKind{TxRegister, TxKeepalive, TxInvite, TxCatalog} {
		runTx(a, k, "c"+strconv.Itoa(i), "1", 200, true)
	}
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.Health != 100.0 {
		t.Errorf("health=%v, want 100", snap.Health)
	}
}

// T1.3-U7: 健康度有失败,可验证
func TestAgg_HealthWithFailure(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	// INVITE 10 次,1 失败,其他三类全 100% 成功
	for i := 0; i < 9; i++ {
		runTx(a, TxInvite, "i"+strconv.Itoa(i), "1", 200, true)
	}
	runTx(a, TxInvite, "i9", "1", 486, false)
	runTx(a, TxRegister, "r1", "1", 200, true)
	runTx(a, TxKeepalive, "k1", "1", 200, true)
	runTx(a, TxCatalog, "ca1", "1", 200, true)

	snap := a.Snapshot(60*time.Second, time.Second)
	// 加权:reg 0.30*1 + kpa 0.35*1 + inv 0.25*0.9 + cat 0.10*1 = 0.975
	// 100 * 0.975 = 97.5 - penalty 0.1 = 97.4
	want := 97.4
	if snap.Health < want-0.5 || snap.Health > want+0.5 {
		t.Errorf("health=%v, want around %v", snap.Health, want)
	}
}

// T1.3-U8: 健康度空数据,返回 sentinel
func TestAgg_HealthEmpty(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.Health != HealthEmpty {
		t.Errorf("health=%v, want HealthEmpty (-1)", snap.Health)
	}
}

// T1.3-U9: 异常扣分封顶
func TestAgg_AbnormalPenaltyCap(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	// 单类 REGISTER 跑 1000 次失败
	for i := 0; i < 1000; i++ {
		runTx(a, TxRegister, "x"+strconv.Itoa(i), "1", 401, false)
	}
	// REGISTER 加权值:0.30 * 0 = 0,归一后 avg = 0 → score = 0
	// score 已经是 0,封顶后还是 0
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.Health < 0 || snap.Health > 0.01 {
		t.Errorf("health=%v, want 0", snap.Health)
	}

	// 再补:REGISTER 100% 成功 1 次 + 1000 异常,验证扣分上限 20
	a2 := newTestAgg(time.Unix(1700000000, 0))
	runTx(a2, TxRegister, "r1", "1", 200, true)
	// 通过 Catalog 跑 1000 次失败凑异常,REGISTER 还是 100%
	for i := 0; i < 1000; i++ {
		runTx(a2, TxCatalog, "c"+strconv.Itoa(i), "1", 408, false)
	}
	snap2 := a2.Snapshot(60*time.Second, time.Second)
	// 加权:REG 0.30*1 + CAT 0.10*0 = 0.30,归一 (0.30/0.40)=0.75 → 75 - 20(封顶) = 55
	if snap2.Health < 54 || snap2.Health > 56 {
		t.Errorf("health=%v, want ~55 (penalty capped at 20)", snap2.Health)
	}
}

// T1.3-U10: SSE 订阅广播
func TestAgg_SubscribeBroadcast(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	ch1, cancel1 := a.Subscribe()
	ch2, cancel2 := a.Subscribe()
	defer cancel1()
	defer cancel2()

	runTx(a, TxRegister, "c1", "1", 200, true)

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Kind != "tx" {
				t.Errorf("subscriber %d got %v, want tx", i, ev.Kind)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d timeout", i)
		}
	}
}

// T1.3-U11: 订阅取消
func TestAgg_SubscribeCancel(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	ch, cancel := a.Subscribe()
	if a.SubscribersCount() != 1 {
		t.Fatalf("subs=%d, want 1", a.SubscribersCount())
	}
	cancel()
	if a.SubscribersCount() != 0 {
		t.Errorf("post-cancel subs=%d, want 0", a.SubscribersCount())
	}
	// chan 应该已关闭(读取返回零值)
	_, ok := <-ch
	if ok {
		t.Errorf("ch should be closed after cancel")
	}
}

// T1.3-U12: 慢消费者不阻塞
func TestAgg_SlowSubscriberDoesNotBlock(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	_, cancel := a.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			runTx(a, TxRegister, "c"+strconv.Itoa(i), "1", 200, true)
		}
		close(done)
	}()
	select {
	case <-done:
		// 通过:即便订阅者不消费,Begin/End 也跑完了
	case <-time.After(2 * time.Second):
		t.Fatal("slow subscriber blocked aggregator")
	}
}

// T1.3-U13: 跨午夜清零
func TestAgg_DayRollover(t *testing.T) {
	day1 := time.Date(2026, 6, 25, 23, 59, 0, 0, time.Local)
	day2 := time.Date(2026, 6, 26, 0, 0, 1, 0, time.Local)
	current := day1
	a := newAggregatorWithClock(func() time.Time { return current })

	runTx(a, TxRegister, "c1", "1", 200, true)
	if a.Snapshot(60*time.Second, time.Second).TodayTotal != 1 {
		t.Fatalf("day1 total != 1")
	}

	// 拨到第二天
	current = day2
	runTx(a, TxRegister, "c2", "1", 200, true)
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayTotal != 1 {
		t.Errorf("day2 total=%d, want 1 (cleared on rollover)", snap.TodayTotal)
	}
}

// 并发 Begin/End
func TestAgg_ConcurrentBeginEnd(t *testing.T) {
	a := newTestAgg(time.Unix(1700000000, 0))
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				cid := "g" + strconv.Itoa(gid) + "i" + strconv.Itoa(i)
				runTx(a, TxKeepalive, cid, "1", 200, true)
			}
		}(g)
	}
	wg.Wait()
	snap := a.Snapshot(60*time.Second, time.Second)
	if snap.TodayTotal != 5000 {
		t.Errorf("concurrent total=%d, want 5000", snap.TodayTotal)
	}
}

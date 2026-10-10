package metrics

import (
	"sync"
	"testing"
	"time"
)

// T1.2-U1: 单秒累加
func TestRing_SingleSecond(t *testing.T) {
	r := NewRing(60)
	now := time.Unix(1700000000, 0)
	r.SetClock(func() time.Time { return now })
	for i := 0; i < 5; i++ {
		r.Incr(now, 1, 0)
	}
	out := r.Snapshot(1*time.Second, 1*time.Second)
	if len(out) != 1 || out[0].Msg != 5 || out[0].Fail != 0 {
		t.Fatalf("got %+v, want one sample msg=5 fail=0", out)
	}
}

// T1.2-U2: 跨秒滑动
func TestRing_CrossSecond(t *testing.T) {
	r := NewRing(60)
	t1 := time.Unix(1700000000, 0)
	t2 := time.Unix(1700000001, 0)
	r.SetClock(func() time.Time { return t2 })
	r.Incr(t1, 10, 0)
	r.Incr(t2, 3, 0)

	out := r.Snapshot(2*time.Second, 1*time.Second)
	if len(out) != 2 {
		t.Fatalf("len(out)=%d, want 2", len(out))
	}
	if out[0].Msg != 10 || out[1].Msg != 3 {
		t.Errorf("samples = %+v, want [10 3]", out)
	}
}

// T1.2-U3: 历史跨越覆盖
func TestRing_OldBucketOverwritten(t *testing.T) {
	r := NewRing(60)
	tEarly := time.Unix(1700000000, 0)
	tNow := time.Unix(1700000000+1500, 0) // +1500 秒,远超 60 桶
	r.SetClock(func() time.Time { return tNow })
	r.Incr(tEarly, 99, 0)
	r.Incr(tNow, 5, 0)

	out := r.Snapshot(60*time.Second, 1*time.Second)
	var totalMsg int64
	for _, s := range out {
		totalMsg += s.Msg
	}
	// 旧桶被覆盖,只剩 tNow 的 5
	if totalMsg != 5 {
		t.Errorf("totalMsg=%d, want 5 (old bucket should be overwritten)", totalMsg)
	}
}

// T1.2-U4: 降采样到 1m
func TestRing_Downsample1Minute(t *testing.T) {
	r := NewRing(120)
	base := time.Unix(1700000000, 0)
	now := base.Add(59 * time.Second)
	r.SetClock(func() time.Time { return now })
	for i := 0; i < 60; i++ {
		r.Incr(base.Add(time.Duration(i)*time.Second), 1, 0)
	}
	out := r.Snapshot(60*time.Second, 60*time.Second)
	if len(out) != 1 {
		t.Fatalf("len(out)=%d, want 1", len(out))
	}
	if out[0].Msg != 60 {
		t.Errorf("downsampled msg=%d, want 60", out[0].Msg)
	}
}

// T1.2-U5: 空 ring 取窗口
func TestRing_EmptySnapshot(t *testing.T) {
	r := NewRing(60)
	now := time.Unix(1700000000, 0)
	r.SetClock(func() time.Time { return now })
	out := r.Snapshot(60*time.Second, 1*time.Second)
	if len(out) != 60 {
		t.Errorf("len(out)=%d, want 60 zero samples", len(out))
	}
	for _, s := range out {
		if s.Msg != 0 || s.Fail != 0 {
			t.Errorf("sample %+v, want zero", s)
		}
	}
}

// T1.2-U6: 并发安全(100 goroutine 各 Incr 1000 次)
func TestRing_ConcurrentSafe(t *testing.T) {
	r := NewRing(10)
	now := time.Unix(1700000000, 0)
	r.SetClock(func() time.Time { return now })

	var wg sync.WaitGroup
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				r.Incr(now, 1, 0)
			}
		}()
	}
	wg.Wait()

	out := r.Snapshot(1*time.Second, 1*time.Second)
	if len(out) != 1 {
		t.Fatalf("len(out)=%d, want 1", len(out))
	}
	if out[0].Msg != 100000 {
		t.Errorf("concurrent msg=%d, want 100000", out[0].Msg)
	}
}

// T1.2-U7: fail 也计数
func TestRing_FailCount(t *testing.T) {
	r := NewRing(60)
	now := time.Unix(1700000000, 0)
	r.SetClock(func() time.Time { return now })
	r.Incr(now, 1, 1)
	r.Incr(now, 1, 0)
	r.Incr(now, 1, 1)
	out := r.Snapshot(1*time.Second, 1*time.Second)
	if out[0].Msg != 3 || out[0].Fail != 2 {
		t.Errorf("got msg=%d fail=%d, want 3/2", out[0].Msg, out[0].Fail)
	}
}

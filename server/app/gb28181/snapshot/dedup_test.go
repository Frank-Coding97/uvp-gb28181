package snapshot

import (
	"testing"
	"time"
)

func TestDedup_FirstCallPasses(t *testing.T) {
	d := newDedup(30 * time.Second)
	if !d.CheckAndMark("did:cid") {
		t.Fatal("首次调用应放行")
	}
}

func TestDedup_SecondCallWithinTTLBlocked(t *testing.T) {
	d := newDedup(30 * time.Second)
	d.CheckAndMark("did:cid")
	if d.CheckAndMark("did:cid") {
		t.Fatal("30s 内二次调用应阻挡")
	}
}

func TestDedup_AfterTTLPasses(t *testing.T) {
	d := newDedup(50 * time.Millisecond)
	d.CheckAndMark("did:cid")
	time.Sleep(60 * time.Millisecond)
	if !d.CheckAndMark("did:cid") {
		t.Fatal("超过 TTL 后应放行")
	}
}

func TestDedup_DifferentKeysIndependent(t *testing.T) {
	d := newDedup(30 * time.Second)
	d.CheckAndMark("did:cid-A")
	if !d.CheckAndMark("did:cid-B") {
		t.Fatal("不同 key 应独立放行")
	}
}

func TestDedup_EvictExpired(t *testing.T) {
	d := newDedup(20 * time.Millisecond)
	d.CheckAndMark("k1")
	d.CheckAndMark("k2")
	time.Sleep(30 * time.Millisecond)
	d.evictExpired()
	d.mu.Lock()
	remaining := len(d.data)
	d.mu.Unlock()
	if remaining != 0 {
		t.Errorf("evictExpired 后应清空,实际剩 %d", remaining)
	}
}

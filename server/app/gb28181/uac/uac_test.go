package uac

import "testing"

// TestSessionManager T5-测1: 会话增删查
func TestSessionManager(t *testing.T) {
	m := NewSessionManager()
	s := &Session{DeviceID: "dev1", ChannelID: "ch1", StreamID: "stream1", State: StateEstablished}
	m.put(s)

	got := m.Get("stream1")
	if got == nil || got.DeviceID != "dev1" {
		t.Fatalf("查会话失败: %v", got)
	}
	m.remove("stream1")
	if m.Get("stream1") != nil {
		t.Error("移除后应查不到")
	}
}

// TestSessionStateValues T5-测2: 状态枚举正确
func TestSessionStateValues(t *testing.T) {
	if StateIdle != 0 || StateInviting != 1 || StateEstablished != 2 || StateBye != 3 {
		t.Error("会话状态枚举值不符预期")
	}
}

// TestSessionManagerConcurrent T5-测3: 并发安全(无 panic/race)
func TestSessionManagerConcurrent(t *testing.T) {
	m := NewSessionManager()
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := string(rune('a' + n))
			m.put(&Session{StreamID: id})
			_ = m.Get(id)
			m.remove(id)
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSessionManagerConditionalGeneration(t *testing.T) {
	m := NewSessionManager()
	current := &Session{StreamID: "fixed", SSRC: "0200000002", Generation: 2, NodeID: 20}
	stale := &Session{StreamID: "fixed", SSRC: "0200000001", Generation: 1, NodeID: 10}
	if !m.PutIfCurrent(current) {
		t.Fatal("current generation should be stored")
	}
	if m.PutIfCurrent(stale) {
		t.Fatal("stale generation must not replace current session")
	}
	if m.PutIfCurrent(&Session{StreamID: "fixed"}) {
		t.Fatal("legacy session must not replace a versioned session")
	}
	got, ok := m.GetCurrent("fixed")
	if !ok || got != current {
		t.Fatalf("current session mismatch: %+v", got)
	}
	if m.RemoveIfCurrent(stale.Ref()) {
		t.Fatal("stale generation must not remove current session")
	}
	if !m.RemoveIfCurrent(current.Ref()) {
		t.Fatal("current generation should be removed")
	}
	if m.RemoveIfCurrent(current.Ref()) {
		t.Fatal("conditional remove must be idempotent")
	}
}

func TestSessionManagerTakeIfCurrent(t *testing.T) {
	m := NewSessionManager()
	current := &Session{StreamID: "fixed", SSRC: "0200000002", Generation: 2, NodeID: 20}
	stale := SessionRef{StreamID: "fixed", SSRC: "0200000001", Generation: 1, NodeID: 10}
	if !m.PutIfCurrent(current) {
		t.Fatal("current generation should be stored")
	}
	if taken, ok := m.TakeIfCurrent(stale); ok || taken != nil {
		t.Fatalf("stale take returned session=%p ok=%v", taken, ok)
	}
	if got, ok := m.GetCurrent("fixed"); !ok || got != current {
		t.Fatalf("stale take changed current session: got=%p ok=%v", got, ok)
	}
	if taken, ok := m.TakeIfCurrent(current.Ref()); !ok || taken != current {
		t.Fatalf("current take returned session=%p ok=%v", taken, ok)
	}
	if taken, ok := m.TakeIfCurrent(current.Ref()); ok || taken != nil {
		t.Fatalf("repeated take returned session=%p ok=%v", taken, ok)
	}
}

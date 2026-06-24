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

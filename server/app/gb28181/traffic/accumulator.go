package traffic

import (
	"errors"
	"fmt"
	"time"
)

type SessionState string

const (
	SessionActive  SessionState = "active"
	SessionSettled SessionState = "settled"
	SessionGap     SessionState = "gap"
)

// Session is the small mutable accounting state used by the accumulator.
// Persistence adapters map it to the durable traffic session model.
type Session struct {
	LastTotalBytes    uint64
	SettledTotalBytes uint64
	State             SessionState
	LastSeenAt        time.Time
	EndedAt           time.Time
	DurationSeconds   int64
}

type Accumulator struct{}

func (Accumulator) ObserveAbsolute(session *Session, absolute uint64, seenAt time.Time) (uint64, bool, error) {
	if session == nil {
		return 0, false, errors.New("traffic session 不能为空")
	}
	if session.State == SessionSettled {
		return 0, false, nil
	}
	delta := uint64(0)
	reset := false
	if absolute >= session.LastTotalBytes {
		delta = absolute - session.LastTotalBytes
	} else {
		// ZLM totalBytes belongs to one media lifecycle. A decrease means the
		// lifecycle changed (or ZLM restarted); never emit a negative delta.
		reset = true
		delta = absolute
	}
	session.LastTotalBytes = absolute
	session.LastSeenAt = seenAt
	if session.State == "" {
		session.State = SessionActive
	}
	return delta, reset, nil
}

func (a Accumulator) SettleAbsolute(session *Session, absolute uint64, endedAt time.Time, durationSeconds int64) (uint64, error) {
	if session == nil {
		return 0, errors.New("traffic session 不能为空")
	}
	if session.State == SessionSettled {
		return 0, nil
	}
	if durationSeconds < 0 {
		return 0, fmt.Errorf("duration 不能为负数: %d", durationSeconds)
	}
	delta, _, err := a.ObserveAbsolute(session, absolute, endedAt)
	if err != nil {
		return 0, err
	}
	session.SettledTotalBytes = session.LastTotalBytes
	session.State = SessionSettled
	session.EndedAt = endedAt
	session.DurationSeconds = durationSeconds
	return delta, nil
}

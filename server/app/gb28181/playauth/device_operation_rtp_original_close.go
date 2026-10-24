package playauth

import (
	"reflect"
	"strings"
	"time"
)

// Each original owner has two fixed, once-only close slots. Results stay in
// the existing step fields; this is dispatch evidence, never device coverage.
type DeviceRTPOriginalCloseCall struct {
	Sequence          int64      `json:"-"`
	DispatchStartedAt time.Time  `json:"-"`
	Outcome           string     `json:"-"`
	LocalQuiescedAt   *time.Time `json:"-"`
}

type rtpOriginalCloseCallWire struct {
	Sequence          int64      `json:"sequence"`
	DispatchStartedAt time.Time  `json:"dispatchStartedAt"`
	Outcome           string     `json:"outcome,omitempty"`
	LocalQuiescedAt   *time.Time `json:"localQuiescedAt,omitempty"`
}

func originalCloseToWire(c *DeviceRTPOriginalCloseCall) *rtpOriginalCloseCallWire {
	if c == nil {
		return nil
	}
	w := rtpOriginalCloseCallWire(*c)
	return &w
}

func (w *rtpOriginalCloseCallWire) call() *DeviceRTPOriginalCloseCall {
	if w == nil {
		return nil
	}
	c := DeviceRTPOriginalCloseCall(*w)
	return &c
}

func validRTPOriginalCloseCalls(s DeviceRTPResourceStep, updated time.Time) bool {
	if s.OriginalCloseCallSequence == 0 {
		return s.ResourceCloseCall == nil && s.IngressCloseCall == nil
	}
	if s.OriginalCloseCallSequence < 1 || s.OriginalCloseCallSequence > 2 || !validIntentID(s.OwnerProcessID) || s.OwnerProcessID == strings.Repeat("0", 32) || !validIntentID(s.OwnerRunID) || s.DispatchStartedAt == nil {
		return false
	}
	count := int64(0)
	var first, second *DeviceRTPOriginalCloseCall
	for _, slot := range []struct {
		c        *DeviceRTPOriginalCloseCall
		result   string
		observed *time.Time
	}{
		{s.ResourceCloseCall, s.ResourceCloseResult, s.ResourceCloseObservedAt},
		{s.IngressCloseCall, s.IngressCloseResult, s.IngressCloseObservedAt},
	} {
		c := slot.c
		if c == nil {
			if slot.result != "" || slot.observed != nil {
				return false
			}
			continue
		}
		count++
		if c.Sequence < 1 || c.Sequence > s.OriginalCloseCallSequence || !canonicalOriginalCloseTime(c.DispatchStartedAt, *s.DispatchStartedAt, updated) {
			return false
		}
		if c.Sequence == 1 {
			if first != nil {
				return false
			}
			first = c
		} else {
			if second != nil {
				return false
			}
			second = c
		}
		if c.Outcome == "" {
			if c.LocalQuiescedAt != nil || slot.result != "" || slot.observed != nil || s.LocalQuiescedAt != nil {
				return false
			}
			continue
		}
		if c.LocalQuiescedAt == nil || !canonicalOriginalCloseTime(*c.LocalQuiescedAt, c.DispatchStartedAt, updated) || (s.LocalQuiescedAt != nil && c.LocalQuiescedAt.After(*s.LocalQuiescedAt)) {
			return false
		}
		switch c.Outcome {
		case rtpCallObserved:
			if slot.result == "" || slot.observed == nil || !canonicalOriginalCloseTime(*slot.observed, c.DispatchStartedAt, updated) || !slot.observed.Equal(*c.LocalQuiescedAt) {
				return false
			}
		case rtpCallUnknown, rtpCallNotInvoked:
			if slot.result != "" || slot.observed != nil {
				return false
			}
		default:
			return false
		}
	}
	return count == s.OriginalCloseCallSequence && first != nil && (second == nil || (first.LocalQuiescedAt != nil && !second.DispatchStartedAt.Before(*first.LocalQuiescedAt)))
}

func canonicalOriginalCloseTime(value, start, end time.Time) bool {
	_, offset := value.Zone()
	return offset == 0 && value.Nanosecond()%1000 == 0 && !value.Before(start) && !value.After(end)
}

// Flush may finish an already persisted slot, never create dispatch evidence.
func originalCloseFactsExtend(old, next *DeviceRTPOriginalCloseCall) bool {
	if old == nil || next == nil {
		return old == nil && next == nil
	}
	candidate := *old
	if candidate.Outcome == "" && candidate.LocalQuiescedAt == nil {
		candidate.Outcome, candidate.LocalQuiescedAt = next.Outcome, next.LocalQuiescedAt
	}
	return reflect.DeepEqual(candidate, *next)
}

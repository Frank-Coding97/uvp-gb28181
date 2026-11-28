package recordquery

import (
	"errors"
	"fmt"
	"time"
)

const WallClockLayout = "2006-01-02T15:04:05"

var ErrInvalidTime = errors.New("invalid-time")

// WallClockError keeps invalid client input distinguishable from other errors.
type WallClockError struct {
	Value  string
	Reason string
}

func (e *WallClockError) Error() string {
	return fmt.Sprintf("%s: %q (%s)", ErrInvalidTime, e.Value, e.Reason)
}

func (e *WallClockError) Unwrap() error { return ErrInvalidTime }

// ParseWallClock interprets an offset-free device wall clock in the configured
// platform location. The round trip rejects times normalized across DST gaps.
func ParseWallClock(value string, location *time.Location) (time.Time, error) {
	if location == nil {
		return time.Time{}, invalidTime(value, "platform location is required")
	}
	parsed, err := time.ParseInLocation(WallClockLayout, value, location)
	if err != nil {
		return time.Time{}, invalidTime(value, "expected YYYY-MM-DDTHH:mm:ss without offset")
	}
	if parsed.In(location).Format(WallClockLayout) != value {
		return time.Time{}, invalidTime(value, "wall clock does not exist in platform location")
	}
	return parsed, nil
}

// FormatMANSCDPWallClock preserves the configured platform wall clock and does
// not serialize through UTC.
func FormatMANSCDPWallClock(value time.Time, location *time.Location) string {
	if location == nil {
		return ""
	}
	return value.In(location).Format(WallClockLayout)
}

// ServerNow serializes the current instant in the platform location with its
// numeric offset for the HTTP options contract.
func ServerNow(value time.Time, location *time.Location) string {
	if location == nil {
		return ""
	}
	return value.In(location).Format(time.RFC3339)
}

func invalidTime(value, reason string) error {
	return &WallClockError{Value: value, Reason: reason}
}

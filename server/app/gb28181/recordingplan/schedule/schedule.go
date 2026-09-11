package schedule

import (
	"fmt"
	"sort"
	"time"
)

const slotsPerDay = 48

var beijingLocation = time.FixedZone("UTC+8", 8*60*60)

// Period is one normalized half-hour range. Weekday uses 1=Monday ... 7=Sunday.
type Period struct {
	Weekday   int
	StartSlot int
	EndSlot   int
}

type Evaluation struct {
	Matched        bool
	NextTransition *time.Time
}

func BeijingLocation() *time.Location { return beijingLocation }

func Normalize(periods []Period, enabled bool) ([]Period, error) {
	if enabled && len(periods) == 0 {
		return nil, fmt.Errorf("启用计划至少需要一个录像时段")
	}
	if len(periods) == 0 {
		return []Period{}, nil
	}

	normalized := append([]Period(nil), periods...)
	for _, period := range normalized {
		if period.Weekday < 1 || period.Weekday > 7 {
			return nil, fmt.Errorf("星期必须在 1 到 7 之间")
		}
		if period.StartSlot < 0 || period.EndSlot > slotsPerDay || period.StartSlot >= period.EndSlot {
			return nil, fmt.Errorf("录像时段必须满足 0 <= start < end <= 48")
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Weekday != normalized[j].Weekday {
			return normalized[i].Weekday < normalized[j].Weekday
		}
		if normalized[i].StartSlot != normalized[j].StartSlot {
			return normalized[i].StartSlot < normalized[j].StartSlot
		}
		return normalized[i].EndSlot < normalized[j].EndSlot
	})

	merged := make([]Period, 0, len(normalized))
	for _, period := range normalized {
		last := len(merged) - 1
		if last >= 0 && merged[last].Weekday == period.Weekday && period.StartSlot <= merged[last].EndSlot {
			if period.EndSlot > merged[last].EndSlot {
				merged[last].EndSlot = period.EndSlot
			}
			continue
		}
		merged = append(merged, period)
	}
	return merged, nil
}

func Evaluate(periods []Period, now time.Time) Evaluation {
	normalized, err := Normalize(periods, false)
	if err != nil {
		return Evaluation{}
	}
	localNow := now.In(beijingLocation)
	result := Evaluation{Matched: matchedAt(normalized, localNow)}
	if len(normalized) == 0 {
		return result
	}

	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, beijingLocation)
	candidates := make([]time.Time, 0, len(normalized)*2)
	seen := make(map[int64]struct{}, len(normalized)*2)
	for offset := 0; offset <= 7; offset++ {
		day := dayStart.AddDate(0, 0, offset)
		weekday := weekdayNumber(day)
		for _, period := range normalized {
			if period.Weekday != weekday {
				continue
			}
			for _, slot := range []int{period.StartSlot, period.EndSlot} {
				candidate := day.Add(time.Duration(slot) * 30 * time.Minute)
				if !candidate.After(localNow) {
					continue
				}
				key := candidate.UnixNano()
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	for _, candidate := range candidates {
		before := matchedAt(normalized, candidate.Add(-time.Nanosecond))
		after := matchedAt(normalized, candidate)
		if before != after {
			next := candidate
			result.NextTransition = &next
			break
		}
	}
	return result
}

func matchedAt(periods []Period, value time.Time) bool {
	local := value.In(beijingLocation)
	weekday := weekdayNumber(local)
	minute := local.Hour()*60 + local.Minute()
	for _, period := range periods {
		if period.Weekday != weekday {
			continue
		}
		if minute >= period.StartSlot*30 && minute < period.EndSlot*30 {
			return true
		}
	}
	return false
}

func weekdayNumber(value time.Time) int {
	if value.Weekday() == time.Sunday {
		return 7
	}
	return int(value.Weekday())
}

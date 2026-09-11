package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func beijingTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, BeijingLocation())
	require.NoError(t, err)
	return parsed
}

func TestNormalizeMergesAdjacentAndOverlappingPeriods(t *testing.T) {
	got, err := Normalize([]Period{
		{Weekday: 1, StartSlot: 16, EndSlot: 20},
		{Weekday: 1, StartSlot: 20, EndSlot: 24},
		{Weekday: 1, StartSlot: 18, EndSlot: 22},
		{Weekday: 7, StartSlot: 47, EndSlot: 48},
		{Weekday: 1, StartSlot: 0, EndSlot: 12},
	}, true)
	require.NoError(t, err)
	require.Equal(t, []Period{
		{Weekday: 1, StartSlot: 0, EndSlot: 12},
		{Weekday: 1, StartSlot: 16, EndSlot: 24},
		{Weekday: 7, StartSlot: 47, EndSlot: 48},
	}, got)
}

func TestNormalizeRejectsInvalidAndEmptyEnabledSchedule(t *testing.T) {
	for _, periods := range [][]Period{
		nil,
		{{Weekday: 0, StartSlot: 0, EndSlot: 1}},
		{{Weekday: 8, StartSlot: 0, EndSlot: 1}},
		{{Weekday: 1, StartSlot: -1, EndSlot: 1}},
		{{Weekday: 1, StartSlot: 1, EndSlot: 49}},
		{{Weekday: 1, StartSlot: 2, EndSlot: 2}},
	} {
		_, err := Normalize(periods, true)
		require.Error(t, err)
	}

	got, err := Normalize(nil, false)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEvaluateUsesHalfOpenBoundariesAndFindsNextTransition(t *testing.T) {
	periods := []Period{{Weekday: 1, StartSlot: 16, EndSlot: 36}}

	before := Evaluate(periods, beijingTime(t, "2026-08-31 07:59"))
	require.False(t, before.Matched)
	require.Equal(t, beijingTime(t, "2026-08-31 08:00"), *before.NextTransition)

	start := Evaluate(periods, beijingTime(t, "2026-08-31 08:00"))
	require.True(t, start.Matched)
	require.Equal(t, beijingTime(t, "2026-08-31 18:00"), *start.NextTransition)

	inside := Evaluate(periods, beijingTime(t, "2026-08-31 17:59"))
	require.True(t, inside.Matched)

	end := Evaluate(periods, beijingTime(t, "2026-08-31 18:00"))
	require.False(t, end.Matched)
	require.Equal(t, beijingTime(t, "2026-09-07 08:00"), *end.NextTransition)
}

func TestEvaluateHandlesSundayToMondaySplitAcrossWeekBoundary(t *testing.T) {
	periods := []Period{
		{Weekday: 7, StartSlot: 47, EndSlot: 48},
		{Weekday: 1, StartSlot: 0, EndSlot: 12},
	}

	result := Evaluate(periods, beijingTime(t, "2026-08-30 23:45"))
	require.True(t, result.Matched)
	require.Equal(t, beijingTime(t, "2026-08-31 06:00"), *result.NextTransition)
}

func TestEvaluateIsIndependentFromInputLocation(t *testing.T) {
	periods := []Period{{Weekday: 1, StartSlot: 16, EndSlot: 36}}
	beijing := beijingTime(t, "2026-08-31 09:00")
	utc := beijing.UTC()

	require.Equal(t, Evaluate(periods, beijing), Evaluate(periods, utc))
}

func TestEvaluateFullWeekHasNoTransition(t *testing.T) {
	periods := make([]Period, 0, 7)
	for day := 1; day <= 7; day++ {
		periods = append(periods, Period{Weekday: day, StartSlot: 0, EndSlot: 48})
	}
	result := Evaluate(periods, beijingTime(t, "2026-08-31 09:00"))
	require.True(t, result.Matched)
	require.Nil(t, result.NextTransition)
}

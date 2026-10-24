package dashboard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveHistoryWindowUsesBoundedBuckets(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, location)
	tests := []struct {
		raw       string
		duration  time.Duration
		bucket    time.Duration
		maxPoints int
	}{
		{HistoryRange1H.String(), time.Hour, time.Minute, 61},
		{HistoryRange24H.String(), 24 * time.Hour, 5 * time.Minute, 289},
		{HistoryRange7D.String(), 7 * 24 * time.Hour, time.Hour, 169},
	}
	for _, test := range tests {
		window, err := ResolveHistoryWindow(test.raw, now, location)
		require.NoError(t, err)
		require.Equal(t, test.duration, window.To.Sub(window.From))
		require.Equal(t, test.bucket, window.Bucket)
		require.Equal(t, test.maxPoints, window.MaxPoints)
		require.Equal(t, "CST", window.Timezone)
	}
}

func TestResolveHistoryWindowDefaultsAndRejectsUnknownRanges(t *testing.T) {
	window, err := ResolveHistoryWindow("", time.Now(), time.UTC)
	require.NoError(t, err)
	require.Equal(t, HistoryRange24H, window.Range)
	_, err = ResolveHistoryWindow("30d", time.Now(), time.UTC)
	require.ErrorContains(t, err, "仅支持")
}

func TestResolveTrafficHistoryWindowRejectsMinuteHistory(t *testing.T) {
	_, err := ResolveTrafficHistoryWindow("1h", time.Now(), time.UTC)
	require.ErrorContains(t, err, "仅支持 24h、7d")
	window, err := ResolveTrafficHistoryWindow("24h", time.Now(), time.UTC)
	require.NoError(t, err)
	require.Equal(t, time.Hour, window.Bucket)
	require.Equal(t, 24, window.MaxPoints)
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, location)
	window, err = ResolveTrafficHistoryWindow("7d", now, location)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 29, 0, 0, 0, 0, location), window.From)
	require.Equal(t, 7, window.MaxPoints)
}

func (value HistoryRange) String() string { return string(value) }

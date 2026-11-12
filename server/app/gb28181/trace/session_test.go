package trace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeriveSessionStateMarksExpiredMissingAndFailed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	expired := deriveSessionState(now.Add(-8*24*time.Hour), 200, 1, 1, now)
	require.False(t, expired.OriginalAvailable)
	require.False(t, expired.Anomaly)

	missing := deriveSessionState(now.Add(-time.Hour), 0, 2, 1, now)
	require.True(t, missing.OriginalAvailable)
	require.True(t, missing.MissingResponse)
	require.True(t, missing.Anomaly)

	failed := deriveSessionState(now.Add(-time.Hour), 500, 1, 1, now)
	require.False(t, failed.MissingResponse)
	require.True(t, failed.Anomaly)
}

package setup

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeStatus_Transitions(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 30, 0, 0, time.UTC)
	status := NewRuntimeStatus()
	status.now = func() time.Time { return now }

	status.MarkUnconfigured()
	require.Equal(t, RuntimeUnconfigured, status.Snapshot().State)
	status.MarkStarting()
	require.Equal(t, RuntimeStarting, status.Snapshot().State)
	status.MarkRunning()
	snapshot := status.Snapshot()
	require.Equal(t, RuntimeRunning, snapshot.State)
	require.Equal(t, now, snapshot.UpdatedAt)
	require.NotNil(t, snapshot.StartedAt)
	require.Equal(t, now, *snapshot.StartedAt)
	require.Empty(t, snapshot.ErrorSummary)
}

func TestRuntimeStatus_ConfigSavedRequiresRestart(t *testing.T) {
	status := NewRuntimeStatus()
	status.MarkRunning()
	startedAt := status.Snapshot().StartedAt
	status.MarkConfigSaved()
	snapshot := status.Snapshot()
	require.Equal(t, RuntimeRestartRequired, snapshot.State)
	require.Equal(t, startedAt, snapshot.StartedAt)
}

func TestRuntimeStatus_RedactsPasswordFromFailure(t *testing.T) {
	status := NewRuntimeStatus()
	status.MarkFailed("bind failed password=Sec12345Aa!! address=0.0.0.0")
	summary := status.Snapshot().ErrorSummary
	require.NotContains(t, summary, "Sec12345Aa!!")
	require.Contains(t, summary, "password=[redacted]")
}

func TestRuntimeStatus_ConcurrentAccess(t *testing.T) {
	status := NewRuntimeStatus()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			status.MarkFailed(fmt.Sprintf("failure-%d", i))
		}(i)
		go func() {
			defer wg.Done()
			_ = status.Snapshot()
		}()
	}
	wg.Wait()
	require.True(t, strings.HasPrefix(status.Snapshot().ErrorSummary, "failure-"))
}

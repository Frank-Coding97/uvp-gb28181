package recording

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDownloadRegistryCreatesOpaqueTaskAndConsumesTicketOnce(t *testing.T) {
	reg := NewDownloadRegistry(DownloadRegistryConfig{Now: func() time.Time { return time.Unix(100, 0).UTC() }})
	created, ticket, err := reg.Create(7, "41")
	require.NoError(t, err)
	require.Len(t, created.TaskID, 43)
	require.Len(t, ticket, 43)
	require.NotContains(t, reg.DebugString(), ticket)

	claimed, _, cancel, err := reg.Claim(created.TaskID, ticket)
	require.NoError(t, err)
	require.Equal(t, DownloadStatusStreaming, claimed.Status)
	require.NotNil(t, cancel)
	_, _, _, err = reg.Claim(created.TaskID, ticket)
	require.ErrorIs(t, err, ErrDownloadTicketInvalid)
}

func TestDownloadRegistryOwnerAndLimits(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	reg := NewDownloadRegistry(DownloadRegistryConfig{Now: func() time.Time { return now }, PerUserStreaming: 2, PerInstanceStreaming: 2})
	first, ticket, err := reg.Create(7, "1")
	require.NoError(t, err)
	_, _, _, err = reg.Claim(first.TaskID, ticket)
	require.NoError(t, err)
	second, ticket, err := reg.Create(7, "2")
	require.NoError(t, err)
	_, _, _, err = reg.Claim(second.TaskID, ticket)
	require.NoError(t, err)
	third, ticket, err := reg.Create(7, "3")
	require.NoError(t, err)
	_, _, _, err = reg.Claim(third.TaskID, ticket)
	require.ErrorIs(t, err, ErrDownloadLimit)

	_, err = reg.Get(first.TaskID, 8)
	require.ErrorIs(t, err, ErrDownloadNotOwner)
	reg.Finish(first.TaskID, DownloadStatusCompleted, "")
	thirdSnapshot, _, _, err := reg.Claim(third.TaskID, ticket)
	require.NoError(t, err)
	require.Equal(t, DownloadStatusStreaming, thirdSnapshot.Status)
}

func TestDownloadRegistryExpiryAndConcurrentClaim(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	reg := NewDownloadRegistry(DownloadRegistryConfig{Now: func() time.Time { return now }, ReadyTTL: time.Minute})
	task, ticket, err := reg.Create(7, "41")
	require.NoError(t, err)
	now = now.Add(2 * time.Minute)
	require.ErrorIs(t, reg.Expire(task.TaskID), ErrDownloadExpired)
	_, _, _, err = reg.Claim(task.TaskID, ticket)
	require.ErrorIs(t, err, ErrDownloadExpired)

	now = time.Unix(100, 0).UTC()
	task, ticket, err = reg.Create(7, "42")
	require.NoError(t, err)
	var wg sync.WaitGroup
	var successes int
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, claimErr := reg.Claim(task.TaskID, ticket); claimErr == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
}

func TestDownloadRegistryProgressNeverExceedsTotal(t *testing.T) {
	reg := NewDownloadRegistry(DownloadRegistryConfig{})
	task, ticket, err := reg.Create(7, "41")
	require.NoError(t, err)
	_, _, _, err = reg.Claim(task.TaskID, ticket)
	require.NoError(t, err)
	reg.SetTotal(task.TaskID, 10)
	reg.AddBytes(task.TaskID, 6)
	reg.AddBytes(task.TaskID, 8)
	snapshot, err := reg.Get(task.TaskID, 7)
	require.NoError(t, err)
	require.EqualValues(t, 10, snapshot.BytesSent)
}

func TestDownloadRegistryCloseCancelsActiveTask(t *testing.T) {
	registry := NewDownloadRegistry(DownloadRegistryConfig{})
	task, ticket, err := registry.Create(7, "41")
	require.NoError(t, err)
	_, taskCtx, _, err := registry.Claim(task.TaskID, ticket)
	require.NoError(t, err)
	registry.Close()
	select {
	case <-taskCtx.Done():
	default:
		t.Fatal("active task context was not cancelled")
	}
	snapshot, err := registry.Get(task.TaskID, 7)
	require.NoError(t, err)
	require.Equal(t, DownloadStatusCancelled, snapshot.Status)
}

func TestDownloadRegistryErrorSentinelsAreStable(t *testing.T) {
	require.True(t, errors.Is(ErrDownloadNotOwner, ErrDownloadNotOwner))
	require.NotEmpty(t, strings.TrimSpace(ErrDownloadTicketInvalid.Error()))
}

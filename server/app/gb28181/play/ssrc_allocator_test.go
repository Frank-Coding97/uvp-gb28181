package play

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRealtimeSSRCAllocatorConcurrentAcquire(t *testing.T) {
	allocator, err := NewRealtimeSSRCAllocator("3402000000")
	require.NoError(t, err)

	const count = 1000
	results := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ssrc, acquireErr := allocator.Acquire()
			if acquireErr != nil {
				errs <- acquireErr
				return
			}
			results <- ssrc
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	require.Empty(t, errs)

	seen := make(map[string]struct{}, count)
	for ssrc := range results {
		require.Len(t, ssrc, 10)
		require.NotContains(t, seen, ssrc)
		seen[ssrc] = struct{}{}
	}
	require.Len(t, seen, count)
}

func TestRealtimeSSRCAllocatorWrapReleaseAndExhaustion(t *testing.T) {
	allocator, err := newRealtimeSSRCAllocator("3402000000", 3)
	require.NoError(t, err)
	first, err := allocator.Acquire()
	require.NoError(t, err)
	second, err := allocator.Acquire()
	require.NoError(t, err)
	third, err := allocator.Acquire()
	require.NoError(t, err)
	require.Equal(t, []string{"0200000000", "0200000001", "0200000002"}, []string{first, second, third})

	_, err = allocator.Acquire()
	require.ErrorIs(t, err, ErrSSRCExhausted)
	allocator.Release(second)
	reused, err := allocator.Acquire()
	require.NoError(t, err)
	require.Equal(t, second, reused)
}

func TestRealtimeSSRCAllocatorReserveRecovery(t *testing.T) {
	allocator, err := newRealtimeSSRCAllocator("3402000000", 3)
	require.NoError(t, err)
	require.NoError(t, allocator.Reserve("0200000001"))

	first, err := allocator.Acquire()
	require.NoError(t, err)
	second, err := allocator.Acquire()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"0200000000", "0200000002"}, []string{first, second})
	_, err = allocator.Acquire()
	require.ErrorIs(t, err, ErrSSRCExhausted)

	require.ErrorIs(t, allocator.Reserve("0200000001"), ErrSSRCInUse)
	require.Error(t, allocator.Reserve("1200000001"))
}

func TestRealtimeSSRCAllocatorRejectsInvalidDomain(t *testing.T) {
	for _, domain := range []string{"", "340200000", "34020000000", "34020x0000"} {
		_, err := NewRealtimeSSRCAllocator(domain)
		require.True(t, errors.Is(err, ErrInvalidSSRCDomain), domain)
	}
}

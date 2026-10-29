package limit

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOpenAPILimitIndependentClients(t *testing.T) {
	now := time.Unix(1790000000, 0)
	m := New(func() time.Time { return now })
	for i := 0; i < 20; i++ {
		_, err := m.Allow(1, 10, 20, false)
		require.NoError(t, err)
	}
	retry, err := m.Allow(1, 10, 20, false)
	require.ErrorIs(t, err, ErrRateLimited)
	require.Equal(t, 100*time.Millisecond, retry)
	_, err = m.Allow(2, 10, 20, false)
	require.NoError(t, err)
	now = now.Add(100 * time.Millisecond)
	_, err = m.Allow(1, 10, 20, false)
	require.NoError(t, err)
	_, err = m.Allow(0, 10, 20, false)
	require.ErrorIs(t, err, ErrInvalidConfiguration)
	_, err = m.Allow(1, 0, 20, false)
	require.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestOpenAPILimitPlayBucketAndIdleExpiry(t *testing.T) {
	now := time.Unix(1790000000, 0)
	m := New(func() time.Time { return now })
	for i := 0; i < 2; i++ {
		_, err := m.Allow(1, 10, 20, true)
		require.NoError(t, err)
	}
	retry, err := m.Allow(1, 10, 20, true)
	require.ErrorIs(t, err, ErrRateLimited)
	require.Equal(t, time.Second, retry)
	_, err = m.Allow(1, 10, 20, false)
	require.NoError(t, err)
	_, err = m.Allow(2, 10, 20, true)
	require.NoError(t, err)
	now = now.Add(11 * time.Minute)
	_, err = m.Allow(3, 10, 20, false)
	require.NoError(t, err)
	require.Len(t, m.clients, 1)
}

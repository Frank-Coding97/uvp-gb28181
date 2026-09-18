package playauth

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceCleanupDiscoveryKeysetAndMalformedRowProgress(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, "malformed", 2, 1)
	f.add(t, 2, cleanupDeviceA, 2, 1)
	f.add(t, 3, cleanupDeviceB, 3, 1)
	f.add(t, 4, cleanupDeviceC, 1, 1)
	f.add(t, 5, "34020000001320000005", 2, 1)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=5").Error)
	s := NewDeviceCleanupStore(f.db)
	ctx := context.Background()
	upper, err := s.PendingUpperBound(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), upper)
	f.add(t, 6, "34020000001320000006", 2, 1)
	p, err := s.DiscoverPending(ctx, 0, upper, 1)
	require.ErrorIs(t, err, ErrDeviceCleanupUnavailable)
	require.Equal(t, int64(1), p.NextPK)
	require.Equal(t, 1, p.Invalid)
	require.Empty(t, p.Targets)
	p, err = s.DiscoverPending(ctx, p.NextPK, upper, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), p.Targets[0].DevicePK)
	require.Equal(t, cleanupDeviceA, p.Targets[0].DeviceID)
	require.Equal(t, int64(2), p.Targets[0].AccessEpoch)
	p, err = s.DiscoverPending(ctx, p.NextPK, upper, 100)
	require.NoError(t, err)
	require.Len(t, p.Targets, 1)
	require.Equal(t, int64(3), p.NextPK)
	p, err = s.DiscoverPending(ctx, p.NextPK, upper, 100)
	require.NoError(t, err)
	require.Zero(t, p.NextPK)
	require.Empty(t, p.Targets)
	state, err := s.Load(ctx, cleanupDeviceA)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
}

func TestDeviceCleanupDiscoveryLimitsAndDependencyFailure(t *testing.T) {
	f := newDeviceCleanupFixture(t)
	for n := int64(1); n <= 103; n++ {
		f.add(t, n, fmt.Sprintf("3402000000132%07d", n), 2, 1)
	}
	s := NewDeviceCleanupStore(f.db)
	ctx := context.Background()
	p, err := s.DiscoverPending(ctx, 0, 103, 10000)
	require.NoError(t, err)
	require.Len(t, p.Targets, 100)
	require.Equal(t, int64(100), p.NextPK)
	p, err = s.DiscoverPending(ctx, p.NextPK, 103, 0)
	require.NoError(t, err)
	require.Len(t, p.Targets, 3)
	_, err = s.DiscoverPending(ctx, -1, 103, 1)
	require.Error(t, err)
	_, err = s.DiscoverPending(nil, 0, 103, 1)
	require.Error(t, err)
	require.NoError(t, f.db.Exec("DROP TABLE gb_device").Error)
	p, err = s.DiscoverPending(ctx, 0, 103, 1)
	require.Error(t, err)
	require.Zero(t, p.NextPK, "query failure must not invent cursor progress")
	require.Empty(t, p.Targets)
}

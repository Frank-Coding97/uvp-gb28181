package trace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type fakePrunableStore struct {
	cutoff time.Time
	batch  int
	count  int64
	err    error
	calls  int
}

func (s *fakePrunableStore) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.cutoff, s.batch, s.calls = cutoff, batchSize, s.calls+1
	return s.count, s.err
}

func TestTracePrunerUsesRetentionCutoffAndOneBoundedBatch(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	store := &fakePrunableStore{count: 500}
	pruner := NewTracePruner(store, 7, 500, func() time.Time { return now })

	result, err := pruner.PruneOnce(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 500, result)
	require.Equal(t, now.Add(-7*24*time.Hour), store.cutoff)
	require.Equal(t, 500, store.batch)
	require.Equal(t, 1, store.calls)
}

func TestTracePrunerPropagatesFailureAndCancellation(t *testing.T) {
	store := &fakePrunableStore{err: errors.New("delete failed")}
	pruner := NewTracePruner(store, 7, 500, time.Now)
	_, err := pruner.PruneOnce(t.Context())
	require.ErrorContains(t, err, "delete failed")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pruner.PruneOnce(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRelationalStorePruneDeletesOnlyOldestBoundedIDs(t *testing.T) {
	db := newRelationalStoreTestDB(t)
	store, err := NewRelationalStore(db)
	require.NoError(t, err)
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{now.Add(-10 * 24 * time.Hour), now.Add(-9 * 24 * time.Hour), now.Add(-time.Hour)} {
		event := testRelationalEvent([]string{
			"019f7a0c-a48d-7ddb-a44d-30a8ab2eed41", "019f7a0c-a48d-7ddb-a44d-30a8ab2eed42", "019f7a0c-a48d-7ddb-a44d-30a8ab2eed43",
		}[i], at, "device", "call", "MESSAGE", 0)
		require.NoError(t, store.InsertBatch(t.Context(), []StoredEvent{event}))
	}

	deleted, err := store.Prune(t.Context(), now.Add(-7*24*time.Hour), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var rows []gbmodels.GbSipTraceMessage
	require.NoError(t, db.Order("occurred_at ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "019f7a0c-a48d-7ddb-a44d-30a8ab2eed42", rows[0].EventID)

	deleted, err = store.Prune(t.Context(), now.Add(-7*24*time.Hour), 500)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	deleted, err = store.Prune(t.Context(), now.Add(-7*24*time.Hour), 500)
	require.NoError(t, err)
	require.Zero(t, deleted)
}

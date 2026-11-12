package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

func TestAlarmDeliveryTargetedClaimAndExpiredCompletion(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, key := range []string{"old", "requested"} {
		_, err := repo.EnqueueAlarmDelivery(context.Background(), &model.GbCascadeAlarmDelivery{DeliveryKey: key, Payload: []byte("alarm"), NextAttemptAt: now})
		require.NoError(t, err)
	}
	claimed, err := repo.ClaimAlarmDeliveries(context.Background(), "token-a", now, time.Second, 1, "requested")
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "requested", claimed[0].DeliveryKey)
	ok, err := repo.CompleteAlarmDelivery(context.Background(), claimed[0].ID, "token-a", now.Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok, "an expired lease cannot commit even before another worker claims it")
	ok, err = repo.CheckpointAlarmDelivery(context.Background(), claimed[0].ID, "token-a", now.Add(time.Second), "stale checkpoint")
	require.NoError(t, err)
	require.False(t, ok, "expired leases cannot overwrite dialog progress")
	recovered, err := repo.ClaimAlarmDeliveries(context.Background(), "token-b", now.Add(time.Second), time.Second, 1, "requested")
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	ok, err = repo.CheckpointAlarmDelivery(context.Background(), claimed[0].ID, "token-a", now, "stale checkpoint")
	require.NoError(t, err)
	require.False(t, ok, "a former owner cannot overwrite the recovered lease's dialog progress")
	ok, err = repo.RetryAlarmDelivery(context.Background(), claimed[0].ID, "token-a", now, now, "late error", 8)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAlarmDeliveryConcurrentClaimsHaveSingleWinner(t *testing.T) {
	repo := newTestRepository(t)
	raw, err := repo.db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	now := time.Now().UTC().Truncate(time.Second)
	_, err = repo.EnqueueAlarmDelivery(context.Background(), &model.GbCascadeAlarmDelivery{DeliveryKey: "one", Payload: []byte("alarm"), NextAttemptAt: now})
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan []model.GbCascadeAlarmDelivery, 8)
	errs := make(chan error, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claimed, err := repo.ClaimAlarmDeliveries(context.Background(), fmt.Sprintf("token-%d", i), now, time.Minute, 1)
			results <- claimed
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	count := 0
	for err := range errs {
		require.NoError(t, err)
	}
	for claimed := range results {
		count += len(claimed)
	}
	require.Equal(t, 1, count)
}

func TestAlarmDeliveryDuplicateInsertErrorRequiresExistingKey(t *testing.T) {
	repo := newTestRepository(t)
	item := &model.GbCascadeAlarmDelivery{DeliveryKey: "original", Payload: []byte("original body"), NextAttemptAt: time.Now()}
	created, err := repo.EnqueueAlarmDelivery(t.Context(), item)
	require.NoError(t, err)
	require.True(t, created)
	// SQL Server's GORM create path ignores a conflict target on a non-primary
	// unique key. Its duplicate-key error is safe only after verifying that key.
	require.NoError(t, repo.db.Callback().Create().Before("gorm:create").Register("test:duplicate", func(db *gorm.DB) { db.AddError(fmt.Errorf("insert rejected")) }))
	created, err = repo.EnqueueAlarmDelivery(t.Context(), &model.GbCascadeAlarmDelivery{DeliveryKey: "original", Payload: []byte("changed")})
	require.NoError(t, err)
	require.False(t, created)
	created, err = repo.EnqueueAlarmDelivery(t.Context(), &model.GbCascadeAlarmDelivery{DeliveryKey: "missing", Payload: []byte("new")})
	require.ErrorContains(t, err, "insert rejected")
	require.False(t, created)
	var got model.GbCascadeAlarmDelivery
	require.NoError(t, repo.db.First(&got, item.ID).Error)
	require.Equal(t, []byte("original body"), got.Payload)
}

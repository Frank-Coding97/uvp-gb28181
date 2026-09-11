package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func setupSchedulerDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSQLiteBaselineRepoDB(t)
}

func TestSchedulerSettingRepo_GetCurrent_NotFound(t *testing.T) {
	db := setupSchedulerDB(t)
	r := repo.NewSchedulerSettingRepo(db)
	s, err := r.GetCurrent(context.Background())
	require.NoError(t, err)
	require.Nil(t, s, "空表应返 nil, nil")
}

func TestSchedulerSettingRepo_UpdateAndGet(t *testing.T) {
	db := setupSchedulerDB(t)
	r := repo.NewSchedulerSettingRepo(db)
	ctx := context.Background()

	require.NoError(t, r.UpdateAlgorithm(ctx, "roundrobin"))

	s, err := r.GetCurrent(ctx)
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, int64(1), s.ID)
	require.Equal(t, "roundrobin", s.Algorithm)
	require.NotZero(t, s.CreatedAt)
	require.NotZero(t, s.UpdatedAt)

	// 切到 weighted
	require.NoError(t, r.UpdateAlgorithm(ctx, "weighted"))
	s2, err := r.GetCurrent(ctx)
	require.NoError(t, err)
	require.Equal(t, "weighted", s2.Algorithm)
}

func TestSchedulerSettingRepo_UpdateAlgorithmPreservesExistingMetadata(t *testing.T) {
	db := setupSchedulerDB(t)
	r := repo.NewSchedulerSettingRepo(db)
	createdAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&repo.SchedulerSetting{
		ID:         1,
		Algorithm:  "roundrobin",
		ConfigJSON: `{"weight":3}`,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}).Error)

	before := time.Now()
	require.NoError(t, r.UpdateAlgorithm(context.Background(), "weighted"))

	var got repo.SchedulerSetting
	require.NoError(t, db.First(&got, 1).Error)
	require.Equal(t, "weighted", got.Algorithm)
	require.Equal(t, `{"weight":3}`, got.ConfigJSON)
	require.True(t, createdAt.Equal(got.CreatedAt), "stored timestamp must preserve the same instant")
	require.GreaterOrEqual(t, got.UpdatedAt, before)
}

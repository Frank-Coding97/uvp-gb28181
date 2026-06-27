package repo_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

func setupSchedulerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&repo.SchedulerSetting{}))
	return db
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

	// 切到 weighted
	require.NoError(t, r.UpdateAlgorithm(ctx, "weighted"))
	s2, err := r.GetCurrent(ctx)
	require.NoError(t, err)
	require.Equal(t, "weighted", s2.Algorithm)
}

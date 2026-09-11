package traffic

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newTrafficSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traffic.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func TestRepositorySQLiteBaselineApplySplitsLocalDays(t *testing.T) {
	db, _ := newTrafficSQLiteBaselineDB(t)
	repo, err := NewGormRepository(db)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	firstAt := time.Date(2026, 9, 7, 15, 59, 0, 0, time.UTC)
	secondAt := firstAt.Add(2 * time.Minute)
	request := ApplyRequest{
		BusinessKey: "sqlite-cross-day", NodeID: 91, ZLMSessionID: "zlm-cross-day",
		Direction: DirectionUpstream, DeviceCode: "sqlite-device", ChannelCode: "sqlite-channel", At: firstAt,
	}
	result, err := repo.Apply(ctx, request.WithAbsolute(100))
	require.NoError(t, err)
	require.EqualValues(t, 100, result.DeltaBytes)
	request.At = secondAt
	result, err = repo.Apply(ctx, request.WithAbsolute(250))
	require.NoError(t, err)
	require.EqualValues(t, 150, result.DeltaBytes)

	var daily []gbmodels.GbDeviceTrafficDaily
	require.NoError(t, db.Order("stat_date").Find(&daily).Error)
	require.Len(t, daily, 2)
	require.Equal(t, "2026-09-07", daily[0].StatDate.In(accountingLocation).Format("2006-01-02"))
	require.EqualValues(t, 100, daily[0].UpstreamBytes)
	require.Equal(t, "2026-09-08", daily[1].StatDate.In(accountingLocation).Format("2006-01-02"))
	require.EqualValues(t, 150, daily[1].UpstreamBytes)

	var hourly []gbmodels.GbDeviceTrafficHourly
	require.NoError(t, db.Order("stat_hour").Find(&hourly).Error)
	require.Len(t, hourly, 2)
}

func TestRepositorySQLiteBaselineOpenGapSerializesNaturalPredicate(t *testing.T) {
	db, path := newTrafficSQLiteBaselineDB(t)
	second, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	rawSecond, err := second.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawSecond.Close() })
	firstRepo, err := NewGormRepository(db)
	require.NoError(t, err)
	secondRepo, err := NewGormRepository(second)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Date(2026, 9, 7, 16, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, repo := range []*GormRepository{firstRepo, secondRepo} {
		wg.Add(1)
		go func(repo *GormRepository) {
			defer wg.Done()
			<-start
			errors <- repo.OpenGap(ctx, 92, "sqlite-test", started)
		}(repo)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceTrafficGap{}).Where("node_id = ? AND reason = ? AND state = ?", 92, "sqlite-test", "open").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRepositorySQLiteBaselineFailureReturnsWithinContext(t *testing.T) {
	db, _ := newTrafficSQLiteBaselineDB(t)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	repo, err := NewGormRepository(db)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err = repo.Apply(ctx, ApplyRequest{
		BusinessKey: "sqlite-failure", NodeID: 93, Direction: DirectionUpstream,
		DeviceCode: "sqlite-device", ChannelCode: "sqlite-channel", At: time.Now().UTC(),
	}.WithAbsolute(1))
	require.Error(t, err)
}

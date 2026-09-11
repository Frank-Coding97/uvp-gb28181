package traffic

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newTrafficTestRepo(t *testing.T) *GormRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTrafficSession{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficHourly{}, &gbmodels.GbDeviceTrafficGap{}))
	repo, err := NewGormRepository(db)
	require.NoError(t, err)
	return repo
}

func TestRepositoryApplyAbsoluteAndSettleOnlyAddsDelta(t *testing.T) {
	repo := newTrafficTestRepo(t)
	base := ApplyRequest{BusinessKey: "sample:7:stream-1:100", NodeID: 7, ZLMSessionID: "zlm-1", Direction: DirectionUpstream, DeviceCode: "device-1", ChannelCode: "channel-1", At: time.Unix(100, 0)}

	result, err := repo.Apply(context.Background(), base.WithAbsolute(100))
	require.NoError(t, err)
	require.EqualValues(t, 100, result.DeltaBytes)

	result, err = repo.Apply(context.Background(), base.WithAbsolute(130))
	require.NoError(t, err)
	require.EqualValues(t, 30, result.DeltaBytes)

	result, err = repo.Apply(context.Background(), base.WithAbsolute(134).Settled(4))
	require.NoError(t, err)
	require.EqualValues(t, 4, result.DeltaBytes)
	require.True(t, result.Settled)

	result, err = repo.Apply(context.Background(), base.WithAbsolute(134).Settled(4))
	require.NoError(t, err)
	require.Zero(t, result.DeltaBytes)

	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 134, daily.UpstreamBytes)
	require.EqualValues(t, 1, daily.UpstreamSessions)
	var hourly gbmodels.GbDeviceTrafficHourly
	require.NoError(t, repo.db.First(&hourly).Error)
	require.EqualValues(t, 134, hourly.UpstreamBytes)
	var session gbmodels.GbDeviceTrafficSession
	require.NoError(t, repo.db.Where("business_key = ?", base.BusinessKey).First(&session).Error)
	require.NotNil(t, session.StartedAt)
}

func TestRepositoryCreatesRowsWhenRecordNotFoundIsMasked(t *testing.T) {
	repo := newTrafficTestRepo(t)
	require.NoError(t, repo.db.Callback().Query().Before("gorm:query").Register("traffic:test_mask_record_not_found", func(tx *gorm.DB) {
		tx.Statement.RaiseErrorOnNotFound = false
	}))

	request := ApplyRequest{
		BusinessKey: "sample:7:stream-masked-not-found:100", NodeID: 7, ZLMSessionID: "zlm-masked-not-found",
		Direction: DirectionUpstream, DeviceCode: "device-1", ChannelCode: "channel-1",
		At: time.Date(2026, 8, 15, 16, 30, 0, 0, time.UTC),
	}
	result, err := repo.Apply(context.Background(), request.WithAbsolute(100))
	require.NoError(t, err)
	require.EqualValues(t, 100, result.DeltaBytes)
	request.At = request.At.Add(time.Minute)
	result, err = repo.Apply(context.Background(), request.WithAbsolute(150))
	require.NoError(t, err)
	require.EqualValues(t, 50, result.DeltaBytes)

	var session gbmodels.GbDeviceTrafficSession
	require.NoError(t, repo.db.Where("business_key = ?", request.BusinessKey).Take(&session).Error)
	require.EqualValues(t, 150, session.LastTotalBytes)
	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 150, daily.UpstreamBytes)
	require.Equal(t, "2026-08-16", daily.StatDate.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02"))

	foundKey, found, err := repo.ActiveBusinessKey(context.Background(), request.NodeID, request.App, "missing-stream", request.Direction)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, foundKey)

	gapAt := time.Unix(200, 0)
	require.NoError(t, repo.OpenGap(context.Background(), request.NodeID, "api_failed", gapAt))
	var gap gbmodels.GbDeviceTrafficGap
	require.NoError(t, repo.db.Where("node_id = ? AND reason = ?", request.NodeID, "api_failed").Take(&gap).Error)
	require.Equal(t, "open", gap.State)
}

func TestRepositorySeparatesDirections(t *testing.T) {
	repo := newTrafficTestRepo(t)
	common := ApplyRequest{NodeID: 7, ZLMSessionID: "session-1", DeviceCode: "device-1", ChannelCode: "channel-1", At: time.Unix(200, 0)}
	_, err := repo.Apply(context.Background(), common.WithDirection(DirectionUpstream).WithAbsolute(10).Settled(1).withBusinessKey("flow:7:session-1:upstream"))
	require.NoError(t, err)
	_, err = repo.Apply(context.Background(), common.WithDirection(DirectionDownstream).WithAbsolute(20).Settled(2).withBusinessKey("flow:7:session-1:downstream"))
	require.NoError(t, err)

	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 10, daily.UpstreamBytes)
	require.EqualValues(t, 20, daily.DownstreamBytes)
}

func TestUpsertDailyPreservesConcurrentDirectionSettlement(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/traffic.db?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTrafficDaily{}))
	rawDB, err := db.DB()
	require.NoError(t, err)
	rawDB.SetMaxOpenConns(2)

	at := time.Date(2026, 8, 16, 1, 0, 0, 0, accountingLocation)
	upstream := ApplyRequest{Direction: DirectionUpstream, DeviceCode: "device-1", ChannelCode: "channel-1", At: at, DurationSeconds: 3}
	downstream := ApplyRequest{Direction: DirectionDownstream, DeviceCode: "device-1", ChannelCode: "channel-1", At: at, DurationSeconds: 5}
	start := make(chan struct{})
	errors := make(chan error, 2)
	go func() {
		<-start
		errors <- upsertDaily(db, upstream, 7, true)
	}()
	go func() {
		<-start
		errors <- upsertDaily(db, downstream, 11, true)
	}()
	close(start)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)

	var rows []gbmodels.GbDeviceTrafficDaily
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.EqualValues(t, 7, rows[0].UpstreamBytes)
	require.EqualValues(t, 11, rows[0].DownstreamBytes)
	require.EqualValues(t, 1, rows[0].UpstreamSessions)
	require.EqualValues(t, 1, rows[0].DownstreamSessions)
	require.EqualValues(t, 3, rows[0].UpstreamDurationSeconds)
	require.EqualValues(t, 5, rows[0].DownstreamDurationSeconds)
}

func TestRepositoryPruneSettledBeforeIsBoundedAndKeepsActiveSessions(t *testing.T) {
	repo := newTrafficTestRepo(t)
	cutoff := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	oldEnd := cutoff.Add(-time.Hour)
	newEnd := cutoff.Add(time.Hour)
	rows := []gbmodels.GbDeviceTrafficSession{
		{BusinessKey: "old-1", DeviceCode: "d", ChannelCode: "c", State: string(SessionSettled), EndedAt: &oldEnd},
		{BusinessKey: "old-2", DeviceCode: "d", ChannelCode: "c", State: string(SessionSettled), EndedAt: &oldEnd},
		{BusinessKey: "new", DeviceCode: "d", ChannelCode: "c", State: string(SessionSettled), EndedAt: &newEnd},
		{BusinessKey: "active", DeviceCode: "d", ChannelCode: "c", State: string(SessionActive), EndedAt: &oldEnd},
	}
	require.NoError(t, repo.db.Create(&rows).Error)

	deleted, err := repo.PruneSettledBefore(context.Background(), cutoff, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	deleted, err = repo.PruneSettledBefore(context.Background(), cutoff, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining []gbmodels.GbDeviceTrafficSession
	require.NoError(t, repo.db.Order("business_key").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.Equal(t, "active", remaining[0].BusinessKey)
	require.Equal(t, "new", remaining[1].BusinessKey)
}

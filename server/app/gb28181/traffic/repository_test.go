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
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTrafficSession{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficGap{}))
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

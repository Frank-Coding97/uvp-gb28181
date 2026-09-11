package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPlayAttemptCountsOnlyMediaReadySuccessAndTerminalFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPlayAttempt{}))
	now := time.Date(2026, 9, 2, 13, 30, 0, 0, time.UTC)
	store := NewPlayAttemptStore(db)
	store.SetClock(func() time.Time { return now })
	successID, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Finish(context.Background(), successID, PlayOutcomeSuccess, "", 2, false))
	failureID, err := store.Begin(context.Background(), 7, "D1", "C2")
	require.NoError(t, err)
	require.NoError(t, store.Finish(context.Background(), failureID, PlayOutcomeFailure, "media_ready", 2, false))
	_, err = store.Begin(context.Background(), 7, "D1", "C3")
	require.NoError(t, err)

	result, err := store.Last24Hours(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Attempts)
	require.EqualValues(t, 1, result.Success)
	require.EqualValues(t, 1, result.Failure)
	require.NotNil(t, result.Rate)
	require.Equal(t, 0.5, *result.Rate)
}

func TestPlayAttemptSummaryReappliesCurrentDeviceScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPlayAttempt{}))
	now := time.Date(2026, 9, 2, 13, 30, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "D1", OwnerDeptID: 10},
		{DeviceID: "D2", OwnerDeptID: 20},
	}).Error)
	require.NoError(t, db.Create(&[]gbmodels.GbPlayAttempt{
		{CorrelationID: "visible-success", DeviceCode: "D1", Outcome: PlayOutcomeSuccess, StartedAt: now},
		{CorrelationID: "hidden-failure", DeviceCode: "D2", Outcome: PlayOutcomeFailure, StartedAt: now},
	}).Error)

	result, err := NewPlayAttemptStore(db).Last24HoursScoped(context.Background(), now, func(query *gorm.DB) *gorm.DB {
		return query.Where("gb_device.owner_dept_id = ?", 10)
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Attempts)
	require.EqualValues(t, 1, result.Success)
	require.Zero(t, result.Failure)
}

func TestPlayAttemptSummaryMarksStaleStartedAttemptAsPartial(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPlayAttempt{}))
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]gbmodels.GbPlayAttempt{
		{CorrelationID: "success", DeviceCode: "D1", ChannelCode: "C1", Outcome: PlayOutcomeSuccess, StartedAt: now.Add(-time.Minute)},
		{CorrelationID: "current", DeviceCode: "D1", ChannelCode: "C2", Outcome: PlayOutcomeStarted, StartedAt: now.Add(-time.Minute)},
		{CorrelationID: "stale", DeviceCode: "D1", ChannelCode: "C3", Outcome: PlayOutcomeStarted, StartedAt: now.Add(-10 * time.Minute)},
	}).Error)

	result, err := NewPlayAttemptStore(db).Last24Hours(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Attempts)
	require.EqualValues(t, 2, result.Started)
	require.EqualValues(t, 1, result.StaleStarted)
	require.Equal(t, StatusPartial, result.Status)
	require.Equal(t, CoveragePartial, result.Coverage)
}

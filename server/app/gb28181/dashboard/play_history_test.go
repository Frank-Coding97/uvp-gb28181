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

func TestPlayHistoryUsesTerminalDenominatorAndVisibilityScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPlayAttempt{}))
	location := time.UTC
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, location)
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "visible", OwnerDeptID: 10},
		{DeviceID: "hidden", OwnerDeptID: 20},
	}).Error)
	finished := now.Add(-time.Minute)
	require.NoError(t, db.Create(&[]gbmodels.GbPlayAttempt{
		{CorrelationID: "s1", DeviceCode: "visible", Outcome: PlayOutcomeSuccess, Reused: true, StartedAt: now.Add(-3 * time.Minute), FinishedAt: &finished},
		{CorrelationID: "f1", DeviceCode: "visible", Outcome: PlayOutcomeFailure, FailureStage: "invite", StartedAt: now.Add(-2 * time.Minute), FinishedAt: &finished},
		{CorrelationID: "p1", DeviceCode: "visible", Outcome: PlayOutcomeStarted, StartedAt: now.Add(-time.Minute)},
		{CorrelationID: "hidden", DeviceCode: "hidden", Outcome: PlayOutcomeSuccess, StartedAt: now.Add(-time.Minute), FinishedAt: &finished},
	}).Error)
	window, err := ResolveHistoryWindow("1h", now, location)
	require.NoError(t, err)
	history, err := NewPlayAttemptStore(db).HistoryScoped(context.Background(), window, func(query *gorm.DB) *gorm.DB {
		return query.Where("gb_device.owner_dept_id = ?", 10)
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, history.Summary.Attempts)
	require.EqualValues(t, 1, history.Summary.Success)
	require.EqualValues(t, 1, history.Summary.Failure)
	require.EqualValues(t, 1, history.Summary.Started)
	require.NotNil(t, history.Summary.Rate)
	require.Equal(t, 0.5, *history.Summary.Rate)
	require.Len(t, history.FailureStages, 1)
	require.Equal(t, "invite", history.FailureStages[0].Key)
	require.ElementsMatch(t, []CountDistribution{{Key: "new", Count: 1, Rate: 0.5}, {Key: "reused", Count: 1, Rate: 0.5}}, history.Reuse)
	require.LessOrEqual(t, len(history.Points), window.MaxPoints)
}

func TestPlayHistoryDoesNotInventRateWithoutTerminalAttempts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPlayAttempt{}))
	now := time.Date(2026, 9, 4, 10, 3, 0, 0, time.UTC)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "visible", OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPlayAttempt{CorrelationID: "pending", DeviceCode: "visible", Outcome: PlayOutcomeStarted, StartedAt: now.Add(-time.Minute)}).Error)
	window, err := ResolveHistoryWindow("1h", now, time.UTC)
	require.NoError(t, err)
	history, err := NewPlayAttemptStore(db).HistoryScoped(context.Background(), window, nil)
	require.NoError(t, err)
	require.Nil(t, history.Summary.Rate)
	require.EqualValues(t, 1, history.Summary.Started)
	require.Equal(t, StatusOK, history.Status)
}

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

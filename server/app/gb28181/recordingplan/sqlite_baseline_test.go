package recordingplan

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newRecordingPlanSQLiteBaselineDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "recording-plan.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db
}

func TestRepositorySQLiteBaselineGapOpenAndCloseUsesRowsAffected(t *testing.T) {
	db := newRecordingPlanSQLiteBaselineDB(t)
	repo := NewRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC)
	first, err := repo.OpenGap(ctx, nil, 811, "DEVICE_OFFLINE", "offline", started)
	require.NoError(t, err)
	require.NotZero(t, first.ID)
	second, err := repo.OpenGap(ctx, nil, 811, "DEVICE_OFFLINE", "offline", started.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	closed, err := repo.CloseOpenGap(ctx, 811, started.Add(3500*time.Millisecond), nil)
	require.NoError(t, err)
	require.True(t, closed)

	var gap models.GbRecordingPlanGap
	require.NoError(t, db.First(&gap, first.ID).Error)
	require.True(t, gap.Recovered)
	require.EqualValues(t, 3500, gap.DurationMs)
}

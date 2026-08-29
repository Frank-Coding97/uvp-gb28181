package recordingplan

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/recording-plan.db?_pragma=busy_timeout(5000)"), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.GbRecordingPlan{}, &models.GbRecordingPlanPeriod{}, &models.GbRecordingPlanBinding{},
		&models.GbRecordingPlanChannelState{}, &models.GbRecordingPlanExecution{}, &models.GbRecordingPlanGap{},
	))
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(4)
	return db
}

func TestRepositoryCreatePlanRollsBackWhenPeriodInsertFails(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:reject_period", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_recording_plan_period" {
			tx.AddError(errors.New("injected period failure"))
		}
	}))
	repo := NewRepository(db)
	plan := &models.GbRecordingPlan{Name: "工作日", OwnerDeptID: 1}
	err := repo.CreatePlan(context.Background(), plan, []models.GbRecordingPlanPeriod{{Weekday: 1, StartSlot: 0, EndSlot: 2}})
	require.ErrorContains(t, err, "injected period failure")
	var count int64
	require.NoError(t, db.Model(&models.GbRecordingPlan{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestRepositoryLeaseIsExclusiveAndCanBeTakenOverAfterExpiry(t *testing.T) {
	db := newRepositoryTestDB(t)
	now := time.Date(2026, 8, 29, 17, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: 9, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateIdle, ReconcileAt: now.Add(-time.Second)}).Error)
	repo := NewRepository(db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	claimed := make(chan []models.GbRecordingPlanChannelState, 2)
	for _, owner := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			<-start
			rows, _ := repo.ClaimDueStates(context.Background(), owner, now, time.Minute, 10)
			claimed <- rows
		}(owner)
	}
	close(start)
	wg.Wait()
	close(claimed)
	total := 0
	for rows := range claimed {
		total += len(rows)
	}
	require.Equal(t, 1, total)

	rows, err := repo.ClaimDueStates(context.Background(), "worker-c", now.Add(2*time.Minute), time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "worker-c", rows[0].LeaseOwner)
}

func TestRepositoryCASRejectsStalePlanOrStateVersion(t *testing.T) {
	db := newRepositoryTestDB(t)
	repo := NewRepository(db)
	state := models.GbRecordingPlanChannelState{ChannelID: 2, PlanVersion: 3, StateVersion: 4, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateIdle, ReconcileAt: time.Now()}
	require.NoError(t, db.Create(&state).Error)

	ok, err := repo.UpdateStateCAS(context.Background(), 2, 2, 4, map[string]any{"actual_state": models.RecordingStateRecording})
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.UpdateStateCAS(context.Background(), 2, 3, 4, map[string]any{"actual_state": models.RecordingStateRecording})
	require.NoError(t, err)
	require.True(t, ok)
	var stored models.GbRecordingPlanChannelState
	require.NoError(t, db.First(&stored, "channel_id = ?", 2).Error)
	require.Equal(t, models.RecordingStateRecording, stored.ActualState)
	require.EqualValues(t, 5, stored.StateVersion)
}

func TestRepositoryGapOpenIsIdempotentAndCloseComputesDuration(t *testing.T) {
	db := newRepositoryTestDB(t)
	repo := NewRepository(db)
	start := time.Date(2026, 8, 29, 17, 0, 0, 0, time.UTC)
	first, err := repo.OpenGap(context.Background(), 1, 8, "DEVICE_OFFLINE", "设备离线", start)
	require.NoError(t, err)
	second, err := repo.OpenGap(context.Background(), 1, 8, "DEVICE_OFFLINE", "设备离线", start.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	closed, err := repo.CloseOpenGap(context.Background(), 8, start.Add(3500*time.Millisecond), nil)
	require.NoError(t, err)
	require.True(t, closed)
	var gap models.GbRecordingPlanGap
	require.NoError(t, db.First(&gap, first.ID).Error)
	require.True(t, gap.Recovered)
	require.EqualValues(t, 3500, gap.DurationMs)
}

func TestRepositoryRejectsSecondPlanBindingForChannel(t *testing.T) {
	db := newRepositoryTestDB(t)
	repo := NewRepository(db)
	now := time.Now()
	require.NoError(t, repo.BindChannels(context.Background(), 1, 7, 9, []uint{42}, now))
	err := repo.BindChannels(context.Background(), 2, 7, 9, []uint{42}, now)
	require.ErrorIs(t, err, ErrChannelAlreadyBound)
}

func TestRepositoryClaimDueStatesHonorsBatchLimit(t *testing.T) {
	db := newRepositoryTestDB(t)
	repo := NewRepository(db)
	now := time.Now()
	for i := uint(1); i <= 50; i++ {
		require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: i, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateIdle, ReconcileAt: now.Add(-time.Minute)}).Error)
	}
	rows, err := repo.ClaimDueStates(context.Background(), "bounded", now, time.Minute, 7)
	require.NoError(t, err)
	require.Len(t, rows, 7)
}

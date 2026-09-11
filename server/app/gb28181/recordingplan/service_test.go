package recordingplan

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

func TestServiceCreateNormalizesAndReadsPlan(t *testing.T) {
	db := newRepositoryTestDB(t)
	service := NewService(db)
	created, err := service.Create(context.Background(), 7, 9, PlanInput{
		Name: " 工作日 ", Enabled: true,
		Periods: []schedule.Period{{Weekday: 1, StartSlot: 16, EndSlot: 20}, {Weekday: 1, StartSlot: 20, EndSlot: 36}},
	})
	require.NoError(t, err)
	require.Equal(t, "工作日", created.Name)
	require.Equal(t, []schedule.Period{{Weekday: 1, StartSlot: 16, EndSlot: 36}}, created.Periods)

	loaded, err := service.Get(context.Background(), 7, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.Periods, loaded.Periods)
}

func TestServiceRejectsInvalidEnabledSchedule(t *testing.T) {
	service := NewService(newRepositoryTestDB(t))
	for _, input := range []PlanInput{
		{Name: "空时段", Enabled: true},
		{Name: "非法", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 3, EndSlot: 3}}},
		{Name: " ", Enabled: false},
	} {
		_, err := service.Create(context.Background(), 1, 1, input)
		require.Error(t, err)
	}
}

func TestServiceUpdateBumpsVersionAndSchedulesBoundChannelsNow(t *testing.T) {
	db := newRepositoryTestDB(t)
	service := NewService(db)
	now := time.Date(2026, 8, 29, 17, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, err := service.Create(context.Background(), 1, 2, PlanInput{Name: "原计划", Enabled: true, Periods: []schedule.Period{{Weekday: 6, StartSlot: 0, EndSlot: 1}}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GbRecordingPlanBinding{PlanID: created.ID, ChannelID: 5, OwnerDeptID: 1, AssignedAt: now}).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: 5, PlanID: &created.ID, PlanVersion: created.Version, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateRecording, ReconcileAt: now.Add(time.Hour)}).Error)

	updated, err := service.Update(context.Background(), 1, 2, created.ID, PlanInput{Name: "新计划", Enabled: true, Periods: []schedule.Period{{Weekday: 6, StartSlot: 2, EndSlot: 3}}})
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Version)
	var state models.GbRecordingPlanChannelState
	require.NoError(t, db.First(&state, "channel_id = ?", 5).Error)
	require.EqualValues(t, 2, state.PlanVersion)
	require.True(t, now.Equal(state.ReconcileAt))
}

func TestServiceDeleteRejectsBoundPlanAndDeletesUnboundPlan(t *testing.T) {
	db := newRepositoryTestDB(t)
	service := NewService(db)
	bound, err := service.Create(context.Background(), 1, 1, PlanInput{Name: "绑定", Enabled: false})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GbRecordingPlanBinding{PlanID: bound.ID, ChannelID: 1, OwnerDeptID: 1, AssignedAt: time.Now()}).Error)
	err = service.Delete(context.Background(), 1, bound.ID)
	require.ErrorIs(t, err, ErrPlanHasBindings)
	var domainErr *DomainError
	require.True(t, errors.As(err, &domainErr))
	require.Equal(t, 1, domainErr.Details["channelCount"])

	unbound, err := service.Create(context.Background(), 1, 1, PlanInput{Name: "未绑定", Enabled: false})
	require.NoError(t, err)
	require.NoError(t, service.Delete(context.Background(), 1, unbound.ID))
	_, err = service.Get(context.Background(), 1, unbound.ID)
	require.ErrorIs(t, err, ErrPlanNotFound)
}

func TestServicePageIsDepartmentScopedAndBounded(t *testing.T) {
	db := newRepositoryTestDB(t)
	service := NewService(db)
	for i := 0; i < 4; i++ {
		_, err := service.Create(context.Background(), 1, 1, PlanInput{Name: "计划" + string(rune('A'+i)), Enabled: false})
		require.NoError(t, err)
	}
	_, err := service.Create(context.Background(), 2, 1, PlanInput{Name: "其他部门", Enabled: false})
	require.NoError(t, err)
	rows, total, err := service.Page(context.Background(), 1, "计划", 2, 2)
	require.NoError(t, err)
	require.EqualValues(t, 4, total)
	require.Len(t, rows, 2)
}

func TestServicePageSummariesFiltersStatusAndCountsBindings(t *testing.T) {
	db := newRepositoryTestDB(t)
	service := NewService(db)
	enabled, err := service.Create(context.Background(), 1, 1, PlanInput{Name: "启用计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 1}}})
	require.NoError(t, err)
	_, err = service.Create(context.Background(), 1, 1, PlanInput{Name: "停用计划", Enabled: false})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GbRecordingPlanBinding{PlanID: enabled.ID, ChannelID: 9, OwnerDeptID: 1, AssignedAt: time.Now()}).Error)
	filter := true
	rows, total, err := service.PageSummaries(context.Background(), 1, "计划", &filter, 1, 500)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Enabled)
	require.EqualValues(t, 1, rows[0].ChannelCount)
}

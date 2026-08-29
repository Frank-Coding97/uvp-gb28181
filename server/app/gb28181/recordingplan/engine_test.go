package recordingplan

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

func TestEngineRecoversInsideScheduleAndKeepsOutsideStopped(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}))
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	plan := models.GbRecordingPlan{Name: "上午", Status: 1, Version: 1, OwnerDeptID: 1}
	require.NoError(t, db.Create(&plan).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanPeriod{PlanID: plan.ID, Weekday: 1, StartSlot: 16, EndSlot: 20}).Error)
	inside := models.GbChannel{DeviceID: "D", ChannelID: "C1", OwnerDeptID: 1, Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeScheduled}
	outside := models.GbChannel{DeviceID: "D", ChannelID: "C2", OwnerDeptID: 1, Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeScheduled}
	require.NoError(t, db.Create(&inside).Error)
	require.NoError(t, db.Create(&outside).Error)
	require.NoError(t, db.Create(&[]models.GbRecordingPlanBinding{{PlanID: plan.ID, ChannelID: inside.ID, OwnerDeptID: 1, AssignedAt: now}, {PlanID: plan.ID, ChannelID: outside.ID, OwnerDeptID: 1, AssignedAt: now}}).Error)
	for _, channel := range []models.GbChannel{inside, outside} {
		require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, PlanID: &plan.ID, PlanVersion: 1, DesiredState: models.RecordingDesiredIdle, ActualState: models.RecordingStateIdle, ReconcileAt: now.Add(-time.Second)}).Error)
	}
	operator := &fakeChannelOperator{}
	engine := NewEngine(db, operator, EngineOptions{InstanceID: "test", BatchSize: 10, Now: func() time.Time { return now }})
	require.NoError(t, engine.Dispatch(context.Background()))
	require.Equal(t, []uint{inside.ID, outside.ID}, operator.started, "both share the same currently matching plan")

	now = now.Add(2 * time.Hour)
	require.NoError(t, engine.Heal(context.Background()))
	require.ElementsMatch(t, []uint{inside.ID, outside.ID}, operator.stopped)
}

func TestEngineDisabledDoesNotPullStreams(t *testing.T) {
	db := newRepositoryTestDB(t)
	operator := &fakeChannelOperator{}
	engine := NewEngine(db, operator, EngineOptions{Enabled: boolPointer(false)})
	require.NoError(t, engine.Heal(context.Background()))
	require.NoError(t, engine.Dispatch(context.Background()))
	require.Empty(t, operator.started)
}

type fakeChannelOperator struct{ started, stopped []uint }

func (f *fakeChannelOperator) Start(_ context.Context, target ChannelTarget) (*play.Result, error) {
	f.started = append(f.started, target.ID)
	return &play.Result{StreamID: target.ChannelCode, SSRC: "1", Generation: 1}, nil
}
func (f *fakeChannelOperator) Stop(_ context.Context, channelID uint) error {
	f.stopped = append(f.stopped, channelID)
	return nil
}

func boolPointer(value bool) *bool { return &value }

package recordingplan

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

func newDiagnosticService(t *testing.T) (*DiagnosticService, *gorm.DB) {
	t.Helper()
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}, &models.GbDeviceStatusEvent{}, &models.GbRecordingSession{}, &models.GbRecordingFile{}))
	return NewDiagnosticService(db), db
}

func TestDiagnosticPlanChannelsAreScopedFilteredAndPaginated(t *testing.T) {
	service, db := newDiagnosticService(t)
	plan := models.GbRecordingPlan{Name: "工作日", Status: 1, Version: 1, OwnerDeptID: 10}
	require.NoError(t, db.Create(&plan).Error)
	for i, state := range []string{models.RecordingStateRecording, models.RecordingStateWaitingDevice, models.RecordingStateRecording} {
		dept := uint(10)
		if i == 2 {
			dept = 20
		}
		channel := models.GbChannel{DeviceID: "D", ChannelID: "C" + string(rune('1'+i)), Name: "仓库", OwnerDeptID: dept}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&models.GbRecordingPlanBinding{PlanID: plan.ID, ChannelID: channel.ID, OwnerDeptID: dept, AssignedAt: time.Now()}).Error)
		require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, PlanID: &plan.ID, DesiredState: models.RecordingDesiredRecording, ActualState: state, ReasonCode: ReasonDeviceOffline, ReconcileAt: time.Now()}).Error)
	}

	page, err := service.PagePlanChannels(context.Background(), 10, plan.ID, ChannelStatusQuery{ActualState: models.RecordingStateWaitingDevice, Page: 1, PageSize: 500})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, 100, page.PageSize)
	require.Len(t, page.List, 1)
	require.Equal(t, models.RecordingStateWaitingDevice, page.List[0].ActualState)
	require.EqualValues(t, 1, page.StatusCounts[models.RecordingStateWaitingDevice])
}

func TestDiagnosticExplainsMissingRecordingByEvidenceLayer(t *testing.T) {
	service, db := newDiagnosticService(t)
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, schedule.BeijingLocation())
	service.now = func() time.Time { return now }
	plan := models.GbRecordingPlan{Name: "全天", Status: 1, Version: 1, OwnerDeptID: 1}
	require.NoError(t, db.Create(&plan).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanPeriod{PlanID: plan.ID, Weekday: 1, StartSlot: 0, EndSlot: 48}).Error)
	device := models.GbDevice{DeviceID: "D1", Name: "门口设备", OwnerDeptID: 1, Status: models.DeviceStatusOffline}
	require.NoError(t, db.Create(&device).Error)
	channel := models.GbChannel{DeviceID: "D1", ChannelID: "C1", Name: "门口", OwnerDeptID: 1, Status: models.ChannelStatusOffline, RecordingMode: models.RecordingModeScheduled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanBinding{PlanID: plan.ID, ChannelID: channel.ID, OwnerDeptID: 1, AssignedAt: now}).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, PlanID: &plan.ID, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateWaitingDevice, ReasonCode: ReasonDeviceOffline, ReasonMessage: "心跳超时", ReconcileAt: now, AttemptCount: 2}).Error)
	require.NoError(t, db.Create(&models.GbDeviceStatusEvent{DeviceID: device.ID, DeviceCode: "D1", EventType: models.DeviceEventHeartbeatTimeout, ToStatus: 0, OccurredAt: now.Add(-time.Minute), Source: models.DeviceEventSourceOfflineScanner}).Error)

	diagnosis, err := service.DiagnoseChannel(context.Background(), 1, channel.ID)
	require.NoError(t, err)
	require.True(t, diagnosis.Schedule.Matched)
	require.False(t, diagnosis.Device.Online)
	require.Equal(t, ReasonDeviceOffline, diagnosis.Conclusion.Code)
	require.Equal(t, models.DeviceEventHeartbeatTimeout, diagnosis.Device.LastEvent.EventType)
	require.Equal(t, 2, diagnosis.Retry.AttemptCount)
}

func TestDiagnosticFlagsMissingFileWithoutExposingStoragePath(t *testing.T) {
	service, db := newDiagnosticService(t)
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, schedule.BeijingLocation())
	service.now = func() time.Time { return now }
	channel := models.GbChannel{DeviceID: "D1", ChannelID: "C1", OwnerDeptID: 1, Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeContinuous}
	require.NoError(t, db.Create(&channel).Error)
	started := now.Add(-70 * time.Minute)
	session := models.GbRecordingSession{ChannelID: channel.ID, DeviceID: "D1", NodeID: 1, VHost: "hidden", App: "rtp", Stream: "secret-stream", State: models.RecordingSessionStateRecording, StartedAt: &started}
	require.NoError(t, db.Create(&session).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateRecording, ReconcileAt: now, StreamID: "secret-stream", RecordingSessionID: &session.ID}).Error)

	diagnosis, err := service.DiagnoseChannel(context.Background(), 1, channel.ID)
	require.NoError(t, err)
	require.Equal(t, "FILE_NOT_GENERATED", diagnosis.File.Code)
	require.NotContains(t, diagnosis.File.Message, "secret-stream")
	require.Empty(t, diagnosis.Media.StreamID)
}

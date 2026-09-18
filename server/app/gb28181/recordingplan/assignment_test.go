package recordingplan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

func newAssignmentService(t *testing.T) (*AssignmentService, *Service) {
	t.Helper()
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}))
	return NewAssignmentService(db), NewService(db)
}

func TestAssignByDeviceExpandsOnlyCurrentVisibleChannels(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "全天", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	device := models.GbDevice{DeviceID: "D1", Name: "一号设备", OwnerDeptID: 1}
	require.NoError(t, assignment.db.Create(&device).Error)
	for i := 0; i < 100; i++ {
		require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: fmt.Sprintf("C%03d", i), Name: "通道", OwnerDeptID: 1}).Error)
	}
	result, err := assignment.Assign(context.Background(), 1, 9, plan.ID, AssignmentSelection{Type: SelectionByDevice, IDs: []uint{device.ID}})
	require.NoError(t, err)
	require.Equal(t, 100, result.AssignedCount)

	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: "C-new", Name: "后来通道", OwnerDeptID: 1}).Error)
	var count int64
	require.NoError(t, assignment.db.Model(&models.GbRecordingPlanBinding{}).Where("plan_id = ?", plan.ID).Count(&count).Error)
	require.EqualValues(t, 100, count)
}

func TestAssignReturnsPerItemResultsAndNeverWritesInvisibleChannel(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	other, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "其他", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	visible := models.GbChannel{DeviceID: "D", ChannelID: "visible", OwnerDeptID: 1}
	conflict := models.GbChannel{DeviceID: "D", ChannelID: "conflict", OwnerDeptID: 1}
	forbidden := models.GbChannel{DeviceID: "D2", ChannelID: "forbidden", OwnerDeptID: 2}
	require.NoError(t, assignment.db.Create(&[]*models.GbChannel{&visible, &conflict, &forbidden}).Error)
	require.NoError(t, assignment.db.Create(&models.GbRecordingPlanBinding{PlanID: other.ID, ChannelID: conflict.ID, OwnerDeptID: 1, AssignedAt: time.Now()}).Error)

	result, err := assignment.Assign(context.Background(), 1, 1, plan.ID, AssignmentSelection{Type: SelectionByChannel, IDs: []uint{visible.ID, conflict.ID, forbidden.ID, 99999}})
	require.NoError(t, err)
	require.Equal(t, 1, result.AssignedCount)
	require.Equal(t, []string{AssignmentAssigned, AssignmentConflict, AssignmentForbidden, AssignmentNotFound}, assignmentStatuses(result.Items))
	var invisibleCount int64
	require.NoError(t, assignment.db.Model(&models.GbRecordingPlanBinding{}).Where("channel_id = ?", forbidden.ID).Count(&invisibleCount).Error)
	require.Zero(t, invisibleCount)
}

func TestAssignRejectsDisabledPlan(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "停用", Enabled: false})
	require.NoError(t, err)
	_, err = assignment.Assign(context.Background(), 1, 1, plan.ID, AssignmentSelection{Type: SelectionByChannel, IDs: []uint{1}})
	require.ErrorIs(t, err, ErrPlanDisabled)
}

func TestChannelModeSwitchKeepsBindingAndDesiredConsistent(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	channel := models.GbChannel{DeviceID: "D", ChannelID: "C", OwnerDeptID: 1}
	require.NoError(t, assignment.db.Create(&channel).Error)
	_, err = assignment.Assign(context.Background(), 1, 1, plan.ID, AssignmentSelection{Type: SelectionByChannel, IDs: []uint{channel.ID}})
	require.NoError(t, err)

	require.NoError(t, assignment.SetMode(context.Background(), 1, channel.ID, models.RecordingModeContinuous))
	requireChannelMode(t, assignment, channel.ID, models.RecordingModeContinuous, true, false)
	require.NoError(t, assignment.SetMode(context.Background(), 1, channel.ID, models.RecordingModeOff))
	requireChannelMode(t, assignment, channel.ID, models.RecordingModeOff, false, false)
	require.ErrorIs(t, assignment.SetMode(context.Background(), 1, channel.ID, models.RecordingModeScheduled), ErrScheduledPlanRequired)
}

func TestAssignmentSearchIsScopedPaginatedAndReturnsStableIDs(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	for i := 0; i < 5; i++ {
		require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: fmt.Sprintf("C%d", i), Name: "仓库", OwnerDeptID: 1, Status: models.ChannelStatusOnline}).Error)
	}
	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D2", ChannelID: "hidden", Name: "仓库", OwnerDeptID: 2, Status: models.ChannelStatusOnline}).Error)
	online := true
	page, err := assignment.SearchChannels(context.Background(), 1, "仓库", &online, 2, 2)
	require.NoError(t, err)
	require.EqualValues(t, 5, page.Total)
	require.Len(t, page.List, 2)
	require.NotZero(t, page.List[0].ID)
	require.NotEqual(t, page.List[0].ID, page.List[1].ID)
}

func assignmentStatuses(items []AssignmentItem) []string {
	statuses := make([]string, 0, len(items))
	for _, item := range items {
		statuses = append(statuses, item.Status)
	}
	return statuses
}

func requireChannelMode(t *testing.T, service *AssignmentService, channelID uint, mode string, desired bool, bound bool) {
	t.Helper()
	var channel models.GbChannel
	require.NoError(t, service.db.First(&channel, channelID).Error)
	require.Equal(t, mode, channel.RecordingMode)
	require.Equal(t, desired, channel.CloudRecordingEnabled)
	var count int64
	require.NoError(t, service.db.Model(&models.GbRecordingPlanBinding{}).Where("channel_id = ?", channelID).Count(&count).Error)
	require.Equal(t, bound, count == 1)
}

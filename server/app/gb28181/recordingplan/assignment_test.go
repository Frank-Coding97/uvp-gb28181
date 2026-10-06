package recordingplan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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

// deptScopeForTest 造一个「可见部门集合 = 指定集合」的 visibleScope，
// 用于模拟 datascope.VisibilityScope 的归属维度效果（不引入 gin/claims 依赖）。
func deptScopeForTest(deptIDs ...uint) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("owner_dept_id IN ?", deptIDs)
	}
}

// TestSearchDevicesUsesVisibleScopeInsteadOfCallerDept 是本次缺陷的回归护栏：
// 候选列表必须按「可见性」而不是「调用方所属部门」过滤。
// 实机症状：设备列表 7 台（分属 4 个部门），admin(dept=1) 的分配弹窗只有 1 台。
func TestSearchDevicesUsesVisibleScopeInsteadOfCallerDept(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	for i, dept := range []uint{1, 4, 4, 5, 6, 6} {
		require.NoError(t, assignment.db.Create(&models.GbDevice{
			DeviceID: fmt.Sprintf("D%d", i), Name: fmt.Sprintf("设备%d", i), OwnerDeptID: dept,
		}).Error)
	}
	// 调用方 dept=1，但可见范围覆盖全部 4 个部门。
	page, err := assignment.WithVisibleScope(deptScopeForTest(1, 4, 5, 6)).
		SearchDevicesFiltered(context.Background(), 1, "", nil, 1, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 6, page.Total, "注入了可见范围就必须按它过滤，不能再退回调用方 dept")
	assert.Len(t, page.List, 6)
}

// TestSearchChannelsUsesVisibleScope 同上，覆盖「按通道」档位
// （实测通道也横跨多个部门，只修设备档位会漏）。
func TestSearchChannelsUsesVisibleScope(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	for i, dept := range []uint{1, 4, 4, 5, 6} {
		require.NoError(t, assignment.db.Create(&models.GbChannel{
			DeviceID: "D", ChannelID: fmt.Sprintf("C%d", i), Name: "通道", OwnerDeptID: dept,
		}).Error)
	}
	page, err := assignment.WithVisibleScope(deptScopeForTest(1, 4, 5, 6)).
		SearchChannels(context.Background(), 1, "", nil, 1, 50)
	require.NoError(t, err)
	assert.EqualValues(t, 5, page.Total)
}

// TestAssignAndSearchShareSameVisibleScope 防「列表能勾、点确认却forbidden」漂移。
// 这是方案 A 的核心风险：可见性若在两处各写一遍，迟早再次跑偏。
func TestAssignAndSearchShareSameVisibleScope(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	// 计划时段是「周一全天」，必须把 now 钉在周一，否则 schedule.Evaluate 不匹配
	// ⇒ cloud_recording_enabled 会是 false，那是正确行为而非缺陷。
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	// 通道归属 dept=4，调用方 dept=1；靠 visibleScope 才可见。
	crossDept := models.GbChannel{DeviceID: "D", ChannelID: "cross", OwnerDeptID: 4}
	require.NoError(t, assignment.db.Create(&crossDept).Error)

	scope := deptScopeForTest(1, 4)
	// 列表里能看到
	page, err := assignment.WithVisibleScope(scope).SearchChannels(context.Background(), 1, "", nil, 1, 50)
	require.NoError(t, err)
	require.Len(t, page.List, 1)

	// ⛔ 落库必须同样放行，且通道真的被切成「按计划」
	result, err := assignment.WithVisibleScope(scope).Assign(context.Background(), 1, 9, plan.ID,
		AssignmentSelection{Type: SelectionByChannel, IDs: []uint{crossDept.ID}})
	require.NoError(t, err)
	assert.Equal(t, 1, result.AssignedCount, "列表可见的通道，分配时必须能成功（不能 forbidden）")
	requireChannelMode(t, assignment, crossDept.ID, models.RecordingModeScheduled, true, true)
}

// TestAssignWithoutVisibleScopeStillFallsBackToOwnerDept 保留旧的兜底语义：
// 未注入 scope（内部调用/单测）时仍按归属部门，越权项报 forbidden。
func TestAssignWithoutVisibleScopeStillFallsBackToOwnerDept(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	visible := models.GbChannel{DeviceID: "D", ChannelID: "ok", OwnerDeptID: 1}
	forbidden := models.GbChannel{DeviceID: "D2", ChannelID: "no", OwnerDeptID: 2}
	require.NoError(t, assignment.db.Create(&[]*models.GbChannel{&visible, &forbidden}).Error)

	result, err := assignment.Assign(context.Background(), 1, 1, plan.ID,
		AssignmentSelection{Type: SelectionByChannel, IDs: []uint{visible.ID, forbidden.ID}})
	require.NoError(t, err)
	assert.Equal(t, 1, result.AssignedCount)
	assert.Equal(t, []string{AssignmentAssigned, AssignmentForbidden}, assignmentStatuses(result.Items))
}

// TestAssignByDeviceExpandsVisibleChannelsAcrossDepts 覆盖「按设备」展开路径：
// 设备可见后，其下通道也要按同一可见性取，不能只按归属部门。
func TestAssignByDeviceExpandsVisibleChannelsAcrossDepts(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "全天", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	// 设备归属 dept=4（调用方是 dept=1），靠 scope 才可见
	device := models.GbDevice{DeviceID: "D9", Name: "跨部门设备", OwnerDeptID: 4}
	require.NoError(t, assignment.db.Create(&device).Error)
	// 其下通道分属dept=4 与 dept=5：可见范围只给 dept=4
	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D9", ChannelID: "in", OwnerDeptID: 4}).Error)
	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D9", ChannelID: "out", OwnerDeptID: 5}).Error)

	result, err := assignment.WithVisibleScope(deptScopeForTest(4)).
		Assign(context.Background(), 1, 9, plan.ID, AssignmentSelection{Type: SelectionByDevice, IDs: []uint{device.ID}})
	require.NoError(t, err)
	assert.Equal(t, 1, result.AssignedCount, "只应展开可见(dept=4)的那条通道")
}

// TestSearchPutsOnlineCandidatesFirst 锁死「在线优先」排序。
// 症状：设备候选按 id 排，在线设备（status=1）会掉到第二页，用户看不到。
// 排序必须发生在**分页之前**（后端 Order），前端排序只能解决当页。
func TestSearchPutsOnlineCandidatesFirst(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	// 故意先建离线、再建在线：按 id 排时在线会落在最后。
	require.NoError(t, assignment.db.Create(&models.GbDevice{DeviceID: "D1", Name: "离线设备", OwnerDeptID: 1, Status: models.DeviceStatusOffline}).Error)
	require.NoError(t, assignment.db.Create(&models.GbDevice{DeviceID: "D2", Name: "在线设备", OwnerDeptID: 1, Status: models.DeviceStatusOnline}).Error)

	page, err := assignment.SearchDevicesFiltered(context.Background(), 1, "", nil, 1, 10)
	require.NoError(t, err)
	require.Len(t, page.List, 2)
	assert.Equal(t, "在线设备", page.List[0].Name, "在线设备必须排在最前")
	assert.Equal(t, "离线设备", page.List[1].Name)
	assert.True(t, page.List[0].Online)
}

// TestSearchChannelsPutsOnlineFirst 同上，覆盖「按通道」档位。
func TestSearchChannelsPutsOnlineFirst(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: "off", Name: "离线通道", OwnerDeptID: 1, Status: models.ChannelStatusOffline}).Error)
	require.NoError(t, assignment.db.Create(&models.GbChannel{DeviceID: "D1", ChannelID: "on", Name: "在线通道", OwnerDeptID: 1, Status: models.ChannelStatusOnline}).Error)

	page, err := assignment.SearchChannels(context.Background(), 1, "", nil, 1, 10)
	require.NoError(t, err)
	require.Len(t, page.List, 2)
	assert.Equal(t, "在线通道", page.List[0].Name, "在线通道必须排在最前")
	assert.Equal(t, "离线通道", page.List[1].Name)
}

// TestOnlineFilterStillAppliesWithNewOrder 防止排序改动破坏「仅显示在线」筛选。
func TestOnlineFilterStillAppliesWithNewOrder(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	require.NoError(t, assignment.db.Create(&models.GbDevice{DeviceID: "D1", Name: "离线设备", OwnerDeptID: 1, Status: models.DeviceStatusOffline}).Error)
	require.NoError(t, assignment.db.Create(&models.GbDevice{DeviceID: "D2", Name: "在线设备", OwnerDeptID: 1, Status: models.DeviceStatusOnline}).Error)
	online := true
	page, err := assignment.SearchDevicesFiltered(context.Background(), 1, "", &online, 1, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, page.Total)
	require.Len(t, page.List, 1)
	assert.Equal(t, "在线设备", page.List[0].Name)
}

// TestOnlineFirstSurvivesPagination 关键：在线优先必须体现在**分页结果**里。
// 若把排序放在分页之后，第一页仍可能全是离线设备（假象是「排序没生效」）。
func TestOnlineFirstSurvivesPagination(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	for i := 0; i < 5; i++ {
		require.NoError(t, assignment.db.Create(&models.GbDevice{
			DeviceID: fmt.Sprintf("OFF%d", i), Name: fmt.Sprintf("离线%d", i),
			OwnerDeptID: 1, Status: models.DeviceStatusOffline,
		}).Error)
	}
	require.NoError(t, assignment.db.Create(&models.GbDevice{DeviceID: "ON", Name: "在线设备", OwnerDeptID: 1, Status: models.DeviceStatusOnline}).Error)

	page, err := assignment.SearchDevicesFiltered(context.Background(), 1, "", nil, 1, 3)
	require.NoError(t, err)
	require.Len(t, page.List, 3)
	assert.Equal(t, "在线设备", page.List[0].Name, "在线设备必须出现在第一页")
}

// TestSearchMarksBoundCandidates 锁死「已被占用的候选要能标记出来」（方案B）。
// 症状：候选列表不过滤、也不标记已绑定项，用户提交后才被拒。
// 前端据此置灰 + 禁用勾选，所以后端必须可靠地给出 bound。
func TestSearchMarksBoundCandidates(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "已有计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)
	taken := models.GbChannel{DeviceID: "D1", ChannelID: "taken", Name: "已占用通道", OwnerDeptID: 1}
	free := models.GbChannel{DeviceID: "D1", ChannelID: "free", Name: "空闲通道", OwnerDeptID: 1}
	require.NoError(t, assignment.db.Create(&[]*models.GbChannel{&taken, &free}).Error)
	_, err = assignment.Assign(context.Background(), 1, 1, plan.ID, AssignmentSelection{Type: SelectionByChannel, IDs: []uint{taken.ID}})
	require.NoError(t, err)

	page, err := assignment.SearchChannels(context.Background(), 1, "", nil, 1, 10)
	require.NoError(t, err)
	bound := map[string]bool{}
	for _, item := range page.List {
		bound[item.Name] = item.Bound
	}
	assert.True(t, bound["已占用通道"], "已绑定通道必须标记 bound=true（前端据此置灰禁用）")
	assert.False(t, bound["空闲通道"], "未绑定通道必须是 bound=false")
}

// TestDeviceBoundOnlyWhenAllChannelsTaken 关键语义：设备的 bound 只在
// **全部**通道都被占用时才为 true。部分占用仍要允许分配（否则用户无法分配
// 该设备下剩余的空闲通道）⇒ 不能误标成全占用。
func TestDeviceBoundOnlyWhenAllChannelsTaken(t *testing.T) {
	assignment, plans := newAssignmentService(t)
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, schedule.BeijingLocation())
	assignment.now = func() time.Time { return now }
	plan, err := plans.Create(context.Background(), 1, 1, PlanInput{Name: "已有计划", Enabled: true, Periods: []schedule.Period{{Weekday: 1, StartSlot: 0, EndSlot: 48}}})
	require.NoError(t, err)

	full := models.GbDevice{DeviceID: "DFull", Name: "全占用设备", OwnerDeptID: 1}
	part := models.GbDevice{DeviceID: "DPart", Name: "部分占用设备", OwnerDeptID: 1}
	none := models.GbDevice{DeviceID: "DNone", Name: "无通道设备", OwnerDeptID: 1}
	require.NoError(t, assignment.db.Create(&[]*models.GbDevice{&full, &part, &none}).Error)
	c1 := models.GbChannel{DeviceID: "DFull", ChannelID: "c1", OwnerDeptID: 1}
	c2 := models.GbChannel{DeviceID: "DPart", ChannelID: "c2", OwnerDeptID: 1}
	c3 := models.GbChannel{DeviceID: "DPart", ChannelID: "c3", OwnerDeptID: 1}
	require.NoError(t, assignment.db.Create(&[]*models.GbChannel{&c1, &c2, &c3}).Error)
	// 只占用 DFull 的唯一通道 + DPart 的一条
	_, err = assignment.Assign(context.Background(), 1, 1, plan.ID,
		AssignmentSelection{Type: SelectionByChannel, IDs: []uint{c1.ID, c2.ID}})
	require.NoError(t, err)

	page, err := assignment.SearchDevicesFiltered(context.Background(), 1, "", nil, 1, 10)
	require.NoError(t, err)
	bound := map[string]bool{}
	for _, item := range page.List {
		bound[item.Name] = item.Bound
	}
	assert.True(t, bound["全占用设备"], "全部通道被占用 ⇒ bound=true")
	assert.False(t, bound["部分占用设备"], "部分占用必须仍可分配 ⇒ 不能标成 bound")
	assert.False(t, bound["无通道设备"], "无通道不算占用")
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

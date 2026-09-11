package recordingplan

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordingplan/schedule"
)

const recordingFileGrace = 65 * time.Minute

type ChannelStatusQuery struct {
	Keyword     string
	DeviceID    string
	ActualState string
	ReasonCode  string
	Online      *bool
	Page        int
	PageSize    int
}

type PlanChannelStatus struct {
	ChannelID      uint       `json:"channelId"`
	ChannelCode    string     `json:"channelCode"`
	ChannelName    string     `json:"channelName"`
	DeviceCode     string     `json:"deviceCode"`
	DeviceName     string     `json:"deviceName"`
	Online         bool       `json:"online"`
	RecordingMode  string     `json:"recordingMode"`
	DesiredState   string     `json:"desiredState"`
	ActualState    string     `json:"actualState"`
	ReasonCode     string     `json:"reasonCode"`
	ReasonMessage  string     `json:"reasonMessage"`
	NextTransition *time.Time `json:"nextTransitionAt"`
	NextRetry      *time.Time `json:"nextRetryAt"`
	AttemptCount   int        `json:"attemptCount"`
	LastMediaAt    *time.Time `json:"lastMediaAt"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt"`
	StateUpdatedAt time.Time  `json:"stateUpdatedAt"`
}

type PlanChannelStatusPage struct {
	List         []PlanChannelStatus `json:"list"`
	Total        int64               `json:"total"`
	Page         int                 `json:"page"`
	PageSize     int                 `json:"pageSize"`
	StatusCounts map[string]int64    `json:"statusCounts"`
}

type DiagnosticEvidence struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	OK      bool   `json:"ok"`
}

type ScheduleEvidence struct {
	Mode             string     `json:"mode"`
	PlanID           *uint64    `json:"planId"`
	PlanName         string     `json:"planName"`
	PlanEnabled      bool       `json:"planEnabled"`
	Matched          bool       `json:"matched"`
	NextTransitionAt *time.Time `json:"nextTransitionAt"`
}

type DeviceEvidence struct {
	Online        bool                        `json:"online"`
	KeepaliveTime *time.Time                  `json:"keepaliveTime"`
	OfflineAt     *time.Time                  `json:"offlineAt"`
	LastEvent     *models.GbDeviceStatusEvent `json:"lastEvent,omitempty"`
}

type MediaEvidence struct {
	Registered  bool       `json:"registered"`
	LastMediaAt *time.Time `json:"lastMediaAt"`
	StreamID    string     `json:"streamId,omitempty"`
}

type RecorderEvidence struct {
	State     string     `json:"state"`
	StartedAt *time.Time `json:"startedAt"`
	StoppedAt *time.Time `json:"stoppedAt"`
	LastError string     `json:"lastError"`
}

type RetryEvidence struct {
	AttemptCount int        `json:"attemptCount"`
	NextRetryAt  *time.Time `json:"nextRetryAt"`
}

type ChannelDiagnosis struct {
	ChannelID  uint               `json:"channelId"`
	Schedule   ScheduleEvidence   `json:"schedule"`
	Device     DeviceEvidence     `json:"device"`
	SIP        DiagnosticEvidence `json:"sip"`
	Media      MediaEvidence      `json:"media"`
	Recorder   RecorderEvidence   `json:"recorder"`
	File       DiagnosticEvidence `json:"file"`
	Retry      RetryEvidence      `json:"retry"`
	Gap        *GapEvent          `json:"currentGap,omitempty"`
	Conclusion DiagnosticEvidence `json:"conclusion"`
}

type ExecutionEvent struct {
	ID            uint64     `json:"id"`
	Action        string     `json:"action"`
	Stage         string     `json:"stage"`
	Attempt       int        `json:"attempt"`
	Result        string     `json:"result"`
	ReasonCode    string     `json:"reasonCode"`
	ReasonMessage string     `json:"reasonMessage"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt"`
	DurationMs    int64      `json:"durationMs"`
}

type GapEvent struct {
	ID            uint64     `json:"id"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt"`
	DurationMs    int64      `json:"durationMs"`
	ReasonCode    string     `json:"reasonCode"`
	ReasonMessage string     `json:"reasonMessage"`
	Recovered     bool       `json:"recovered"`
}

type ChannelTimeline struct {
	Executions     []ExecutionEvent `json:"executions"`
	ExecutionTotal int64            `json:"executionTotal"`
	Gaps           []GapEvent       `json:"gaps"`
	GapTotal       int64            `json:"gapTotal"`
	Page           int              `json:"page"`
	PageSize       int              `json:"pageSize"`
}

type DiagnosticService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDiagnosticService(db *gorm.DB) *DiagnosticService {
	return &DiagnosticService{db: db, now: time.Now}
}

func (s *DiagnosticService) PagePlanChannels(ctx context.Context, ownerDeptID uint, planID uint64, input ChannelStatusQuery) (*PlanChannelStatusPage, error) {
	if _, err := findPlan(s.db.WithContext(ctx), ownerDeptID, planID); err != nil {
		return nil, err
	}
	input.Page, input.PageSize = normalizePage(input.Page, input.PageSize)
	baseQuery := func() *gorm.DB {
		base := s.db.WithContext(ctx).Table("gb_recording_plan_binding b").
			Joins("JOIN gb_channel c ON c.id = b.channel_id AND c.deleted_at IS NULL").
			Joins("LEFT JOIN gb_recording_plan_channel_state s ON s.channel_id = c.id").
			Joins("LEFT JOIN gb_device d ON d.device_id = c.device_id AND d.deleted_at IS NULL").
			Where("b.plan_id = ? AND b.owner_dept_id = ? AND c.owner_dept_id = ?", planID, ownerDeptID, ownerDeptID)
		return applyChannelStatusFilters(base, input)
	}
	result := &PlanChannelStatusPage{List: []PlanChannelStatus{}, Page: input.Page, PageSize: input.PageSize, StatusCounts: map[string]int64{}}
	if err := baseQuery().Count(&result.Total).Error; err != nil {
		return nil, err
	}
	type countRow struct {
		ActualState string
		Count       int64
	}
	var counts []countRow
	if err := baseQuery().Select("COALESCE(s.actual_state, '') AS actual_state, COUNT(*) AS count").Group("s.actual_state").Scan(&counts).Error; err != nil {
		return nil, err
	}
	for _, row := range counts {
		result.StatusCounts[row.ActualState] = row.Count
	}
	err := baseQuery().Select(`c.id AS channel_id, c.channel_id AS channel_code, COALESCE(NULLIF(c.alias,''),c.name) AS channel_name,
		c.device_id AS device_code, COALESCE(NULLIF(d.alias,''),d.name) AS device_name, CASE WHEN c.status = 1 THEN 1 ELSE 0 END AS online,
		c.recording_mode, COALESCE(s.desired_state,'idle') AS desired_state, COALESCE(s.actual_state,'idle') AS actual_state,
		COALESCE(s.reason_code,'') AS reason_code, COALESCE(s.reason_message,'') AS reason_message,
		s.next_transition_at, s.next_retry_at, COALESCE(s.attempt_count,0) AS attempt_count,
		s.last_media_at, s.last_success_at, s.updated_at AS state_updated_at`).
		Order("c.id").Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).Scan(&result.List).Error
	return result, err
}

func applyChannelStatusFilters(query *gorm.DB, input ChannelStatusQuery) *gorm.DB {
	if keyword := strings.TrimSpace(input.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("c.name LIKE ? OR c.alias LIKE ? OR c.channel_id LIKE ? OR c.device_id LIKE ?", like, like, like, like)
	}
	if deviceID := strings.TrimSpace(input.DeviceID); deviceID != "" {
		query = query.Where("c.device_id = ?", deviceID)
	}
	if state := strings.TrimSpace(input.ActualState); state != "" {
		query = query.Where("s.actual_state = ?", state)
	}
	if reason := strings.TrimSpace(input.ReasonCode); reason != "" {
		query = query.Where("s.reason_code = ?", reason)
	}
	if input.Online != nil {
		query = query.Where("c.status = ?", *input.Online)
	}
	return query
}

func (s *DiagnosticService) Timeline(ctx context.Context, ownerDeptID, channelID uint, page, pageSize int) (*ChannelTimeline, error) {
	if _, err := s.scopedChannel(ctx, ownerDeptID, channelID); err != nil {
		return nil, err
	}
	page, pageSize = normalizePage(page, pageSize)
	result := &ChannelTimeline{Executions: []ExecutionEvent{}, Gaps: []GapEvent{}, Page: page, PageSize: pageSize}
	executions := s.db.WithContext(ctx).Model(&models.GbRecordingPlanExecution{}).Where("channel_id = ?", channelID)
	if err := executions.Count(&result.ExecutionTotal).Error; err != nil {
		return nil, err
	}
	if err := executions.Select("id, action, stage, attempt, result, reason_code, reason_message, started_at, ended_at, duration_ms").Order("started_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&result.Executions).Error; err != nil {
		return nil, err
	}
	gaps := s.db.WithContext(ctx).Model(&models.GbRecordingPlanGap{}).Where("channel_id = ?", channelID)
	if err := gaps.Count(&result.GapTotal).Error; err != nil {
		return nil, err
	}
	if err := gaps.Select("id, started_at, ended_at, duration_ms, reason_code, reason_message, recovered").Order("started_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&result.Gaps).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DiagnosticService) DiagnoseChannel(ctx context.Context, ownerDeptID, channelID uint) (*ChannelDiagnosis, error) {
	channel, err := s.scopedChannel(ctx, ownerDeptID, channelID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	result := &ChannelDiagnosis{ChannelID: channel.ID, Schedule: ScheduleEvidence{Mode: channel.RecordingMode}, Media: MediaEvidence{}, File: DiagnosticEvidence{Code: "FILE_NOT_EXPECTED", Message: "当前不要求生成录像文件", OK: true}}

	var state models.GbRecordingPlanChannelState
	stateFound := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&state)
	if stateFound.Error != nil {
		return nil, stateFound.Error
	}
	if stateFound.RowsAffected > 0 {
		result.Retry = RetryEvidence{AttemptCount: state.AttemptCount, NextRetryAt: state.NextRetryAt}
		result.Media.Registered = state.StreamID != "" && state.ActualState == models.RecordingStateRecording
		result.Media.LastMediaAt = state.LastMediaAt
	}

	if err := s.fillSchedule(ctx, channel, now, &result.Schedule); err != nil {
		return nil, err
	}
	if err := s.fillDevice(ctx, channel.DeviceID, &result.Device); err != nil {
		return nil, err
	}
	result.SIP = sipEvidence(stateFound.RowsAffected > 0, state)
	if err := s.fillRecorderAndFile(ctx, channel.ID, now, &result.Recorder, &result.File); err != nil {
		return nil, err
	}
	var gap GapEvent
	gapFound := s.db.WithContext(ctx).Model(&models.GbRecordingPlanGap{}).Where("channel_id = ? AND ended_at IS NULL", channel.ID).Order("id DESC").Select("id, started_at, ended_at, duration_ms, reason_code, reason_message, recovered").Limit(1).Scan(&gap)
	if gapFound.Error != nil {
		return nil, gapFound.Error
	}
	if gap.ID != 0 {
		result.Gap = &gap
	}
	result.Conclusion = diagnosisConclusion(channel, stateFound.RowsAffected > 0, state, result)
	return result, nil
}

func (s *DiagnosticService) scopedChannel(ctx context.Context, ownerDeptID, channelID uint) (*models.GbChannel, error) {
	var channel models.GbChannel
	result := s.db.WithContext(ctx).Where("id = ? AND owner_dept_id = ?", channelID, ownerDeptID).Limit(1).Find(&channel)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrPlanNotFound
	}
	return &channel, nil
}

func (s *DiagnosticService) fillSchedule(ctx context.Context, channel *models.GbChannel, now time.Time, evidence *ScheduleEvidence) error {
	if channel.RecordingMode == models.RecordingModeContinuous {
		evidence.PlanEnabled, evidence.Matched = true, true
		return nil
	}
	if channel.RecordingMode != models.RecordingModeScheduled {
		return nil
	}
	var binding models.GbRecordingPlanBinding
	found := s.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Limit(1).Find(&binding)
	if found.Error != nil || found.RowsAffected == 0 {
		return found.Error
	}
	var plan models.GbRecordingPlan
	found = s.db.WithContext(ctx).Where("id = ? AND owner_dept_id = ?", binding.PlanID, channel.OwnerDeptID).Limit(1).Find(&plan)
	if found.Error != nil || found.RowsAffected == 0 {
		return found.Error
	}
	periods, err := loadPeriods(s.db.WithContext(ctx), plan.ID)
	if err != nil {
		return err
	}
	evaluation := schedule.Evaluate(periods, now)
	evidence.PlanID, evidence.PlanName, evidence.PlanEnabled = &plan.ID, plan.Name, plan.Status == 1
	evidence.Matched, evidence.NextTransitionAt = evaluation.Matched, evaluation.NextTransition
	return nil
}

func (s *DiagnosticService) fillDevice(ctx context.Context, deviceCode string, evidence *DeviceEvidence) error {
	var device models.GbDevice
	found := s.db.WithContext(ctx).Where("device_id = ?", deviceCode).Limit(1).Find(&device)
	if found.Error != nil {
		return found.Error
	}
	if found.RowsAffected == 0 {
		return nil
	}
	evidence.Online, evidence.KeepaliveTime, evidence.OfflineAt = device.Status == models.DeviceStatusOnline, device.KeepaliveTime, device.OfflineAt
	var event models.GbDeviceStatusEvent
	eventFound := s.db.WithContext(ctx).Where("device_id = ?", device.ID).Order("occurred_at DESC, id DESC").Limit(1).Find(&event)
	if eventFound.Error != nil {
		return eventFound.Error
	}
	if eventFound.RowsAffected > 0 {
		event.IP, event.Detail = "", ""
		evidence.LastEvent = &event
	}
	return nil
}

func (s *DiagnosticService) fillRecorderAndFile(ctx context.Context, channelID uint, now time.Time, recorder *RecorderEvidence, file *DiagnosticEvidence) error {
	var session models.GbRecordingSession
	found := s.db.WithContext(ctx).Where("channel_id = ?", channelID).Order("id DESC").Limit(1).Find(&session)
	if found.Error != nil {
		return found.Error
	}
	if found.RowsAffected == 0 {
		return nil
	}
	recorder.State, recorder.StartedAt, recorder.StoppedAt, recorder.LastError = session.State, session.StartedAt, session.StoppedAt, session.LastError
	var latest models.GbRecordingFile
	fileFound := s.db.WithContext(ctx).Where("channel_id = ?", channelID).Order("COALESCE(start_time, discovered_at) DESC, id DESC").Limit(1).Find(&latest)
	if fileFound.Error != nil {
		return fileFound.Error
	}
	if fileFound.RowsAffected > 0 && session.StartedAt != nil && latest.DiscoveredAt.After(*session.StartedAt) {
		*file = DiagnosticEvidence{Code: "FILE_GENERATED", Message: "已收到录像文件索引", OK: true}
		return nil
	}
	if session.State == models.RecordingSessionStateRecording && session.StartedAt != nil && now.Sub(*session.StartedAt) > recordingFileGrace {
		*file = DiagnosticEvidence{Code: "FILE_NOT_GENERATED", Message: "录像已超过默认切片时长及宽限，但尚未收到文件回调", OK: false}
	}
	return nil
}

func sipEvidence(stateFound bool, state models.GbRecordingPlanChannelState) DiagnosticEvidence {
	if !stateFound {
		return DiagnosticEvidence{Code: "STATE_NOT_INITIALIZED", Message: "调度状态尚未初始化", OK: false}
	}
	if state.ReasonCode == ReasonStreamStartFailed {
		return DiagnosticEvidence{Code: state.ReasonCode, Message: state.ReasonMessage, OK: false}
	}
	if state.StreamID != "" {
		return DiagnosticEvidence{Code: "STREAM_STARTED", Message: "拉流阶段已完成", OK: true}
	}
	return DiagnosticEvidence{Code: "STREAM_NOT_STARTED", Message: "尚未建立实时媒体流", OK: false}
}

func diagnosisConclusion(channel *models.GbChannel, stateFound bool, state models.GbRecordingPlanChannelState, diagnosis *ChannelDiagnosis) DiagnosticEvidence {
	if channel.RecordingMode == models.RecordingModeOff {
		return DiagnosticEvidence{Code: ReasonModeOff, Message: "通道录像模式已关闭", OK: true}
	}
	if !diagnosis.Schedule.PlanEnabled {
		return DiagnosticEvidence{Code: ReasonPlanDisabled, Message: "录像计划已停用", OK: true}
	}
	if !diagnosis.Schedule.Matched {
		return DiagnosticEvidence{Code: ReasonOutsideSchedule, Message: "当前不在录像时间段", OK: true}
	}
	if !diagnosis.Device.Online {
		return DiagnosticEvidence{Code: ReasonDeviceOffline, Message: "设备或通道离线", OK: false}
	}
	if !stateFound {
		return DiagnosticEvidence{Code: "STATE_NOT_INITIALIZED", Message: "调度状态尚未初始化", OK: false}
	}
	if state.ReasonCode != "" && state.ActualState != models.RecordingStateRecording {
		return DiagnosticEvidence{Code: state.ReasonCode, Message: state.ReasonMessage, OK: false}
	}
	if !diagnosis.File.OK && diagnosis.File.Code == "FILE_NOT_GENERATED" {
		return diagnosis.File
	}
	if state.ActualState == models.RecordingStateRecording {
		return DiagnosticEvidence{Code: "RECORDING", Message: "通道正在按计划录像", OK: true}
	}
	return DiagnosticEvidence{Code: "PENDING", Message: "等待下一次调度对账", OK: false}
}

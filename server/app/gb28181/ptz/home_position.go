package ptz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const homePositionFreshFor = 60 * time.Second

type HomePositionUpdate struct {
	DeviceID           uint
	ChannelID          uint
	ChannelCode        string
	Enabled            bool
	ResetTime          *int
	PresetID           *int
	EnabledEncoding    gbmodels.PTZHomePositionEnabledEncoding
	ConfirmedAt        time.Time
	Source             gbmodels.PTZHomePositionSource
	Verification       gbmodels.PTZHomePositionVerification
	SourceSN           int
	SourceOperationID  string
	SourceOperationSeq uint
	RawSummary         string
}

type HomePositionCapabilityStatus string

const (
	HomePositionCapabilitySupported   HomePositionCapabilityStatus = "supported"
	HomePositionCapabilityUnsupported HomePositionCapabilityStatus = "unsupported"
	HomePositionCapabilityUnknown     HomePositionCapabilityStatus = "unknown"
)

type HomePositionCapability struct {
	Status HomePositionCapabilityStatus `json:"status"`
	Reason string                       `json:"reason"`
}

type HomePositionCapabilities struct {
	Control HomePositionCapability `json:"control"`
	Query   HomePositionCapability `json:"query"`
}

type HomePositionValue struct {
	Enabled      bool                                 `json:"enabled"`
	ResetTime    *int                                 `json:"resetTime"`
	PresetID     *int                                 `json:"presetId"`
	ConfirmedAt  time.Time                            `json:"confirmedAt"`
	Source       gbmodels.PTZHomePositionSource       `json:"source"`
	Verification gbmodels.PTZHomePositionVerification `json:"verification"`
}

type HomePositionControlStatus string

const (
	HomePositionControlIdle      HomePositionControlStatus = "idle"
	HomePositionControlPending   HomePositionControlStatus = "pending"
	HomePositionControlAccepted  HomePositionControlStatus = "accepted"
	HomePositionControlRejected  HomePositionControlStatus = "rejected"
	HomePositionControlTimeout   HomePositionControlStatus = "timeout"
	HomePositionControlUnknown   HomePositionControlStatus = "unknown"
	HomePositionControlCancelled HomePositionControlStatus = "cancelled"
)

type HomePositionControlState struct {
	Status      HomePositionControlStatus `json:"status"`
	OperationID *string                   `json:"operationId"`
	Action      *string                   `json:"action"`
	ErrorCode   *string                   `json:"errorCode"`
	DeadlineAt  *time.Time                `json:"deadlineAt"`
}

type HomePositionRefreshStatus string

const (
	HomePositionRefreshIdle            HomePositionRefreshStatus = "idle"
	HomePositionRefreshPending         HomePositionRefreshStatus = "pending"
	HomePositionRefreshSucceeded       HomePositionRefreshStatus = "succeeded"
	HomePositionRefreshSucceededNoData HomePositionRefreshStatus = "succeeded_no_data"
	HomePositionRefreshTimeout         HomePositionRefreshStatus = "timeout"
	HomePositionRefreshFailed          HomePositionRefreshStatus = "failed"
)

type HomePositionRefreshState struct {
	Status      HomePositionRefreshStatus `json:"status"`
	OperationID *string                   `json:"operationId"`
	ErrorCode   *string                   `json:"errorCode"`
	DeadlineAt  *time.Time                `json:"deadlineAt"`
}

type HomePositionReadModel struct {
	HomePosition   *HomePositionValue       `json:"homePosition"`
	ControlSupport HomePositionCapability   `json:"controlSupport"`
	QuerySupport   HomePositionCapability   `json:"querySupport"`
	Freshness      gbmodels.PTZFreshness    `json:"freshness"`
	Control        HomePositionControlState `json:"control"`
	Refresh        HomePositionRefreshState `json:"refresh"`
}

// PTZOperationReadModel is the intentionally small operation contract used
// by UI polling. Transport correlation and audit fields stay server-side.
type PTZOperationReadModel struct {
	OperationID      string                      `json:"operationId"`
	Status           gbmodels.PTZOperationStatus `json:"status"`
	ErrorCode        *string                     `json:"errorCode"`
	ErrorMessage     *string                     `json:"errorMessage"`
	CompletedAt      *time.Time                  `json:"completedAt"`
	DeadlineAt       *time.Time                  `json:"deadlineAt"`
	ResponseRequired bool                        `json:"responseRequired,omitempty"`
	DeviceResult     *string                     `json:"deviceResult,omitempty"`
	TargetScope      string                      `json:"targetScope,omitempty"`
	TargetCode       string                      `json:"targetCode,omitempty"`
	ProfileVersion   string                      `json:"profileVersion,omitempty"`
}

// ApplyHomePosition accepts only a strictly newer source operation. Equal and
// older responses return the current row without renewing ConfirmedAt.
func (s *Service) ApplyHomePosition(ctx context.Context, update HomePositionUpdate) (gbmodels.GbPTZHomePosition, bool, error) {
	if s == nil || s.db == nil {
		return gbmodels.GbPTZHomePosition{}, false, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 未就绪", nil)
	}
	entry := s.lockChannel(update.ChannelID)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		s.unlockChannel(update.ChannelID, entry)
	}()
	return s.applyHomePositionDB(ptzWriter(s.db).WithContext(ctx), update)
}

// Callers must hold the channel lock so the initial insert remains portable
// across all supported database dialects.
func (s *Service) applyHomePositionDB(db *gorm.DB, update HomePositionUpdate) (gbmodels.GbPTZHomePosition, bool, error) {
	if update.DeviceID == 0 || update.ChannelID == 0 || strings.TrimSpace(update.ChannelCode) == "" {
		return gbmodels.GbPTZHomePosition{}, false, fmt.Errorf("看守位缓存目标不完整")
	}
	if update.ConfirmedAt.IsZero() {
		update.ConfirmedAt = s.now()
	}
	if update.EnabledEncoding == "" {
		update.EnabledEncoding = gbmodels.PTZHomePositionEnabledNumeric
	}
	operationID := nullableString(update.SourceOperationID)
	updatedAt := s.now()
	updates := map[string]interface{}{
		"device_id": update.DeviceID, "channel_code": update.ChannelCode,
		"enabled": update.Enabled, "reset_time": update.ResetTime, "preset_id": update.PresetID,
		"enabled_encoding": update.EnabledEncoding, "confirmed_at": update.ConfirmedAt,
		"source": update.Source, "verification": update.Verification, "source_sn": update.SourceSN,
		"source_operation_id": operationID, "source_operation_seq": update.SourceOperationSeq,
		"raw_summary": update.RawSummary, "updated_at": updatedAt,
	}
	result := db.Model(&gbmodels.GbPTZHomePosition{}).
		Where("channel_id = ? AND source_operation_seq < ?", update.ChannelID, update.SourceOperationSeq).
		Updates(updates)
	if result.Error != nil {
		return gbmodels.GbPTZHomePosition{}, false, result.Error
	}
	if result.RowsAffected > 0 {
		home, err := getHomePositionDB(db, update.ChannelID)
		return home, true, err
	}

	current, found, err := findHomePositionDB(db, update.ChannelID)
	if err != nil {
		return gbmodels.GbPTZHomePosition{}, false, err
	}
	if found {
		return current, false, nil
	}
	row := gbmodels.GbPTZHomePosition{
		DeviceID: update.DeviceID, ChannelID: update.ChannelID, ChannelCode: update.ChannelCode,
		Enabled: update.Enabled, ResetTime: update.ResetTime, PresetID: update.PresetID,
		EnabledEncoding: update.EnabledEncoding, ConfirmedAt: update.ConfirmedAt,
		Source: update.Source, Verification: update.Verification, SourceSN: update.SourceSN,
		SourceOperationID: operationID, SourceOperationSeq: update.SourceOperationSeq,
		RawSummary: update.RawSummary, CreatedAt: updatedAt, UpdatedAt: updatedAt,
	}
	if err := db.Create(&row).Error; err != nil {
		return gbmodels.GbPTZHomePosition{}, false, err
	}
	return row, true, nil
}

func nullableString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func (s *Service) findHomePosition(ctx context.Context, channelID uint) (gbmodels.GbPTZHomePosition, bool, error) {
	return findHomePositionDB(ptzWriter(s.db).WithContext(ctx), channelID)
}

func findHomePositionDB(db *gorm.DB, channelID uint) (gbmodels.GbPTZHomePosition, bool, error) {
	var home gbmodels.GbPTZHomePosition
	result := db.Where("channel_id = ?", channelID).Limit(1).Find(&home)
	return home, result.RowsAffected > 0, result.Error
}

func (s *Service) getHomePosition(ctx context.Context, channelID uint) (gbmodels.GbPTZHomePosition, error) {
	return getHomePositionDB(ptzWriter(s.db).WithContext(ctx), channelID)
}

func getHomePositionDB(db *gorm.DB, channelID uint) (gbmodels.GbPTZHomePosition, error) {
	home, found, err := findHomePositionDB(db, channelID)
	if err != nil {
		return home, err
	}
	if !found {
		return home, gorm.ErrRecordNotFound
	}
	return home, nil
}

func HomePositionFreshness(home *gbmodels.GbPTZHomePosition, latestQuery *gbmodels.GbPTZOperation, now time.Time) gbmodels.PTZFreshness {
	if home == nil || home.ConfirmedAt.IsZero() {
		return gbmodels.PTZFreshnessUnknown
	}
	if latestQuery != nil && latestQuery.ID > home.SourceOperationSeq {
		if latestQuery.Status == gbmodels.PTZOperationAccepted && latestQuery.ResponseHasData != nil && !*latestQuery.ResponseHasData {
			return gbmodels.PTZFreshnessStale
		}
		switch latestQuery.Status {
		case gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationUnknown, gbmodels.PTZOperationCancelled:
			return gbmodels.PTZFreshnessStale
		}
	}
	if now.After(home.ConfirmedAt.Add(homePositionFreshFor)) {
		return gbmodels.PTZFreshnessStale
	}
	return gbmodels.PTZFreshnessFresh
}

func parseHomePositionProfile(raw *string) (map[string]json.RawMessage, string) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, "设备未上报该能力"
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*raw), &profile); err != nil {
		return nil, "Capabilities JSON 无效"
	}
	return profile, "设备未上报该能力"
}

func profileHomePositionCapability(profile map[string]json.RawMessage, missingReason string, keys ...string) (HomePositionCapability, bool) {
	var value json.RawMessage
	for _, key := range keys {
		if candidate, ok := profile[key]; ok {
			value = candidate
			break
		}
	}
	if value == nil || string(value) == "null" {
		return HomePositionCapability{Status: HomePositionCapabilityUnknown, Reason: missingReason}, false
	}
	var supported bool
	if err := json.Unmarshal(value, &supported); err != nil {
		return HomePositionCapability{Status: HomePositionCapabilityUnknown, Reason: "设备上报的能力值不是布尔值"}, false
	}
	if supported {
		return HomePositionCapability{Status: HomePositionCapabilitySupported, Reason: "设备明确上报支持"}, true
	}
	return HomePositionCapability{Status: HomePositionCapabilityUnsupported, Reason: "设备明确上报不支持"}, true
}

func (s *Service) ResolveHomePositionCapabilities(ctx context.Context, channelID uint, raw *string) (HomePositionCapabilities, error) {
	profile, missingReason := parseHomePositionProfile(raw)
	control, controlDeclared := profileHomePositionCapability(profile, missingReason, "home_position_control", "homePositionControl")
	query, queryDeclared := profileHomePositionCapability(profile, missingReason, "home_position_query", "homePositionQuery")
	if !controlDeclared {
		var count int64
		if err := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
			Where("channel_id = ? AND cmd_type = ? AND action = ? AND status = ?", channelID, manscdp.CmdDeviceControl, "home_position", gbmodels.PTZOperationAccepted).
			Count(&count).Error; err != nil {
			return HomePositionCapabilities{}, err
		}
		if count > 0 {
			control = HomePositionCapability{Status: HomePositionCapabilitySupported, Reason: "历史控制响应已确认支持"}
		}
	}
	if !queryDeclared {
		var count int64
		if err := s.db.WithContext(ctx).Model(&gbmodels.GbPTZOperation{}).
			Where("channel_id = ? AND cmd_type = ? AND status = ?", channelID, manscdp.CmdHomePositionQuery, gbmodels.PTZOperationAccepted).
			Count(&count).Error; err != nil {
			return HomePositionCapabilities{}, err
		}
		if count > 0 {
			query = HomePositionCapability{Status: HomePositionCapabilitySupported, Reason: "历史查询响应已确认支持"}
		}
	}
	return HomePositionCapabilities{Control: control, Query: query}, nil
}

func operationString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func operationDeadline(operation gbmodels.GbPTZOperation) *time.Time {
	if operation.DeadlineAt != nil {
		return operation.DeadlineAt
	}
	if operation.TransportDeadlineAt != nil {
		return operation.TransportDeadlineAt
	}
	return operation.QueueDeadlineAt
}

func BuildPTZOperationReadModel(operation gbmodels.GbPTZOperation) PTZOperationReadModel {
	return PTZOperationReadModel{
		OperationID:      operation.OperationID,
		Status:           operation.Status,
		ErrorCode:        operationString(operation.ErrorCode),
		ErrorMessage:     operationString(operation.ErrorMessage),
		CompletedAt:      operation.CompletedAt,
		DeadlineAt:       operationDeadline(operation),
		ResponseRequired: operation.ResponseRequired,
		DeviceResult:     operationString(operation.DeviceResult),
		TargetScope:      operation.TargetScope,
		TargetCode:       operation.TargetCode,
		ProfileVersion:   operation.ProfileVersion,
	}
}

func controlReadState(operation *gbmodels.GbPTZOperation) HomePositionControlState {
	if operation == nil {
		return HomePositionControlState{Status: HomePositionControlIdle}
	}
	status := HomePositionControlStatus(operation.Status)
	if operation.Status == gbmodels.PTZOperationQueued || operation.Status == gbmodels.PTZOperationSent {
		status = HomePositionControlPending
	}
	return HomePositionControlState{
		Status: status, OperationID: operationString(operation.OperationID),
		Action: operationString(operation.Action), ErrorCode: operationString(operation.ErrorCode),
		DeadlineAt: operationDeadline(*operation),
	}
}

func refreshReadState(operation *gbmodels.GbPTZOperation) HomePositionRefreshState {
	if operation == nil {
		return HomePositionRefreshState{Status: HomePositionRefreshIdle}
	}
	status := HomePositionRefreshFailed
	switch operation.Status {
	case gbmodels.PTZOperationQueued, gbmodels.PTZOperationSent:
		status = HomePositionRefreshPending
	case gbmodels.PTZOperationAccepted:
		status = HomePositionRefreshSucceeded
		if operation.ResponseHasData != nil && !*operation.ResponseHasData {
			status = HomePositionRefreshSucceededNoData
		}
	case gbmodels.PTZOperationTimeout:
		status = HomePositionRefreshTimeout
	}
	return HomePositionRefreshState{
		Status: status, OperationID: operationString(operation.OperationID),
		ErrorCode: operationString(operation.ErrorCode), DeadlineAt: operationDeadline(*operation),
	}
}

func (s *Service) latestHomePositionOperation(ctx context.Context, channelID uint, query *gorm.DB) (*gbmodels.GbPTZOperation, error) {
	var operation gbmodels.GbPTZOperation
	result := query.WithContext(ctx).Where("channel_id = ?", channelID).Order("id DESC").Limit(1).Find(&operation)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &operation, nil
}

func (s *Service) exactReconcileOperation(ctx context.Context, home *gbmodels.GbPTZHomePosition) (*gbmodels.GbPTZOperation, error) {
	if home == nil || home.Source != gbmodels.PTZHomePositionSourceControlACK ||
		home.Verification != gbmodels.PTZHomePositionVerificationUnverified || home.SourceOperationID == nil {
		return nil, nil
	}
	var control gbmodels.GbPTZOperation
	result := s.db.WithContext(ctx).Where("operation_id = ?", *home.SourceOperationID).Limit(1).Find(&control)
	if result.Error != nil || result.RowsAffected == 0 || control.ReconcileOperationID == nil {
		return nil, result.Error
	}
	var reconcile gbmodels.GbPTZOperation
	result = s.db.WithContext(ctx).Where("operation_id = ? AND channel_id = ? AND cmd_type = ?", *control.ReconcileOperationID, home.ChannelID, manscdp.CmdHomePositionQuery).Limit(1).Find(&reconcile)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &reconcile, nil
}

func (s *Service) GetHomePositionReadModel(ctx context.Context, channelID uint, rawCapabilities *string) (HomePositionReadModel, error) {
	if s == nil || s.db == nil {
		return HomePositionReadModel{}, operationError(ErrorCodeHomePositionUnavailable, "PTZ service 未就绪", nil)
	}
	capabilities, err := s.ResolveHomePositionCapabilities(ctx, channelID, rawCapabilities)
	if err != nil {
		return HomePositionReadModel{}, err
	}
	home, found, err := s.findHomePosition(ctx, channelID)
	if err != nil {
		return HomePositionReadModel{}, err
	}
	var homePointer *gbmodels.GbPTZHomePosition
	if found {
		homePointer = &home
	}
	latestQuery, err := s.latestHomePositionOperation(ctx, channelID, s.db.Model(&gbmodels.GbPTZOperation{}).Where("cmd_type = ?", manscdp.CmdHomePositionQuery))
	if err != nil {
		return HomePositionReadModel{}, err
	}
	latestControl, err := s.latestHomePositionOperation(ctx, channelID, s.db.Model(&gbmodels.GbPTZOperation{}).Where("cmd_type = ? AND action = ?", manscdp.CmdDeviceControl, "home_position"))
	if err != nil {
		return HomePositionReadModel{}, err
	}
	refresh, err := s.exactReconcileOperation(ctx, homePointer)
	if err != nil {
		return HomePositionReadModel{}, err
	}
	if refresh == nil {
		query := s.db.Model(&gbmodels.GbPTZOperation{}).Where("cmd_type = ?", manscdp.CmdHomePositionQuery)
		if homePointer != nil {
			query = query.Where("id >= ?", home.SourceOperationSeq)
		}
		refresh, err = s.latestHomePositionOperation(ctx, channelID, query)
		if err != nil {
			return HomePositionReadModel{}, err
		}
	}
	model := HomePositionReadModel{
		ControlSupport: capabilities.Control,
		QuerySupport:   capabilities.Query,
		Freshness:      HomePositionFreshness(homePointer, latestQuery, s.now()),
		Control:        controlReadState(latestControl),
		Refresh:        refreshReadState(refresh),
	}
	if homePointer != nil {
		model.HomePosition = &HomePositionValue{
			Enabled: home.Enabled, ResetTime: home.ResetTime, PresetID: home.PresetID,
			ConfirmedAt: home.ConfirmedAt, Source: home.Source, Verification: home.Verification,
		}
	}
	return model, nil
}

// GetHomePositionReadModelForRefresh keeps a manual refresh response tied to
// the operation returned by that request. A newer unrelated query must not
// replace an idempotent replay's operation in the same HTTP response.
func (s *Service) GetHomePositionReadModelForRefresh(ctx context.Context, channelID uint, rawCapabilities *string, operationID string) (HomePositionReadModel, error) {
	model, err := s.GetHomePositionReadModel(ctx, channelID, rawCapabilities)
	if err != nil || strings.TrimSpace(operationID) == "" {
		return model, err
	}
	var operation gbmodels.GbPTZOperation
	result := ptzWriter(s.db).WithContext(ctx).
		Where("operation_id = ? AND channel_id = ? AND cmd_type = ? AND action = ?", operationID, channelID, manscdp.CmdHomePositionQuery, "refresh_home_position").
		Limit(1).Find(&operation)
	if result.Error != nil {
		return HomePositionReadModel{}, result.Error
	}
	if result.RowsAffected == 1 {
		model.Refresh = refreshReadState(&operation)
	}
	return model, nil
}

func (s *Service) homePositionParseOptions(ctx context.Context, channelID uint) (manscdp.HomePositionParseOptions, error) {
	var channel gbmodels.GbChannel
	result := s.db.WithContext(ctx).Select("id", "capabilities").Where("id = ?", channelID).Limit(1).Find(&channel)
	if result.Error != nil || result.RowsAffected == 0 || channel.Capabilities == nil {
		return manscdp.HomePositionParseOptions{}, result.Error
	}
	profile, _ := parseHomePositionProfile(channel.Capabilities)
	for _, key := range []string{"home_position_enabled_encoding", "homePositionEnabledEncoding"} {
		value, ok := profile[key]
		if !ok {
			continue
		}
		var encoding string
		if json.Unmarshal(value, &encoding) == nil && strings.EqualFold(strings.TrimSpace(encoding), "boolean_text") {
			return manscdp.HomePositionParseOptions{AllowBooleanEnabled: true}, nil
		}
	}
	return manscdp.HomePositionParseOptions{}, nil
}

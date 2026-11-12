// Package deviceconfig exposes scoped OpenAPI access to the existing GB/T
// 28181 configuration and video-parameter operations.
package deviceconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

var (
	ErrNotFound               = errors.New("device configuration not found")
	ErrInvalidRequest         = errors.New("device configuration request is invalid")
	ErrConflict               = errors.New("device configuration request conflicts with an existing operation")
	ErrUnavailable            = errors.New("device configuration service unavailable")
	ErrDeviceOffline          = errors.New("device is offline")
	ErrCapabilityNotSupported = errors.New("device configuration capability is not supported")
	ErrCapabilityUnknown      = errors.New("device configuration capability is not known")
)

const configFreshFor = time.Minute

// CommandService is the existing GB28181 PTZ/configuration domain service.
// OpenAPI never constructs SIP or MANSCDP messages itself.
type CommandService interface {
	ReadDeviceConfigs(context.Context, runtimeptz.Target, []string, uint, uint, string) (gbmodels.GbPTZOperation, error)
	ApplyDeviceConfig(context.Context, runtimeptz.Target, manscdp.DeviceConfigBlocks, uint, uint, string) (gbmodels.GbPTZOperation, error)
	ReadVideoParams(context.Context, runtimeptz.Target, uint, uint, string) (gbmodels.GbPTZOperation, error)
	ApplyVideoParams(context.Context, runtimeptz.Target, []manscdp.VideoParamItem, uint, uint, string) (gbmodels.GbPTZOperation, error)
	GetOperation(context.Context, string) (gbmodels.GbPTZOperation, error)
}

// Request contains identity from the authenticated OpenAPI gateway. The client
// ID, department boundary, device codes, and idempotency key are never read
// from an unsigned JSON field.
type Request struct {
	ClientID       int64
	Scope          resource.DepartmentScope
	DeviceID       string
	ChannelID      string
	IdempotencyKey string
}

type Service struct {
	db       *gorm.DB
	commands CommandService
	now      func() time.Time
}

func New(db *gorm.DB, commands CommandService) *Service {
	return &Service{db: db, commands: commands, now: time.Now}
}

type ConfigEntry struct {
	ConfigType string          `json:"configType"`
	TargetCode string          `json:"targetCode"`
	TargetKind string          `json:"targetKind"`
	Config     json.RawMessage `json:"config"`
	Freshness  string          `json:"freshness"`
	ObservedAt time.Time       `json:"observedAt"`
}

type ConfigSnapshot struct {
	DeviceID        string        `json:"deviceId"`
	ChannelID       string        `json:"channelId"`
	ProtocolVersion string        `json:"protocolVersion"`
	Freshness       string        `json:"freshness"`
	ObservedAt      *time.Time    `json:"observedAt,omitempty"`
	Items           []ConfigEntry `json:"items"`
}

type VideoParamSnapshot struct {
	DeviceID        string                   `json:"deviceId"`
	ChannelID       string                   `json:"channelId"`
	ProtocolVersion string                   `json:"protocolVersion"`
	Freshness       string                   `json:"freshness"`
	ObservedAt      *time.Time               `json:"observedAt,omitempty"`
	Items           []manscdp.VideoParamItem `json:"items"`
}

type Reconciliation struct {
	State           string     `json:"state"`
	OperationID     string     `json:"operationId,omitempty"`
	Status          string     `json:"status,omitempty"`
	ResponseHasData *bool      `json:"responseHasData,omitempty"`
	ErrorCode       string     `json:"errorCode,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

type OperationResult struct {
	OperationID        string         `json:"operationId"`
	Action             string         `json:"action"`
	Status             string         `json:"status"`
	SN                 int            `json:"sn"`
	ProtocolVersion    string         `json:"protocolVersion"`
	DeviceAcknowledged bool           `json:"deviceAcknowledged"`
	DeviceResult       string         `json:"deviceResult,omitempty"`
	ErrorCode          string         `json:"errorCode,omitempty"`
	ResponseAt         *time.Time     `json:"responseAt,omitempty"`
	ResponseHasData    *bool          `json:"responseHasData,omitempty"`
	Reconciliation     Reconciliation `json:"reconciliation"`
}

func (s *Service) Refresh(ctx context.Context, request Request, configTypes []string) (OperationResult, error) {
	if !s.ready() || ctx == nil || !validRequest(request, true) {
		return OperationResult{}, ErrInvalidRequest
	}
	target, _, _, err := s.resolveTarget(ctx, request)
	if err != nil {
		return OperationResult{}, err
	}
	if err = requireOnline(target); err != nil {
		return OperationResult{}, err
	}
	if configTypes == nil {
		switch target.Profile.Version {
		case protocol.Version2016:
			configTypes = []string{manscdp.ConfigTypeBasicParam}
		case protocol.Version2022:
			for _, configType := range manscdp.ConfigTypeOrder {
				if configType != manscdp.ConfigTypeBasicParam {
					configTypes = append(configTypes, configType)
				}
			}
		default:
			return OperationResult{}, ErrCapabilityNotSupported
		}
	}
	normalized, err := normalizeConfigTypes(configTypes, string(target.Profile.Version))
	if err != nil {
		return OperationResult{}, err
	}
	if hasType(normalized, manscdp.ConfigTypeBasicParam) && len(normalized) != 1 {
		return OperationResult{}, ErrInvalidRequest
	}
	operation, err := s.commands.ReadDeviceConfigs(ctx, target, normalized, 0, request.Scope.OwnerDeptID, clientIdempotencyKey(request.ClientID, request.IdempotencyKey))
	if err != nil {
		return OperationResult{}, normalizeCommandError(err)
	}
	return operationResult(operation, nil), nil
}

func (s *Service) Apply(ctx context.Context, request Request, blocks manscdp.DeviceConfigBlocks) (OperationResult, error) {
	if !s.ready() || ctx == nil || !validRequest(request, true) || blocks.IsEmpty() {
		return OperationResult{}, ErrInvalidRequest
	}
	if err := manscdp.ValidateDeviceConfigBlocks(blocks); err != nil {
		return OperationResult{}, ErrInvalidRequest
	}
	target, device, _, err := s.resolveTarget(ctx, request)
	if err != nil {
		return OperationResult{}, err
	}
	if err = requireOnline(target); err != nil {
		return OperationResult{}, err
	}
	types := blocks.PresentConfigTypes()
	if err = validateConfigTypeVersion(types, string(target.Profile.Version)); err != nil {
		return OperationResult{}, err
	}
	for _, configType := range types {
		if _, readOnly := manscdp.ReadOnlyDeviceConfigTypeReason(configType); readOnly || configType == manscdp.ConfigTypeSnapShotConfig {
			return OperationResult{}, ErrCapabilityNotSupported
		}
	}
	if err = s.requireObservedConfigSupport(ctx, uint(device.ID), request, types); err != nil {
		return OperationResult{}, err
	}
	operation, err := s.commands.ApplyDeviceConfig(ctx, target, blocks, 0, request.Scope.OwnerDeptID, clientIdempotencyKey(request.ClientID, request.IdempotencyKey))
	if err != nil {
		return OperationResult{}, normalizeCommandError(err)
	}
	return operationResult(operation, nil), nil
}

func (s *Service) RefreshVideoParams(ctx context.Context, request Request) (OperationResult, error) {
	if !s.ready() || ctx == nil || !validRequest(request, true) {
		return OperationResult{}, ErrInvalidRequest
	}
	target, _, _, err := s.resolveTarget(ctx, request)
	if err != nil {
		return OperationResult{}, err
	}
	if target.Profile.Version != protocol.Version2022 {
		return OperationResult{}, ErrCapabilityNotSupported
	}
	if err = requireOnline(target); err != nil {
		return OperationResult{}, err
	}
	operation, err := s.commands.ReadVideoParams(ctx, target, 0, request.Scope.OwnerDeptID, clientIdempotencyKey(request.ClientID, request.IdempotencyKey))
	if err != nil {
		return OperationResult{}, normalizeCommandError(err)
	}
	return operationResult(operation, nil), nil
}

func (s *Service) ApplyVideoParams(ctx context.Context, request Request, items []manscdp.VideoParamItem) (OperationResult, error) {
	if !s.ready() || ctx == nil || !validRequest(request, true) || manscdp.ValidateVideoParamItems(items) != nil {
		return OperationResult{}, ErrInvalidRequest
	}
	target, device, channel, err := s.resolveTarget(ctx, request)
	if err != nil {
		return OperationResult{}, err
	}
	if target.Profile.Version != protocol.Version2022 {
		return OperationResult{}, ErrCapabilityNotSupported
	}
	if err = requireOnline(target); err != nil {
		return OperationResult{}, err
	}
	var configSupport int64
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbDeviceConfig{}).
		Where("device_id = ? AND target_code = ? AND config_type = ?", device.ID, channel.ChannelID, manscdp.ConfigTypeVideoParamOpt).Count(&configSupport).Error; err != nil {
		return OperationResult{}, ErrUnavailable
	}
	var videoParamSupport int64
	if err := s.db.WithContext(ctx).Model(&gbmodels.GbDeviceVideoParam{}).
		Where("device_id = ? AND target_code = ?", device.ID, channel.ChannelID).Count(&videoParamSupport).Error; err != nil {
		return OperationResult{}, ErrUnavailable
	}
	if configSupport == 0 && videoParamSupport == 0 {
		return OperationResult{}, ErrCapabilityUnknown
	}
	operation, err := s.commands.ApplyVideoParams(ctx, target, items, 0, request.Scope.OwnerDeptID, clientIdempotencyKey(request.ClientID, request.IdempotencyKey))
	if err != nil {
		return OperationResult{}, normalizeCommandError(err)
	}
	return operationResult(operation, nil), nil
}

func (s *Service) GetConfigSnapshot(ctx context.Context, request Request) (ConfigSnapshot, error) {
	if !s.ready() || ctx == nil || !validRequest(request, false) {
		return ConfigSnapshot{}, ErrInvalidRequest
	}
	target, device, channel, err := s.resolveTarget(ctx, request)
	if err != nil {
		return ConfigSnapshot{}, err
	}
	result := ConfigSnapshot{DeviceID: request.DeviceID, ChannelID: request.ChannelID, ProtocolVersion: string(target.Profile.Version), Freshness: "unknown", Items: []ConfigEntry{}}
	rows := make([]gbmodels.GbDeviceConfig, 0)
	if err := s.db.WithContext(ctx).Where("device_id = ? AND target_code IN ?", device.ID, []string{request.DeviceID, channel.ChannelID}).Order("config_type, target_code").Find(&rows).Error; err != nil {
		return ConfigSnapshot{}, ErrUnavailable
	}
	for _, row := range rows {
		payload, err := sanitizeConfigPayload(row.ConfigType, row.PayloadJSON)
		if err != nil {
			return ConfigSnapshot{}, ErrUnavailable
		}
		kind := "channel"
		if row.TargetCode == request.DeviceID {
			kind = "device"
		}
		freshness := freshnessAt(s.now(), row.ObservedAt)
		result.Items = append(result.Items, ConfigEntry{ConfigType: row.ConfigType, TargetCode: row.TargetCode, TargetKind: kind, Config: payload, Freshness: freshness, ObservedAt: row.ObservedAt})
		if result.ObservedAt == nil || row.ObservedAt.After(*result.ObservedAt) {
			observedAt := row.ObservedAt
			result.ObservedAt = &observedAt
		}
	}
	if result.ObservedAt != nil {
		result.Freshness = freshnessAt(s.now(), *result.ObservedAt)
	}
	return result, nil
}

func (s *Service) GetVideoParamSnapshot(ctx context.Context, request Request) (VideoParamSnapshot, error) {
	if !s.ready() || ctx == nil || !validRequest(request, false) {
		return VideoParamSnapshot{}, ErrInvalidRequest
	}
	target, device, channel, err := s.resolveTarget(ctx, request)
	if err != nil {
		return VideoParamSnapshot{}, err
	}
	result := VideoParamSnapshot{DeviceID: request.DeviceID, ChannelID: request.ChannelID, ProtocolVersion: string(target.Profile.Version), Freshness: "unknown", Items: []manscdp.VideoParamItem{}}
	rows := make([]gbmodels.GbDeviceVideoParam, 0)
	if err := s.db.WithContext(ctx).Where("device_id = ? AND target_code = ?", device.ID, channel.ChannelID).Order("stream_number").Find(&rows).Error; err != nil {
		return VideoParamSnapshot{}, ErrUnavailable
	}
	for _, row := range rows {
		result.Items = append(result.Items, manscdp.VideoParamItem{StreamNumber: row.StreamNumber, VideoFormat: row.VideoFormat, Resolution: row.Resolution, FrameRate: row.FrameRate, BitRateType: row.BitRateType, VideoBitRate: row.VideoBitRate})
		if result.ObservedAt == nil || row.ObservedAt.After(*result.ObservedAt) {
			observedAt := row.ObservedAt
			result.ObservedAt = &observedAt
		}
	}
	if result.ObservedAt != nil {
		result.Freshness = freshnessAt(s.now(), *result.ObservedAt)
	}
	return result, nil
}

func (s *Service) GetOperation(ctx context.Context, request Request, operationID string) (OperationResult, error) {
	if !s.ready() || ctx == nil || !validRequest(request, false) || !validOperationID(operationID) {
		return OperationResult{}, ErrNotFound
	}
	if _, _, _, err := s.resolveTarget(ctx, request); err != nil {
		return OperationResult{}, err
	}
	operation, err := s.commands.GetOperation(ctx, operationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OperationResult{}, ErrNotFound
		}
		return OperationResult{}, ErrUnavailable
	}
	if !strings.HasPrefix(operation.IdempotencyKey, clientIdempotencyPrefix(request.ClientID)) || operation.DeviceCode != request.DeviceID || operation.ChannelCode != request.ChannelID {
		return OperationResult{}, ErrNotFound
	}
	var reconcile *gbmodels.GbPTZOperation
	if operation.ReconcileOperationID != nil && strings.TrimSpace(*operation.ReconcileOperationID) != "" {
		child, childErr := s.commands.GetOperation(ctx, strings.TrimSpace(*operation.ReconcileOperationID))
		if childErr == nil && child.TriggerOperationID != nil && strings.TrimSpace(*child.TriggerOperationID) == operation.OperationID {
			reconcile = &child
		}
	}
	return operationResult(operation, reconcile), nil
}

func (s *Service) ready() bool { return s != nil && s.db != nil && s.commands != nil }

func (s *Service) resolveTarget(ctx context.Context, request Request) (runtimeptz.Target, gbmodels.GbDevice, gbmodels.GbChannel, error) {
	if !validGBCode(request.DeviceID) || !validGBCode(request.ChannelID) {
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrNotFound
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, s.db, request.Scope)
	if err != nil {
		if errors.Is(err, resource.ErrResourceNotFound) || errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrNotFound
		}
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	var device gbmodels.GbDevice
	query := s.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_device d2 WHERE d2.device_id = gb_device.device_id AND d2.deleted_at IS NULL AND d2.id <> gb_device.id)").Limit(1).Find(&device)
	if query.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	if query.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrNotFound
	}
	var channel gbmodels.GbChannel
	query = s.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, request.ChannelID, device.OwnerDeptID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_channel c2 WHERE c2.device_id = gb_channel.device_id AND c2.channel_id = gb_channel.channel_id AND c2.deleted_at IS NULL AND c2.id <> gb_channel.id)").Limit(1).Find(&channel)
	if query.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrUnavailable
	}
	if query.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrNotFound
	}
	var profile protocol.Profile
	switch strings.TrimSpace(device.EffectiveVersion) {
	case string(protocol.Version2016):
		profile = protocol.ProfileFor(protocol.Version2016)
	case string(protocol.Version2022):
		profile = protocol.ProfileFor(protocol.Version2022)
	default:
		return runtimeptz.Target{}, gbmodels.GbDevice{}, gbmodels.GbChannel{}, ErrCapabilityNotSupported
	}
	target := runtimeptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID, ChannelID: channel.ID, ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: device.Status == gbmodels.DeviceStatusOnline,
		ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline, Profile: profile,
	}
	return target, device, channel, nil
}

func (s *Service) requireObservedConfigSupport(ctx context.Context, deviceID uint, request Request, types []string) error {
	for _, configType := range types {
		codes := []string{request.ChannelID}
		if configType == manscdp.ConfigTypeBasicParam {
			codes = append(codes, request.DeviceID)
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&gbmodels.GbDeviceConfig{}).
			Where("device_id = ? AND target_code IN ? AND config_type = ?", deviceID, codes, configType).Count(&count).Error; err != nil {
			return ErrUnavailable
		}
		if count == 0 {
			return ErrCapabilityUnknown
		}
	}
	return nil
}

func requireOnline(target runtimeptz.Target) error {
	if !target.DeviceOnline || !target.ChannelOnline {
		return ErrDeviceOffline
	}
	return nil
}

func normalizeConfigTypes(configTypes []string, version string) ([]string, error) {
	if len(configTypes) == 0 || len(configTypes) > len(manscdp.ConfigTypeOrder) {
		return nil, ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(configTypes))
	for _, value := range configTypes {
		value = strings.TrimSpace(value)
		known := false
		for _, candidate := range manscdp.ConfigTypeOrder {
			if candidate == value {
				known = true
				break
			}
		}
		if !known {
			return nil, ErrInvalidRequest
		}
		seen[value] = struct{}{}
	}
	normalized := make([]string, 0, len(seen))
	for _, configType := range manscdp.ConfigTypeOrder {
		if _, ok := seen[configType]; ok {
			normalized = append(normalized, configType)
		}
	}
	if err := validateConfigTypeVersion(normalized, version); err != nil {
		return nil, err
	}
	return normalized, nil
}

func validateConfigTypeVersion(configTypes []string, version string) error {
	if version != string(protocol.Version2016) && version != string(protocol.Version2022) {
		return ErrCapabilityNotSupported
	}
	if version == string(protocol.Version2016) {
		for _, configType := range configTypes {
			if configType != manscdp.ConfigTypeBasicParam {
				return ErrCapabilityNotSupported
			}
		}
	}
	return nil
}

func operationResult(operation gbmodels.GbPTZOperation, child *gbmodels.GbPTZOperation) OperationResult {
	result := OperationResult{
		OperationID: operation.OperationID, Action: operation.Action, Status: string(operation.Status), SN: operation.SN,
		ProtocolVersion: operation.ProfileVersion, DeviceAcknowledged: operation.ResponseAt != nil || operation.Status == gbmodels.PTZOperationAccepted || operation.Status == gbmodels.PTZOperationRejected,
		DeviceResult: operation.DeviceResult, ErrorCode: operation.ErrorCode, ResponseAt: operation.ResponseAt,
		ResponseHasData: operation.ResponseHasData, Reconciliation: Reconciliation{State: "not_applicable"},
	}
	if operation.Action == runtimeptz.ActionApplyDeviceConfig || operation.Action == "apply_video_params" {
		result.Reconciliation = Reconciliation{State: "pending"}
		if child != nil {
			state := runtimeptz.DeriveDeviceConfigReconcileState(child)
			if operation.Action == "apply_video_params" {
				state = runtimeptz.DeriveVideoParamReconcileState(child)
			}
			result.Reconciliation = Reconciliation{
				State: state.State, OperationID: child.OperationID, Status: string(child.Status),
				ResponseHasData: child.ResponseHasData, ErrorCode: child.ErrorCode, CompletedAt: child.CompletedAt,
			}
		}
	} else if operation.Action == runtimeptz.ActionRefreshDeviceConfigs || operation.Action == "refresh_video_params" {
		result.Reconciliation = Reconciliation{State: "pending", OperationID: operation.OperationID, Status: string(operation.Status), ResponseHasData: operation.ResponseHasData, ErrorCode: operation.ErrorCode, CompletedAt: operation.CompletedAt}
		if operation.Status == gbmodels.PTZOperationAccepted {
			state := runtimeptz.DeriveDeviceConfigReconcileState(&operation)
			if operation.Action == "refresh_video_params" {
				state = runtimeptz.DeriveVideoParamReconcileState(&operation)
			}
			result.Reconciliation.State = state.State
		}
	}
	return result
}

func normalizeCommandError(err error) error {
	if err == nil {
		return nil
	}
	var operationErr *runtimeptz.OperationError
	if errors.As(err, &operationErr) {
		switch operationErr.Code {
		case runtimeptz.ErrorCodeHomePositionNotFound:
			return ErrNotFound
		case runtimeptz.ErrorCodeHomePositionInvalidArgument:
			return ErrInvalidRequest
		case runtimeptz.ErrorCodeHomePositionDeviceOffline:
			return ErrDeviceOffline
		case runtimeptz.ErrorCodeHomePositionIdempotencyConflict:
			return ErrConflict
		default:
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}

func clientIdempotencyKey(clientID int64, value string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", clientID, value)))
	return fmt.Sprintf("openapi:%d:%s", clientID, hex.EncodeToString(digest[:]))
}

func clientIdempotencyPrefix(clientID int64) string { return fmt.Sprintf("openapi:%d:", clientID) }

func validRequest(request Request, requireIdempotency bool) bool {
	if request.ClientID <= 0 || request.Scope.OwnerDeptID == 0 || (request.Scope.DataScope != resource.DataScopeDepartment && request.Scope.DataScope != resource.DataScopeDepartmentAndChildren) ||
		!validGBCode(request.DeviceID) || !validGBCode(request.ChannelID) {
		return false
	}
	return !requireIdempotency || validIdempotencyKey(request.IdempotencyKey)
}

func validIdempotencyKey(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for i := range value {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func validGBCode(value string) bool {
	if len(value) != 20 || !utf8.ValidString(value) {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func validOperationID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("-_.", char)) {
			return false
		}
	}
	return true
}

func hasType(types []string, wanted string) bool {
	for _, value := range types {
		if value == wanted {
			return true
		}
	}
	return false
}

func freshnessAt(now, observedAt time.Time) string {
	if observedAt.IsZero() {
		return "unknown"
	}
	if now.Sub(observedAt) < 0 || now.Sub(observedAt) <= configFreshFor {
		return "fresh"
	}
	return "stale"
}

func sanitizeConfigPayload(configType, payload string) (json.RawMessage, error) {
	var result any
	switch configType {
	case manscdp.ConfigTypeBasicParam:
		var value manscdp.BasicParamBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeVideoParamOpt:
		var value manscdp.VideoParamOptBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeVideoRecordPlan:
		var value manscdp.VideoRecordPlanBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = struct {
			RecordEnable int                      `json:"recordEnable"`
			Schedules    []manscdp.RecordSchedule `json:"schedules"`
			StreamNumber int                      `json:"streamNumber"`
		}{value.RecordEnable, value.Schedules, value.StreamNumber}
	case manscdp.ConfigTypeVideoAlarmRecord:
		var value manscdp.VideoAlarmRecordBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = struct {
			RecordEnable  int  `json:"recordEnable"`
			RecordTime    *int `json:"recordTime,omitempty"`
			PreRecordTime *int `json:"preRecordTime,omitempty"`
			StreamNumber  int  `json:"streamNumber"`
		}{value.RecordEnable, value.RecordTime, value.PreRecordTime, value.StreamNumber}
	case manscdp.ConfigTypePictureMask:
		var value manscdp.PictureMaskBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeFrameMirror:
		var value manscdp.FrameMirrorBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeAlarmReport:
		var value manscdp.AlarmReportBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeOSDConfig:
		var value manscdp.OSDConfigBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = value
	case manscdp.ConfigTypeSnapShotConfig:
		var value manscdp.SnapShotBlock
		if err := json.Unmarshal([]byte(payload), &value); err != nil {
			return nil, err
		}
		result = struct {
			SnapNum  *int `json:"snapNum,omitempty"`
			Interval *int `json:"interval,omitempty"`
		}{value.SnapNum, value.Interval}
	default:
		return nil, fmt.Errorf("unsupported device config type %q", configType)
	}
	encoded, err := json.Marshal(result)
	return encoded, err
}

var _ = url.URL{}

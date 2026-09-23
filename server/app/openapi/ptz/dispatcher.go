package ptz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

type Dispatcher struct {
	db      *gorm.DB
	service *runtimeptz.Service
}

func NewDispatcher(db *gorm.DB, service *runtimeptz.Service) *Dispatcher {
	return &Dispatcher{db: db, service: service}
}

func (d *Dispatcher) Ready() bool {
	return d != nil && d.db != nil && d.service != nil
}

type presetInput struct {
	PresetID int    `json:"presetId"`
	Name     string `json:"name"`
}

type presetView struct {
	PresetID        int    `json:"presetId"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	LastOperationID string `json:"lastOperationId,omitempty"`
}

type operationView struct {
	OperationID  string `json:"operationId"`
	Status       string `json:"status"`
	Action       string `json:"action"`
	SN           int    `json:"sn"`
	SIPStatus    int    `json:"sipStatus,omitempty"`
	DeviceResult string `json:"deviceResult,omitempty"`
	DeviceError  string `json:"deviceError,omitempty"`
}

func (d *Dispatcher) Handle(ctx context.Context, request auth.PTZRequest) (any, error) {
	if !d.Ready() {
		return nil, auth.ErrPTZUnavailable
	}
	switch request.Scope {
	case auth.PTZPresetListScope:
		return d.listPresets(ctx, request)
	case auth.PTZPresetSaveScope:
		return d.savePreset(ctx, request)
	case auth.PTZPresetCallScope:
		return d.executePreset(ctx, request, manscdp.PTZActionCallPreset)
	case auth.PTZPresetDeleteScope:
		return d.executePreset(ctx, request, manscdp.PTZActionDeletePreset)
	case auth.PTZOperationReadScope:
		return d.getOperation(ctx, request)
	default:
		return nil, auth.ErrPTZInvalid
	}
}

func (d *Dispatcher) resolveTarget(ctx context.Context, request auth.PTZRequest) (runtimeptz.Target, gbmodels.GbChannel, error) {
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, d.db, resource.DepartmentScope{OwnerDeptID: request.OwnerDeptID, DataScope: request.DataScope})
	if err != nil {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, err
	}
	var device gbmodels.GbDevice
	result := d.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_device d2 WHERE d2.device_id = gb_device.device_id AND d2.deleted_at IS NULL AND d2.id <> gb_device.id)").Limit(1).Find(&device)
	if result.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, result.Error
	}
	if result.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, auth.ErrPTZNotFound
	}
	var channel gbmodels.GbChannel
	result = d.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, request.ChannelID, device.OwnerDeptID, departmentIDs).
		Where("NOT EXISTS (SELECT 1 FROM gb_channel c2 WHERE c2.device_id = gb_channel.device_id AND c2.channel_id = gb_channel.channel_id AND c2.deleted_at IS NULL AND c2.id <> gb_channel.id)").Limit(1).Find(&channel)
	if result.Error != nil {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, result.Error
	}
	if result.RowsAffected != 1 {
		return runtimeptz.Target{}, gbmodels.GbChannel{}, auth.ErrPTZNotFound
	}
	profile := protocol.ProfileFor(protocol.Version2016)
	if strings.TrimSpace(device.EffectiveVersion) == protocol.Version2022 {
		profile = protocol.ProfileFor(protocol.Version2022)
	}
	target := runtimeptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID,
		ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline:  device.Status == gbmodels.DeviceStatusOnline,
		ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline,
		Profile:       profile,
	}
	if gbconfig.CurrentPlayAuthSettings().RequiredByOpenAPI {
		state, err := playauth.NewDeviceSecurityStore(d.db).Load(ctx, device.DeviceID)
		if err != nil || state.CleanupCompletedEpoch != state.AccessEpoch {
			return runtimeptz.Target{}, gbmodels.GbChannel{}, auth.ErrPTZUnavailable
		}
		target.DeviceEpoch = state.AccessEpoch
	}
	return target, channel, nil
}

func (d *Dispatcher) listPresets(ctx context.Context, request auth.PTZRequest) (any, error) {
	_, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	var rows []gbmodels.GbPTZPreset
	if err := d.db.WithContext(ctx).Where("channel_id = ? AND status <> ?", channel.ID, gbmodels.PTZPresetDeleted).Order("preset_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]presetView, 0, len(rows))
	for _, row := range rows {
		items = append(items, presetView{PresetID: row.PresetID, Name: row.Name, Status: string(row.Status), LastOperationID: row.LastOperationID})
	}
	return map[string]any{"items": items}, nil
}

func decodePreset(body []byte) (presetInput, error) {
	var input presetInput
	if !utf8.Valid(body) || len(body) == 0 {
		return input, auth.ErrPTZInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, auth.ErrPTZInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return input, auth.ErrPTZInvalid
	}
	if input.PresetID < 1 || input.PresetID > 255 || utf8.RuneCountInString(input.Name) > 255 {
		return input, auth.ErrPTZInvalid
	}
	return input, nil
}

func (d *Dispatcher) savePreset(ctx context.Context, request auth.PTZRequest) (any, error) {
	input, err := decodePreset(request.Body)
	if err != nil {
		return nil, err
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(manscdp.PTZActionSetPreset), IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"action": string(manscdp.PTZActionSetPreset), "id": input.PresetID, "name": input.Name},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: manscdp.PTZActionSetPreset, ID: input.PresetID})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return operationResponse(operation), nil
}

func (d *Dispatcher) executePreset(ctx context.Context, request auth.PTZRequest, action manscdp.PTZExtendedAction) (any, error) {
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	var presetID int
	if _, err := fmtSscanfDecimal(request.PresetID, &presetID); err != nil || presetID < 1 || presetID > 255 {
		return nil, auth.ErrPTZInvalid
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"action": string(action), "id": presetID},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: action, ID: presetID})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return operationResponse(operation), nil
}

func (d *Dispatcher) getOperation(ctx context.Context, request auth.PTZRequest) (any, error) {
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.GetOperation(ctx, request.OperationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, auth.ErrPTZNotFound
	}
	if err != nil {
		return nil, err
	}
	if operation.DeviceID != target.DeviceID || operation.ChannelID != channel.ID || operation.DeviceCode != target.DeviceCode || operation.ChannelCode != channel.ChannelID {
		return nil, auth.ErrPTZNotFound
	}
	return operationResponse(operation), nil
}

func operationResponse(operation gbmodels.GbPTZOperation) operationView {
	return operationView{OperationID: operation.OperationID, Status: string(operation.Status), Action: operation.Action, SN: operation.SN, SIPStatus: operation.SIPStatus, DeviceResult: operation.DeviceResult, DeviceError: operation.DeviceError}
}

func mapTargetError(err error) error {
	if errors.Is(err, resource.ErrResourceNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.ErrPTZNotFound
	}
	return err
}

func mapOperationError(err error) error {
	var operationErr *runtimeptz.OperationError
	if errors.As(err, &operationErr) {
		switch operationErr.Code {
		case runtimeptz.ErrorCodeHomePositionInvalidArgument:
			return auth.ErrPTZInvalid
		case runtimeptz.ErrorCodeHomePositionIdempotencyConflict:
			return auth.ErrPTZConflict
		case runtimeptz.ErrorCodeHomePositionNotFound:
			return auth.ErrPTZNotFound
		}
	}
	return err
}

// Kept local to avoid accepting strconv's permissive plus sign or whitespace.
func fmtSscanfDecimal(value string, target *int) (int, error) {
	if value == "" {
		return 0, errors.New("empty decimal")
	}
	n := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("invalid decimal")
		}
		n = n*10 + int(char-'0')
		if n > 255 {
			return 0, errors.New("decimal overflow")
		}
	}
	*target = n
	return 1, nil
}

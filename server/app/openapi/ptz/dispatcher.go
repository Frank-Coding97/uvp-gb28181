package ptz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
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

type controlOperationView struct {
	OperationID        string `json:"operationId"`
	Status             string `json:"status"`
	Action             string `json:"action"`
	SN                 int    `json:"sn"`
	Result             string `json:"result"`
	DeviceAcknowledged bool   `json:"deviceAcknowledged"`
}

type moveInput struct {
	Action string `json:"action"`
	Speed  *int   `json:"speed"`
}

type preciseInput struct {
	Pan  *float64 `json:"pan"`
	Tilt *float64 `json:"tilt"`
	Zoom *float64 `json:"zoom"`
}

type extendedInput struct {
	Action   string `json:"action"`
	ID       *int   `json:"id"`
	PresetID *int   `json:"presetId"`
	Value    *int   `json:"value"`
}

type wiperInput struct {
	Action string `json:"action"`
}

type homePositionInput struct {
	Enabled   *bool `json:"enabled"`
	ResetTime *int  `json:"resetTime"`
	PresetID  *int  `json:"presetId"`
}

type cruiseControlInput struct {
	Action  string `json:"action"`
	TrackID *int   `json:"trackId"`
}

type cruiseTrackStopInput struct {
	PresetID int `json:"presetId"`
}

type cruiseTrackCreateInput struct {
	TrackID         *int                   `json:"trackId"`
	Name            string                 `json:"name"`
	Speed           int                    `json:"speed"`
	DwellSec        int                    `json:"dwellSec"`
	Stops           []cruiseTrackStopInput `json:"stops"`
	ReplaceExisting bool                   `json:"replaceExisting"`
	Confirmed       *bool                  `json:"confirmed"`
}

type cruiseTrackCreateStep struct {
	Step     string
	Sequence int
	Action   manscdp.PTZExtendedAction
	Command  manscdp.PTZExtendedCommand
}

type cruiseTrackPointView struct {
	PresetIndex int `json:"presetIndex"`
	StayTime    int `json:"stayTime"`
	Speed       int `json:"speed"`
}

type cruiseTrackDetailView struct {
	SumNum       *int                   `json:"sumNum,omitempty"`
	CruisePoints []cruiseTrackPointView `json:"cruisePoints"`
}

type cruiseTrackView struct {
	TrackID    int                    `json:"trackId"`
	Name       string                 `json:"name"`
	Enabled    *bool                  `json:"enabled"`
	Detail     *cruiseTrackDetailView `json:"detail,omitempty"`
	Source     string                 `json:"source"`
	DeviceTime *time.Time             `json:"deviceTime"`
	ObservedAt *time.Time             `json:"observedAt,omitempty"`
}

type targetTrackAreaInput struct {
	Length    int `json:"length"`
	Width     int `json:"width"`
	MidPointX int `json:"midPointX"`
	MidPointY int `json:"midPointY"`
	LengthX   int `json:"lengthX"`
	LengthY   int `json:"lengthY"`
}

type targetTrackInput struct {
	Mode      string                `json:"mode"`
	DeviceID2 string                `json:"deviceId2"`
	Area      *targetTrackAreaInput `json:"area"`
}

func (d *Dispatcher) Handle(ctx context.Context, request auth.PTZRequest) (any, error) {
	if !d.Ready() {
		return nil, auth.ErrPTZUnavailable
	}
	if isControlScope(request.Scope) && !validControlIdempotencyKey(request.IdempotencyKey) {
		return nil, auth.ErrPTZInvalid
	}
	switch request.Scope {
	case auth.PTZMoveScope:
		return d.move(ctx, request)
	case auth.PTZPreciseScope:
		return d.precise(ctx, request)
	case auth.PTZExtendedScope:
		return d.extended(ctx, request)
	case auth.PTZWiperScope:
		return d.wiper(ctx, request)
	case auth.PTZHomePositionSetScope:
		return d.setHomePosition(ctx, request)
	case auth.PTZCruiseControlScope:
		return d.controlCruise(ctx, request)
	case auth.PTZCruiseTrackCreateScope:
		return d.createCruiseTrack(ctx, request)
	case auth.PTZCruiseTrackReadScope:
		return d.getCruiseTrack(ctx, request)
	case auth.PTZTargetTrackScope:
		return d.trackTarget(ctx, request)
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

func isControlScope(scope string) bool {
	switch scope {
	case auth.PTZMoveScope, auth.PTZPreciseScope, auth.PTZExtendedScope, auth.PTZWiperScope, auth.PTZHomePositionSetScope, auth.PTZCruiseControlScope, auth.PTZCruiseTrackCreateScope:
		return true
	default:
		return false
	}
}

func validControlIdempotencyKey(value string) bool {
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
	return target, channel, nil
}

func decodeStrictBody(body []byte, output any, allowedFields ...string) error {
	if !utf8.Valid(body) || len(body) == 0 {
		return auth.ErrPTZInvalid
	}
	check := json.NewDecoder(bytes.NewReader(body))
	check.UseNumber()
	if err := validateUniqueJSONValue(check); err != nil {
		return auth.ErrPTZInvalid
	}
	if token, err := check.Token(); err != io.EOF || token != nil {
		return auth.ErrPTZInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return auth.ErrPTZInvalid
	}
	allowed := make(map[string]struct{}, len(allowedFields))
	for _, field := range allowedFields {
		allowed[field] = struct{}{}
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return auth.ErrPTZInvalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return auth.ErrPTZInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return auth.ErrPTZInvalid
	}
	return nil
}

func validateUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return auth.ErrPTZInvalid
			}
			if _, exists := seen[key]; exists {
				return auth.ErrPTZInvalid
			}
			seen[key] = struct{}{}
			if err := validateUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return auth.ErrPTZInvalid
		}
	case '[':
		for decoder.More() {
			if err := validateUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return auth.ErrPTZInvalid
		}
	default:
		return auth.ErrPTZInvalid
	}
	return nil
}

func (d *Dispatcher) move(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input moveInput
	if err := decodeStrictBody(request.Body, &input, "action", "speed"); err != nil || input.Speed == nil {
		return nil, auth.ErrPTZInvalid
	}
	action, err := manscdp.ParsePTZAction(input.Action)
	if err != nil || *input.Speed < 0 || *input.Speed > 255 || (*input.Speed == 0 && !action.Speedless()) {
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"action": action, "speed": *input.Speed},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildPTZControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZCommand{Action: action, Speed: *input.Speed})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func (d *Dispatcher) precise(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input preciseInput
	if err := decodeStrictBody(request.Body, &input, "pan", "tilt", "zoom"); err != nil || (input.Pan == nil && input.Tilt == nil && input.Zoom == nil) {
		return nil, auth.ErrPTZInvalid
	}
	if !validPreciseCoordinate("pan", input.Pan) || !validPreciseCoordinate("tilt", input.Tilt) || !validPreciseCoordinate("zoom", input.Zoom) {
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	if !target.Profile.SupportsPrecisePTZ() {
		return nil, auth.ErrPTZUnsupported
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: "precise", IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"pan": input.Pan, "tilt": input.Tilt, "zoom": input.Zoom},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildPTZPreciseDeviceControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZPreciseControl{Pan: input.Pan, Tilt: input.Tilt, Zoom: input.Zoom})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func validPreciseCoordinate(axis string, value *float64) bool {
	if value == nil {
		return true
	}
	switch axis {
	case "pan":
		return *value >= -360 && *value <= 360
	case "tilt":
		return *value >= -180 && *value <= 180
	case "zoom":
		return *value >= 0 && *value <= 100000
	default:
		return false
	}
}

func (d *Dispatcher) extended(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input extendedInput
	if err := decodeStrictBody(request.Body, &input, "action", "id", "presetId", "value"); err != nil || input.ID == nil {
		return nil, auth.ErrPTZInvalid
	}
	action := manscdp.PTZExtendedAction(input.Action)
	if !isOpenAPIExtendedAction(action) || *input.ID < 0 || *input.ID > 255 {
		return nil, auth.ErrPTZInvalid
	}
	command := manscdp.PTZExtendedCommand{Action: action, ID: *input.ID}
	payload := map[string]any{"action": action, "id": *input.ID}
	switch action {
	case manscdp.PTZActionScanStart:
		if input.PresetID != nil || input.Value != nil {
			return nil, auth.ErrPTZInvalid
		}
	case manscdp.PTZActionScanSetSpeed, manscdp.PTZActionCruiseSetSpeed, manscdp.PTZActionCruiseSetDwell:
		if input.PresetID != nil || input.Value == nil || *input.Value < 1 || *input.Value > 4095 {
			return nil, auth.ErrPTZInvalid
		}
		command.Value16 = *input.Value
		payload["value"] = *input.Value
	case manscdp.PTZActionCruiseAddStop:
		if input.PresetID == nil || *input.PresetID < 1 || *input.PresetID > 255 || input.Value != nil {
			return nil, auth.ErrPTZInvalid
		}
		command.SubID = *input.PresetID
		payload["presetId"] = *input.PresetID
	case manscdp.PTZActionCruiseDeleteStop:
		if input.PresetID == nil || *input.PresetID < 0 || *input.PresetID > 255 || input.Value != nil {
			return nil, auth.ErrPTZInvalid
		}
		command.SubID = *input.PresetID
		payload["presetId"] = *input.PresetID
	default:
		if input.PresetID != nil || input.Value != nil {
			return nil, auth.ErrPTZInvalid
		}
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: payload,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, command)
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func isOpenAPIExtendedAction(action manscdp.PTZExtendedAction) bool {
	switch action {
	case manscdp.PTZActionScanStart, manscdp.PTZActionScanStop, manscdp.PTZActionScanSetLeft, manscdp.PTZActionScanSetRight,
		manscdp.PTZActionScanSetSpeed, manscdp.PTZActionCruiseAddStop, manscdp.PTZActionCruiseDeleteStop,
		manscdp.PTZActionCruiseSetSpeed, manscdp.PTZActionCruiseSetDwell:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) wiper(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input wiperInput
	if err := decodeStrictBody(request.Body, &input, "action"); err != nil {
		return nil, err
	}
	var action manscdp.PTZExtendedAction
	operationAction := ""
	switch strings.TrimSpace(input.Action) {
	case "on":
		action, operationAction = manscdp.PTZActionAuxOn, "wiper_on"
	case "off":
		action, operationAction = manscdp.PTZActionAuxOff, "wiper_off"
	default:
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: operationAction, IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"action": operationAction, "id": manscdp.PTZAuxiliaryIDWiper},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: action, ID: manscdp.PTZAuxiliaryIDWiper})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func (d *Dispatcher) setHomePosition(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input homePositionInput
	if err := decodeStrictBody(request.Body, &input, "enabled", "resetTime", "presetId"); err != nil || input.Enabled == nil {
		return nil, auth.ErrPTZInvalid
	}
	var resetTime, presetID *int
	if *input.Enabled {
		if input.ResetTime == nil || *input.ResetTime < 10 || *input.ResetTime > 3600 || input.PresetID == nil || *input.PresetID < 0 || *input.PresetID > 255 {
			return nil, auth.ErrPTZInvalid
		}
		resetTime, presetID = input.ResetTime, input.PresetID
	} else if input.ResetTime != nil || input.PresetID != nil {
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	payload := map[string]any{"enabled": *input.Enabled}
	if resetTime != nil {
		payload["resetTime"] = *resetTime
		payload["presetId"] = *presetID
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: "home_position", IdempotencyKey: request.IdempotencyKey,
		ResponseRequired: true, MaxAttempts: 1, Profile: target.Profile, Payload: payload,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.HomePositionControl{Enabled: *input.Enabled, ResetTime: resetTime, PresetIndex: presetID})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func (d *Dispatcher) controlCruise(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input cruiseControlInput
	if err := decodeStrictBody(request.Body, &input, "action", "trackId"); err != nil || input.TrackID == nil || *input.TrackID < 0 || *input.TrackID > 255 {
		return nil, auth.ErrPTZInvalid
	}
	var action manscdp.PTZExtendedAction
	switch input.Action {
	case "start":
		action = manscdp.PTZActionCruiseStart
	case "stop":
		action = manscdp.PTZActionCruiseStop
	case "delete":
		action = manscdp.PTZActionCruiseDeletePath
	default:
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	operation, err := d.service.Execute(ctx, target, runtimeptz.Command{
		CmdType: manscdp.CmdDeviceControl, Action: string(action), IdempotencyKey: request.IdempotencyKey,
		Profile: target.Profile, Payload: map[string]any{"action": action, "id": *input.TrackID},
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, manscdp.PTZExtendedCommand{Action: action, ID: *input.TrackID})
		},
	})
	if err != nil {
		return nil, mapOperationError(err)
	}
	return controlOperationResponse(operation), nil
}

func (d *Dispatcher) createCruiseTrack(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input cruiseTrackCreateInput
	if err := decodeStrictBody(request.Body, &input, "trackId", "name", "speed", "dwellSec", "stops", "replaceExisting", "confirmed"); err != nil {
		return nil, auth.ErrPTZInvalid
	}
	if input.TrackID == nil || *input.TrackID < 0 || *input.TrackID > 255 || len(input.Stops) == 0 || len(input.Stops) > 32 ||
		input.Speed < 0 || input.Speed > 4095 || input.DwellSec < 0 || input.DwellSec > 4095 ||
		utf8.RuneCountInString(input.Name) > 255 || input.ReplaceExisting && (input.Confirmed == nil || !*input.Confirmed) {
		return nil, auth.ErrPTZInvalid
	}
	for _, stop := range input.Stops {
		if stop.PresetID < 1 || stop.PresetID > 255 {
			return nil, auth.ErrPTZInvalid
		}
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}

	trackID := *input.TrackID
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "巡航 " + strconv.Itoa(trackID)
	}
	fingerprint := map[string]any{
		"trackId": trackID, "name": name, "speed": input.Speed, "dwellSec": input.DwellSec,
		"stops": input.Stops, "replaceExisting": input.ReplaceExisting,
	}
	steps := make([]cruiseTrackCreateStep, 0, len(input.Stops)+3)
	if input.ReplaceExisting {
		steps = append(steps, cruiseTrackCreateStep{Step: "clear", Action: manscdp.PTZActionCruiseDeletePath,
			Command: manscdp.PTZExtendedCommand{ID: trackID}})
	}
	for index, stop := range input.Stops {
		steps = append(steps, cruiseTrackCreateStep{Step: "add_stop", Sequence: index + 1, Action: manscdp.PTZActionCruiseAddStop,
			Command: manscdp.PTZExtendedCommand{ID: trackID, SubID: stop.PresetID}})
	}
	if input.Speed > 0 {
		steps = append(steps, cruiseTrackCreateStep{Step: "set_speed", Action: manscdp.PTZActionCruiseSetSpeed,
			Command: manscdp.PTZExtendedCommand{ID: trackID, Value16: input.Speed}})
	}
	if input.DwellSec > 0 {
		steps = append(steps, cruiseTrackCreateStep{Step: "set_dwell", Action: manscdp.PTZActionCruiseSetDwell,
			Command: manscdp.PTZExtendedCommand{ID: trackID, Value16: input.DwellSec}})
	}
	commands := make([]runtimeptz.Command, 0, len(steps))
	for index, step := range steps {
		step := step
		payload := map[string]any{"request": fingerprint, "step": step.Step, "sequence": step.Sequence, "action": step.Action, "trackId": trackID}
		if step.Step == "add_stop" {
			payload["presetId"] = step.Command.SubID
		}
		if step.Step == "set_speed" {
			payload["speed"] = input.Speed
		}
		if step.Step == "set_dwell" {
			payload["dwellSec"] = input.DwellSec
		}
		commands = append(commands, runtimeptz.Command{
			CmdType: manscdp.CmdDeviceControl, Action: string(step.Action),
			IdempotencyKey: cruiseTrackChildKey(request.IdempotencyKey, index),
			Profile:        target.Profile, Payload: payload,
			Build: func(sn int) ([]byte, error) {
				command := step.Command
				command.Action = step.Action
				return manscdp.BuildExtendedPTZControlWithProfile(target.Profile, channel.ChannelID, sn, command)
			},
		})
	}
	operations, dispatchErr := d.service.ExecuteBatch(ctx, target, commands)
	if dispatchErr != nil {
		mapped := mapOperationError(dispatchErr)
		if errors.Is(mapped, auth.ErrPTZConflict) || len(operations) == 0 || operations[0].OperationID == "" && len(operations) == 1 {
			return nil, mapped
		}
	}

	stepResults := make([]map[string]any, len(steps))
	completedStops := 0
	partial := dispatchErr != nil
	failedStep := ""
	for index, step := range steps {
		result := map[string]any{"step": step.Step, "status": "not_sent"}
		if index < len(operations) {
			operation := operations[index]
			if operation.OperationID != "" {
				result["operationId"] = operation.OperationID
			}
			if operation.Status != "" {
				result["status"] = string(operation.Status)
			}
			if operation.Status != gbmodels.PTZOperationSent {
				partial = true
				if failedStep == "" {
					failedStep = step.Step
				}
			} else if step.Step == "add_stop" {
				completedStops++
			}
		} else {
			partial = true
			if failedStep == "" {
				failedStep = step.Step
			}
		}
		switch step.Step {
		case "add_stop":
			result["presetId"] = step.Command.SubID
			result["index"] = step.Sequence
		case "set_speed":
			result["speed"] = input.Speed
		case "set_dwell":
			result["dwellSec"] = input.DwellSec
		}
		stepResults[index] = result
	}
	if !partial {
		if err := d.upsertCruiseTrack(ctx, target, channel, trackID, name, input); err != nil {
			return nil, err
		}
	}
	result := map[string]any{
		"trackId": trackID, "name": name, "result": "sent_result_unconfirmed", "deviceAcknowledged": false,
		"totalStops": len(input.Stops), "completedStops": completedStops, "steps": stepResults,
	}
	if partial {
		result["result"] = "partial_failure"
		if failedStep != "" {
			result["failedStep"] = failedStep
		}
	}
	return result, nil
}

func cruiseTrackChildKey(key string, sequence int) string {
	hash := sha256.Sum256([]byte(key))
	return "cruise:" + hex.EncodeToString(hash[:]) + ":" + strconv.Itoa(sequence)
}

func (d *Dispatcher) upsertCruiseTrack(ctx context.Context, target runtimeptz.Target, channel gbmodels.GbChannel, trackID int, name string, input cruiseTrackCreateInput) error {
	points := make([]cruiseTrackPointView, 0, len(input.Stops))
	for _, stop := range input.Stops {
		points = append(points, cruiseTrackPointView{PresetIndex: stop.PresetID, StayTime: input.DwellSec, Speed: input.Speed})
	}
	detail, err := json.Marshal(map[string]any{"trackId": trackID, "name": name, "sumNum": len(points), "cruisePoints": points, "source": "reconcile-pending"})
	if err != nil {
		return err
	}
	now := time.Now()
	track := gbmodels.GbPTZCruiseTrack{
		DeviceID: target.DeviceID, ChannelID: channel.ID, TrackID: trackID, Name: name,
		DetailJSON: string(detail), RawSummary: "reconcile-pending", UpdatedAt: now, CreatedAt: now,
	}
	return d.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "track_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"device_id", "name", "detail_json", "raw_summary", "updated_at"}),
	}).Create(&track).Error
}

func (d *Dispatcher) getCruiseTrack(ctx context.Context, request auth.PTZRequest) (any, error) {
	trackID := 0
	if _, err := fmtSscanfDecimal(request.TrackID, &trackID); err != nil || trackID < 0 || trackID > 255 {
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	var stored gbmodels.GbPTZCruiseTrack
	result := d.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND track_id = ?", target.DeviceID, channel.ID, trackID).Limit(1).Find(&stored)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, auth.ErrPTZNotFound
	}
	view := cruiseTrackView{TrackID: stored.TrackID, Name: stored.Name, Enabled: stored.Enabled, DeviceTime: stored.DeviceTime, Source: "device_cache"}
	freshness := gbmodels.PTZFreshnessUnknown
	if stored.RawSummary == "reconcile-pending" {
		view.Source = "platform_pending_reconcile"
	} else {
		observedAt := stored.UpdatedAt
		view.ObservedAt = &observedAt
		freshness = gbmodels.PTZFreshnessFresh
		if time.Since(observedAt) > time.Minute {
			freshness = gbmodels.PTZFreshnessStale
		}
	}
	if strings.TrimSpace(stored.DetailJSON) != "" {
		var detail cruiseTrackDetailView
		if err := json.Unmarshal([]byte(stored.DetailJSON), &detail); err != nil {
			return nil, auth.ErrPTZUnavailable
		}
		view.Detail = &detail
	}
	return map[string]any{"track": view, "freshness": freshness, "observedAt": view.ObservedAt}, nil
}

func (d *Dispatcher) trackTarget(ctx context.Context, request auth.PTZRequest) (any, error) {
	var input targetTrackInput
	if err := decodeStrictBody(request.Body, &input, "mode", "deviceId2", "area"); err != nil {
		return nil, err
	}
	mode, err := manscdp.ParseTargetTrackMode(input.Mode)
	if err != nil {
		return nil, auth.ErrPTZInvalid
	}
	var area *manscdp.TargetTrackArea
	if input.Area != nil {
		area = &manscdp.TargetTrackArea{
			Length: input.Area.Length, Width: input.Area.Width, MidPointX: input.Area.MidPointX,
			MidPointY: input.Area.MidPointY, LengthX: input.Area.LengthX, LengthY: input.Area.LengthY,
		}
	}
	if err := manscdp.ValidateTargetTrackCommand(manscdp.TargetTrackCommand{Mode: mode, DeviceID2: input.DeviceID2, Area: area}); err != nil {
		return nil, auth.ErrPTZInvalid
	}
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	if target.Profile.Version != protocol.Version2022 {
		return nil, auth.ErrPTZUnsupported
	}
	capability := manscdp.ParseControlCapabilities(channel.Capabilities, channel.PTZType).TargetTrack
	if capability.State != manscdp.CapabilitySupported {
		return nil, auth.ErrPTZUnsupported
	}
	deviceID2 := strings.TrimSpace(input.DeviceID2)
	if deviceID2 != "" {
		if !validateTargetTrackCode(deviceID2) {
			return nil, auth.ErrPTZInvalid
		}
		departmentIDs, scopeErr := resource.ResolveDepartmentIDs(ctx, d.db, resource.DepartmentScope{OwnerDeptID: request.OwnerDeptID, DataScope: request.DataScope})
		if scopeErr != nil {
			return nil, scopeErr
		}
		var panoramicChannel gbmodels.GbChannel
		result := d.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", request.DeviceID, deviceID2, channel.OwnerDeptID, departmentIDs).Limit(1).Find(&panoramicChannel)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, auth.ErrPTZNotFound
		}
	}
	actorDeptID := uint(0)
	if request.OwnerDeptID > 0 {
		actorDeptID = request.OwnerDeptID
	}
	operation, err := d.service.TrackTarget(ctx, target, runtimeptz.TargetTrackRequest{Mode: mode, DeviceID2: deviceID2, Area: area}, 0, actorDeptID, request.IdempotencyKey)
	if err != nil {
		return nil, mapOperationError(err)
	}
	view := controlOperationResponse(operation)
	view.Result = "sent_result_unconfirmed"
	view.DeviceAcknowledged = false
	return map[string]any{
		"operationId": view.OperationID, "status": view.Status, "action": view.Action, "sn": view.SN,
		"result": view.Result, "responseRequired": false, "deviceAcknowledged": false,
	}, nil
}

func validateTargetTrackCode(value string) bool {
	if len(value) == 0 || len(value) > 20 {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func controlOperationResponse(operation gbmodels.GbPTZOperation) controlOperationView {
	result := "sent_result_unconfirmed"
	acknowledged := false
	if operation.ResponseRequired {
		result = "pending_device_response"
		switch operation.Status {
		case gbmodels.PTZOperationAccepted:
			result, acknowledged = "device_accepted", true
		case gbmodels.PTZOperationRejected:
			result, acknowledged = "device_rejected", true
		case gbmodels.PTZOperationTimeout, gbmodels.PTZOperationUnknown:
			result = "result_unknown"
		}
	}
	return controlOperationView{OperationID: operation.OperationID, Status: string(operation.Status), Action: operation.Action, SN: operation.SN, Result: result, DeviceAcknowledged: acknowledged}
}

func (d *Dispatcher) listPresets(ctx context.Context, request auth.PTZRequest) (any, error) {
	target, channel, err := d.resolveTarget(ctx, request)
	if err != nil {
		return nil, mapTargetError(err)
	}
	var rows []gbmodels.GbPTZPreset
	if err := d.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND status <> ?", target.DeviceID, channel.ID, gbmodels.PTZPresetDeleted).Order("preset_id ASC").Find(&rows).Error; err != nil {
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
		case runtimeptz.ErrorCodeHomePositionDeviceOffline:
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

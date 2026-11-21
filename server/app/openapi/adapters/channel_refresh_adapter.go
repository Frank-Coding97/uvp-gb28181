package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

const ChannelQueryOperationScope = "channel:query:operation:read"
const ChannelQueryOperationAdapterKey = "devicequery.query-operation.v1"

type ChannelQueryExecutor interface {
	Execute(context.Context, runtimeptz.Target, runtimeptz.Command) (gbmodels.GbPTZOperation, error)
}

type channelRefreshDefinition struct{ key, scope, kind string }

var channelRefreshDefinitions = []channelRefreshDefinition{
	{"devicequery.refresh.preset.v1", "channel:ptz:preset:refresh", "preset"},
	{"devicequery.refresh.home.v1", "channel:ptz:home:refresh", "home"},
	{"devicequery.refresh.cruise.v1", "channel:ptz:cruise:refresh", "cruise"},
	{"devicequery.refresh.position.v1", "channel:ptz:position:refresh", "position"},
	{"devicequery.refresh.storage.v1", "channel:storage-card:refresh", "storage"},
	{"devicequery.refresh.status.v1", "channel:control-status:refresh", "status"},
	{ChannelQueryOperationAdapterKey, ChannelQueryOperationScope, "operation"},
}

type channelRefreshAdapter struct {
	definition channelRefreshDefinition
	resolver   MaintenanceTargetResolver
	service    ChannelQueryExecutor
	reader     MaintenanceOperationReader
}

// The live executor is supplied by the current SIP generation, just as for
// maintenance commands. Query records retain a client-scoped key and actor department.
func NewChannelRefreshAdapterRegistrations(resolver MaintenanceTargetResolver, service ChannelQueryExecutor, reader MaintenanceOperationReader) []catalogruntime.AdapterRegistration {
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(channelRefreshDefinitions))
	for _, definition := range channelRefreshDefinitions {
		registrations = append(registrations, catalogruntime.AdapterRegistration{Key: definition.key, ContractVersion: "v1", Adapter: &channelRefreshAdapter{definition: definition, resolver: resolver, service: service, reader: reader}})
	}
	return registrations
}

func (a *channelRefreshAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a == nil || a.resolver == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	request, ok := invocation.Value.(Request)
	if !ok || request.ClientID <= 0 || invocation.Scope != a.definition.scope || len(invocation.Query) != 0 || len(request.Query) != 0 {
		return nil, ErrInvalidRequest
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, err
	}
	if invocation.Params["deviceId"] != request.DeviceID || invocation.Params["channelId"] != request.ChannelID || !validMaintenanceGBCode(request.DeviceID) || !validMaintenanceGBCode(request.ChannelID) {
		return nil, ErrInvalidRequest
	}
	if a.definition.kind == "operation" {
		return a.readOperation(ctx, invocation, request)
	}
	if a.service == nil {
		return nil, ErrUnavailable
	}
	if invocation.Method != http.MethodPost || len(invocation.Params) != 2 {
		return nil, ErrInvalidRequest
	}
	input, err := decodeChannelRefreshInput(invocation.Body, a.definition.kind)
	if err != nil {
		return nil, err
	}
	key, err := scopedMaintenanceIdempotencyKey(request.ClientID, request.IdempotencyKey)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	channel, err := a.resolver.ResolveChannel(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	if err != nil {
		return nil, normalizeMaintenanceResolveError(err)
	}
	if err = validateMaintenanceTarget(channel.Target, request.DeviceID, request.ChannelID); err != nil {
		return nil, err
	}
	if err = requireKnownMaintenanceProfile(channel.Target.Profile); err != nil {
		return nil, err
	}
	command, err := buildChannelRefreshCommand(a.definition.kind, channel.Target, input, key, request.ResourceScope.OwnerDeptID)
	if err != nil {
		return nil, err
	}
	operation, err := a.service.Execute(ctx, channel.Target, command)
	if err != nil {
		return nil, normalizeMaintenanceServiceError(err)
	}
	return maintenanceOperationView(operation), nil
}

type channelRefreshInput struct {
	TrackID *int   `json:"trackId"`
	Kind    string `json:"kind"`
}

func decodeChannelRefreshInput(body []byte, kind string) (channelRefreshInput, error) {
	var input channelRefreshInput
	if len(body) == 0 || hasDuplicateMaintenanceJSONMember(body) {
		return input, ErrInvalidRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return input, ErrInvalidRequest
	}
	for name, value := range fields {
		if string(bytes.TrimSpace(value)) == "null" || (name != "trackId" || kind != "cruise") && (name != "kind" || kind != "status") {
			return input, ErrInvalidRequest
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return input, ErrInvalidRequest
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, ErrInvalidRequest
	}
	if input.TrackID != nil && (*input.TrackID < 1 || *input.TrackID > 255) {
		return input, ErrInvalidRequest
	}
	if kind == "status" && input.Kind != "record" && input.Kind != "alarm" {
		return input, ErrInvalidRequest
	}
	return input, nil
}

func buildChannelRefreshCommand(kind string, target runtimeptz.Target, input channelRefreshInput, key string, department uint) (runtimeptz.Command, error) {
	profile := target.Profile
	command := runtimeptz.Command{IdempotencyKey: key, Profile: profile, ActorDeptID: department, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: target.ChannelCode, Payload: map[string]interface{}{}, ResponseRequired: true, MaxAttempts: 3}
	code := target.ChannelCode
	switch kind {
	case "preset":
		command.CmdType, command.Action = manscdp.CmdPresetQuery, "refresh_presets"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildPresetQueryWithProfile(profile, code, sn) }
	case "home":
		command.CmdType, command.Action = manscdp.CmdHomePositionQuery, "refresh_home_position"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildHomePositionQueryWithProfile(profile, code, sn) }
	case "cruise":
		if input.TrackID == nil {
			command.CmdType, command.Action = manscdp.CmdCruiseTrackListQuery, "refresh_cruise_tracks"
			command.Build = func(sn int) ([]byte, error) { return manscdp.BuildCruiseTrackListQueryWithProfile(profile, code, sn) }
		} else {
			track := *input.TrackID
			command.CmdType, command.Action = manscdp.CmdCruiseTrackQuery, "refresh_cruise_track"
			command.Payload["trackId"] = track
			command.Build = func(sn int) ([]byte, error) {
				return manscdp.BuildCruiseTrackQueryWithProfile(profile, code, sn, track)
			}
		}
	case "position":
		command.CmdType, command.Action = manscdp.CmdPTZPreciseStatusQuery, "refresh_precise_status"
		if profile.SupportsPrecisePTZ() {
			command.CmdType = manscdp.CmdPTZPosition
		}
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildPTZPreciseStatusQueryWithProfile(profile, code, sn) }
	case "storage":
		if profile.Version != protocol.Version2022 {
			return command, ErrCapabilityNotSupported
		}
		command.CmdType, command.Action = manscdp.CmdSDCardStatus, "refresh_storage_cards"
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildSDCardStatusQueryWithProfile(profile, code, sn) }
	case "status":
		command.CmdType, command.Action = manscdp.CmdDeviceStatus, "device_status_"+input.Kind
		if input.Kind == "alarm" {
			code = target.DeviceCode
			command.TargetCode = code
			command.TargetScope = gbmodels.ControlTargetScopeDevice
		}
		command.Payload["fact"], command.Payload["targetCode"] = input.Kind, code
		command.Build = func(sn int) ([]byte, error) { return manscdp.BuildDeviceStatusQueryWithProfile(profile, code, sn) }
	default:
		return command, ErrInvalidRequest
	}
	return command, nil
}

func (a *channelRefreshAdapter) readOperation(ctx context.Context, invocation catalogruntime.Invocation, request Request) (any, error) {
	if a.reader == nil {
		return nil, ErrUnavailable
	}
	operationID := invocation.Params["operationId"]
	if invocation.Method != http.MethodGet || len(invocation.Body) != 0 || len(invocation.Params) != 3 || !validMaintenanceOperationID(operationID) {
		return nil, ErrInvalidRequest
	}
	channel, err := a.resolver.ResolveChannel(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	if err != nil {
		return nil, normalizeMaintenanceResolveError(err)
	}
	row, err := a.reader.GetOperationForClient(ctx, request.ResourceScope, request.DeviceID, operationID, request.ClientID)
	if err != nil {
		return nil, normalizeMaintenanceResolveError(err)
	}
	if row.OperationID != operationID || row.DeviceCode != request.DeviceID || row.ChannelCode != request.ChannelID || row.ChannelID != channel.Target.ChannelID || !strings.HasPrefix(row.IdempotencyKey, clientIdempotencyPrefix(request.ClientID)) || !isChannelQueryAction(row.Action) {
		return nil, ErrNotFound
	}
	return maintenanceOperationView(row), nil
}

func isChannelQueryAction(action string) bool {
	switch action {
	case "refresh_presets", "refresh_home_position", "refresh_cruise_tracks", "refresh_cruise_track", "refresh_precise_status", "refresh_storage_cards", "device_status_record", "device_status_alarm":
		return true
	}
	return false
}

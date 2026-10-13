package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	PTZPresetListScope        = "ptz:preset:list"
	PTZPresetSaveScope        = "ptz:preset:save"
	PTZPresetCallScope        = "ptz:preset:call"
	PTZPresetDeleteScope      = "ptz:preset:delete"
	PTZOperationReadScope     = "ptz:operation:read"
	PTZMoveScope              = "ptz:move"
	PTZPreciseScope           = "ptz:precise"
	PTZExtendedScope          = "ptz:extended"
	PTZWiperScope             = "ptz:wiper"
	PTZHomePositionSetScope   = "ptz:home-position:set"
	PTZCruiseControlScope     = "ptz:cruise:control"
	PTZCruiseTrackCreateScope = "ptz:cruise:track:create"
	PTZCruiseTrackReadScope   = "ptz:cruise:track:read"
	PTZTargetTrackScope       = "ptz:target-track"
)

var (
	ErrPTZUnavailable = errors.New("OpenAPI PTZ dispatcher unavailable")
	ErrPTZInvalid     = errors.New("invalid OpenAPI PTZ request")
	ErrPTZNotFound    = errors.New("OpenAPI PTZ resource not found")
	ErrPTZConflict    = errors.New("OpenAPI PTZ operation conflict")
	ErrPTZUnsupported = errors.New("OpenAPI PTZ capability not supported")
)

// PTZRequest is the authenticated, department-bound command passed to the
// GB28181 runtime. It contains no operator claims and never trusts an owner
// department supplied by the caller.
type PTZRequest struct {
	Scope          string
	ClientID       int64
	OwnerDeptID    uint
	DataScope      int8
	DeviceID       string
	ChannelID      string
	PresetID       string
	TrackID        string
	OperationID    string
	IdempotencyKey string
	Body           []byte
}

// PTZDispatcher is the only seam between the OpenAPI boundary and the SIP/PTZ
// runtime. The runtime owns target resolution, protocol encoding and durable
// operation state; the gateway owns AK/SK, scope, replay and audit admission.
type PTZDispatcher interface {
	Ready() bool
	Handle(context.Context, PTZRequest) (any, error)
}

func isPTZScope(scope string) bool {
	switch scope {
	case PTZPresetListScope, PTZPresetSaveScope, PTZPresetCallScope, PTZPresetDeleteScope, PTZOperationReadScope,
		PTZMoveScope, PTZPreciseScope, PTZExtendedScope, PTZWiperScope, PTZHomePositionSetScope, PTZCruiseControlScope,
		PTZCruiseTrackCreateScope, PTZCruiseTrackReadScope, PTZTargetTrackScope:
		return true
	default:
		return false
	}
}

func ptzPattern(scope string) string {
	switch scope {
	case PTZPresetListScope, PTZPresetSaveScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets"
	case PTZPresetCallScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId/call"
	case PTZPresetDeleteScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId"
	case PTZOperationReadScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/operations/:operationId"
	case PTZMoveScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/move"
	case PTZPreciseScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/precise"
	case PTZExtendedScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/extended"
	case PTZWiperScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/wiper"
	case PTZHomePositionSetScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/home-position"
	case PTZCruiseControlScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/control"
	case PTZCruiseTrackCreateScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks"
	case PTZCruiseTrackReadScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/cruise/tracks/:trackId"
	case PTZTargetTrackScope:
		return "/openapi/v1/devices/:deviceId/channels/:channelId/ptz/target-track"
	default:
		return ""
	}
}

func ptzMethod(scope string) string {
	if scope == PTZPresetListScope || scope == PTZOperationReadScope || scope == PTZCruiseTrackReadScope {
		return http.MethodGet
	}
	if scope == PTZPresetDeleteScope {
		return http.MethodDelete
	}
	return http.MethodPost
}

func ptzRouteMatches(q gatewayRequest) bool {
	pattern := ptzPattern(q.scope)
	if pattern == "" || q.pattern != pattern || q.method != ptzMethod(q.scope) || validatePath(q.path) != nil || q.rawQuery != "" {
		return false
	}
	path := strings.NewReplacer(":deviceId", q.deviceID, ":channelId", q.channelID, ":presetId", q.presetID, ":operationId", q.operationID, ":trackId", q.trackID).Replace(pattern)
	rawPath, _, hasQuery := strings.Cut(q.rawURI, "?")
	return path == q.path && rawPath == q.path && !hasQuery
}

func validatePTZID(value string) bool {
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

func validatePTZRequest(q gatewayRequest) error {
	if !validatePTZID(q.deviceID) || !validatePTZID(q.channelID) {
		return ErrPTZInvalid
	}
	if q.scope == PTZPresetCallScope || q.scope == PTZPresetDeleteScope {
		if len(q.presetID) == 0 || len(q.presetID) > 3 {
			return ErrPTZInvalid
		}
		for i := range q.presetID {
			if q.presetID[i] < '0' || q.presetID[i] > '9' {
				return ErrPTZInvalid
			}
		}
	}
	if q.scope == PTZOperationReadScope && (len(q.operationID) == 0 || len(q.operationID) > 64 || !utf8.ValidString(q.operationID)) {
		return ErrPTZInvalid
	}
	if q.scope == PTZCruiseTrackReadScope {
		if len(q.trackID) == 0 || len(q.trackID) > 3 {
			return ErrPTZInvalid
		}
		trackID := 0
		for i := range q.trackID {
			if q.trackID[i] < '0' || q.trackID[i] > '9' {
				return ErrPTZInvalid
			}
			trackID = trackID*10 + int(q.trackID[i]-'0')
		}
		if trackID > 255 {
			return ErrPTZInvalid
		}
	}
	if q.scope == PTZPresetSaveScope {
		if len(q.body) == 0 || len(q.body) > maxBodyBytes {
			return ErrPTZInvalid
		}
	}
	if q.scope == PTZPresetCallScope || q.scope == PTZPresetDeleteScope {
		if string(q.body) != "{}" {
			return ErrPTZInvalid
		}
	}
	if q.scope == PTZPresetSaveScope || q.scope == PTZPresetCallScope || q.scope == PTZPresetDeleteScope {
		if len(q.idempotencyKey) == 0 || len(q.idempotencyKey) > 128 || !utf8.ValidString(q.idempotencyKey) {
			return ErrPTZInvalid
		}
		for _, r := range q.idempotencyKey {
			if r < 0x21 || r > 0x7e {
				return ErrPTZInvalid
			}
		}
	}
	if isPTZJSONWriteScope(q.scope) {
		if len(q.body) == 0 || len(q.body) > maxBodyBytes || validateJSONBody(q.body) != nil {
			return ErrPTZInvalid
		}
		if len(q.idempotencyKey) == 0 || len(q.idempotencyKey) > 128 || !utf8.ValidString(q.idempotencyKey) {
			return ErrPTZInvalid
		}
		for _, r := range q.idempotencyKey {
			if r < 0x21 || r > 0x7e {
				return ErrPTZInvalid
			}
		}
	}
	return nil
}

func isPTZJSONWriteScope(scope string) bool {
	switch scope {
	case PTZMoveScope, PTZPreciseScope, PTZExtendedScope, PTZWiperScope, PTZHomePositionSetScope, PTZCruiseControlScope,
		PTZCruiseTrackCreateScope, PTZTargetTrackScope:
		return true
	default:
		return false
	}
}

func ptzMetadata(q gatewayRequest, viewOwner uint, dataScope int8) (metadataInput, error) {
	if err := validatePTZRequest(q); err != nil {
		return metadataInput{}, err
	}
	return metadataInput{owner: viewOwner, dataScope: dataScope, scope: q.scope, deviceID: q.deviceID, channelID: q.channelID}, nil
}

func ptzMetadataFailure(id string, err error) gatewayResponse {
	if errors.Is(err, resource.ErrResourceNotFound) || errors.Is(err, ErrPTZNotFound) {
		return gatewayError(id, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	}
	if errors.Is(err, ErrPTZInvalid) {
		return gatewayError(id, http.StatusBadRequest, "INVALID_REQUEST")
	}
	if errors.Is(err, ErrPTZConflict) {
		return gatewayError(id, http.StatusConflict, "CONFLICT")
	}
	if errors.Is(err, ErrPTZUnsupported) {
		return gatewayError(id, http.StatusUnprocessableEntity, "CAPABILITY_NOT_SUPPORTED")
	}
	return gatewayError(id, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
}

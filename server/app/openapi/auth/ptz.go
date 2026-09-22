package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

const (
	PTZPresetListScope    = "ptz:preset:list"
	PTZPresetSaveScope    = "ptz:preset:save"
	PTZPresetCallScope    = "ptz:preset:call"
	PTZPresetDeleteScope  = "ptz:preset:delete"
	PTZOperationReadScope = "ptz:operation:read"
)

var (
	ErrPTZUnavailable = errors.New("OpenAPI PTZ dispatcher unavailable")
	ErrPTZInvalid     = errors.New("invalid OpenAPI PTZ request")
	ErrPTZNotFound    = errors.New("OpenAPI PTZ resource not found")
	ErrPTZConflict    = errors.New("OpenAPI PTZ operation conflict")
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
	case PTZPresetListScope, PTZPresetSaveScope, PTZPresetCallScope, PTZPresetDeleteScope, PTZOperationReadScope:
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
	default:
		return ""
	}
}

func ptzMethod(scope string) string {
	if scope == PTZPresetListScope || scope == PTZOperationReadScope {
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
	path := strings.NewReplacer(":deviceId", q.deviceID, ":channelId", q.channelID, ":presetId", q.presetID, ":operationId", q.operationID).Replace(pattern)
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
	return nil
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
	return gatewayError(id, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
}

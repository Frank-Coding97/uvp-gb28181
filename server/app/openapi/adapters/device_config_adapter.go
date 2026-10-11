package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	runtimeptz "uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/deviceconfig"
)

const (
	DeviceConfigRefreshScope       = "device:config:refresh"
	DeviceConfigReadScope          = "device:config:read"
	DeviceConfigApplyScope         = "device:config:apply"
	VideoParamsRefreshScope        = "channel:video-params:refresh"
	VideoParamsReadScope           = "channel:video-params:read"
	VideoParamsApplyScope          = "channel:video-params:apply"
	DeviceConfigOperationReadScope = "device:config:operation:read"

	DeviceConfigRefreshAdapterKey       = "device.config.refresh.v1"
	DeviceConfigReadAdapterKey          = "device.config.read.v1"
	DeviceConfigApplyAdapterKey         = "device.config.apply.v1"
	VideoParamsRefreshAdapterKey        = "device.video-params.refresh.v1"
	VideoParamsReadAdapterKey           = "device.video-params.read.v1"
	VideoParamsApplyAdapterKey          = "device.video-params.apply.v1"
	DeviceConfigOperationReadAdapterKey = "device.config.operation-read.v1"
)

const maxDeviceConfigBodyBytes = 1 << 20

type DeviceConfigService interface {
	Refresh(context.Context, deviceconfig.Request, []string) (deviceconfig.OperationResult, error)
	Apply(context.Context, deviceconfig.Request, manscdp.DeviceConfigBlocks) (deviceconfig.OperationResult, error)
	RefreshVideoParams(context.Context, deviceconfig.Request) (deviceconfig.OperationResult, error)
	ApplyVideoParams(context.Context, deviceconfig.Request, []manscdp.VideoParamItem) (deviceconfig.OperationResult, error)
	GetConfigSnapshot(context.Context, deviceconfig.Request) (deviceconfig.ConfigSnapshot, error)
	GetVideoParamSnapshot(context.Context, deviceconfig.Request) (deviceconfig.VideoParamSnapshot, error)
	GetOperation(context.Context, deviceconfig.Request, string) (deviceconfig.OperationResult, error)
}

type deviceConfigOperation uint8

const (
	refreshDeviceConfig deviceConfigOperation = iota + 1
	readDeviceConfig
	applyDeviceConfig
	refreshVideoParams
	readVideoParams
	applyVideoParams
	readDeviceConfigOperation
)

type deviceConfigAdapter struct {
	key       string
	scope     string
	method    string
	operation deviceConfigOperation
	service   DeviceConfigService
}

var _ catalogruntime.Adapter = (*deviceConfigAdapter)(nil)
var _ DeviceConfigService = (*deviceconfig.Service)(nil)

func NewDeviceConfigAdapterRegistrations(service DeviceConfigService) []catalogruntime.AdapterRegistration {
	definitions := []deviceConfigAdapter{
		{key: DeviceConfigRefreshAdapterKey, scope: DeviceConfigRefreshScope, method: "POST", operation: refreshDeviceConfig, service: service},
		{key: DeviceConfigReadAdapterKey, scope: DeviceConfigReadScope, method: "GET", operation: readDeviceConfig, service: service},
		{key: DeviceConfigApplyAdapterKey, scope: DeviceConfigApplyScope, method: "PUT", operation: applyDeviceConfig, service: service},
		{key: VideoParamsRefreshAdapterKey, scope: VideoParamsRefreshScope, method: "POST", operation: refreshVideoParams, service: service},
		{key: VideoParamsReadAdapterKey, scope: VideoParamsReadScope, method: "GET", operation: readVideoParams, service: service},
		{key: VideoParamsApplyAdapterKey, scope: VideoParamsApplyScope, method: "PUT", operation: applyVideoParams, service: service},
		{key: DeviceConfigOperationReadAdapterKey, scope: DeviceConfigOperationReadScope, method: "GET", operation: readDeviceConfigOperation, service: service},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(definitions))
	for index := range definitions {
		adapter := &definitions[index]
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: adapter.Key(), ContractVersion: ResourceAdapterContractVersion, Adapter: adapter,
		})
	}
	return registrations
}

func (adapter *deviceConfigAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

func (adapter *deviceConfigAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (data any, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, ErrUnavailable
		}
	}()
	if adapter == nil || adapter.service == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != adapter.scope || strings.ToUpper(invocation.Method) != adapter.method {
		return nil, ErrInvalidRequest
	}
	request, ok := invocation.Value.(Request)
	if !ok || request.ClientID <= 0 || !validResourceScope(request.ResourceScope) {
		return nil, ErrInvalidRequest
	}
	if !validConfigPathID(request.DeviceID) || !validConfigPathID(request.ChannelID) ||
		invocation.Params["deviceId"] != request.DeviceID || invocation.Params["channelId"] != request.ChannelID {
		return nil, ErrInvalidRequest
	}
	operationID := ""
	expectedParams := 2
	if adapter.operation == readDeviceConfigOperation {
		expectedParams = 3
		operationID = invocation.Params["operationId"]
		if !validConfigOperationID(operationID) {
			return nil, ErrInvalidRequest
		}
	}
	if len(invocation.Params) != expectedParams {
		return nil, ErrInvalidRequest
	}
	if len(invocation.Query) != 0 {
		return nil, ErrInvalidRequest
	}
	if !validDeviceConfigIdempotencyHeader(request.IdempotencyKey) {
		return nil, ErrInvalidRequest
	}
	serviceRequest := deviceconfig.Request{
		ClientID: request.ClientID, Scope: request.ResourceScope,
		DeviceID: request.DeviceID, ChannelID: request.ChannelID,
		IdempotencyKey: request.IdempotencyKey,
	}

	switch adapter.operation {
	case refreshDeviceConfig:
		if err = requireDeviceConfigIdempotencyKey(request.IdempotencyKey); err != nil {
			return nil, err
		}
		input, decodeErr := decodeDeviceConfigRefreshInput(invocation.Body)
		if decodeErr != nil {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.Refresh(ctx, serviceRequest, input.ConfigTypes)
	case readDeviceConfig:
		if len(invocation.Body) != 0 {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.GetConfigSnapshot(ctx, serviceRequest)
	case applyDeviceConfig:
		if err = requireDeviceConfigIdempotencyKey(request.IdempotencyKey); err != nil {
			return nil, err
		}
		input, decodeErr := decodeDeviceConfigApplyInput(invocation.Body)
		if decodeErr != nil || input.Blocks.IsEmpty() || !validOpenAPIDeviceConfigBlocks(input.Blocks) || manscdp.ValidateDeviceConfigBlocks(input.Blocks) != nil {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.Apply(ctx, serviceRequest, input.Blocks)
	case refreshVideoParams:
		if err = requireDeviceConfigIdempotencyKey(request.IdempotencyKey); err != nil {
			return nil, err
		}
		if len(invocation.Body) != 0 && !isEmptyJSONObject(invocation.Body) {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.RefreshVideoParams(ctx, serviceRequest)
	case readVideoParams:
		if len(invocation.Body) != 0 {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.GetVideoParamSnapshot(ctx, serviceRequest)
	case applyVideoParams:
		if err = requireDeviceConfigIdempotencyKey(request.IdempotencyKey); err != nil {
			return nil, err
		}
		items, decodeErr := decodeVideoParamApplyInput(invocation.Body)
		if decodeErr != nil || len(items) == 0 || len(items) > 8 || manscdp.ValidateVideoParamItems(items) != nil {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.service.ApplyVideoParams(ctx, serviceRequest, items)
	case readDeviceConfigOperation:
		if len(invocation.Body) != 0 {
			return nil, ErrInvalidRequest
		}
		result, operationErr := adapter.service.GetOperation(ctx, serviceRequest, operationID)
		if operationErr == nil && !isDeviceConfigOperationAction(result.Action) {
			return nil, ErrNotFound
		}
		data, err = result, operationErr
	default:
		return nil, ErrUnavailable
	}
	return data, normalizeDeviceConfigError(err)
}

func isDeviceConfigOperationAction(action string) bool {
	switch action {
	case runtimeptz.ActionRefreshDeviceConfigs, runtimeptz.ActionApplyDeviceConfig,
		runtimeptz.ActionRefreshVideoParams, "apply_video_params":
		return true
	default:
		return false
	}
}

func validDeviceConfigIdempotencyHeader(value string) bool {
	if value == "" {
		return true
	}
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func requireDeviceConfigIdempotencyKey(value string) error {
	if !validDeviceConfigIdempotencyHeader(value) || value == "" {
		return ErrInvalidRequest
	}
	return nil
}

type deviceConfigRefreshInput struct {
	ConfigTypes []string `json:"configTypes"`
}

type deviceConfigApplyInput struct {
	Blocks manscdp.DeviceConfigBlocks `json:"blocks"`
}

func validOpenAPIDeviceConfigBlocks(blocks manscdp.DeviceConfigBlocks) bool {
	// These fields either report device capability/state, contain platform-only
	// aggregation metadata, or let a caller choose where a camera uploads images.
	// Capture destinations are owned by the platform's capture-session flow.
	if blocks.VideoParamOpt != nil || blocks.SnapShot != nil {
		return false
	}
	if blocks.BasicParam != nil && (blocks.BasicParam.PositionCapability != nil || blocks.BasicParam.Longitude != nil || blocks.BasicParam.Latitude != nil) {
		return false
	}
	if blocks.VideoRecordPlan != nil && blocks.VideoRecordPlan.AdditionalStreams != 0 {
		return false
	}
	if blocks.VideoAlarmRecord != nil && blocks.VideoAlarmRecord.AdditionalStreams != 0 {
		return false
	}
	return true
}

type videoParamItemInput struct {
	StreamNumber *int    `json:"streamNumber"`
	VideoFormat  string  `json:"videoFormat"`
	Resolution   string  `json:"resolution"`
	FrameRate    string  `json:"frameRate"`
	BitRateType  string  `json:"bitRateType"`
	VideoBitRate *string `json:"videoBitRate"`
}

type videoParamApplyInput struct {
	Items []videoParamItemInput `json:"items"`
}

func decodeDeviceConfigRefreshInput(body []byte) (deviceConfigRefreshInput, error) {
	input := deviceConfigRefreshInput{}
	if len(body) == 0 {
		return input, nil
	}
	var decoded deviceConfigRefreshInput
	if err := decodeStrictDeviceConfigJSON(body, &decoded); err != nil {
		return deviceConfigRefreshInput{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return deviceConfigRefreshInput{}, deviceconfig.ErrInvalidRequest
	}
	if value, exists := fields["configTypes"]; exists && string(value) == "null" {
		return deviceConfigRefreshInput{}, deviceconfig.ErrInvalidRequest
	}
	if decoded.ConfigTypes != nil {
		if len(decoded.ConfigTypes) == 0 {
			return deviceConfigRefreshInput{}, deviceconfig.ErrInvalidRequest
		}
		input.ConfigTypes = decoded.ConfigTypes
	}
	return input, nil
}

func decodeDeviceConfigApplyInput(body []byte) (deviceConfigApplyInput, error) {
	var input deviceConfigApplyInput
	if err := decodeStrictDeviceConfigJSON(body, &input); err != nil {
		return deviceConfigApplyInput{}, err
	}
	return input, nil
}

func decodeVideoParamApplyInput(body []byte) ([]manscdp.VideoParamItem, error) {
	var input videoParamApplyInput
	if err := decodeStrictDeviceConfigJSON(body, &input); err != nil {
		return nil, err
	}
	items := make([]manscdp.VideoParamItem, 0, len(input.Items))
	for _, item := range input.Items {
		if item.StreamNumber == nil {
			return nil, deviceconfig.ErrInvalidRequest
		}
		items = append(items, manscdp.VideoParamItem{
			StreamNumber: *item.StreamNumber, VideoFormat: strings.TrimSpace(item.VideoFormat),
			Resolution: strings.TrimSpace(item.Resolution), FrameRate: strings.TrimSpace(item.FrameRate),
			BitRateType: strings.TrimSpace(item.BitRateType), VideoBitRate: trimConfigOptionalString(item.VideoBitRate),
		})
	}
	return items, nil
}

func decodeStrictDeviceConfigJSON(body []byte, target any) error {
	if len(body) == 0 || len(body) > maxDeviceConfigBodyBytes {
		return deviceconfig.ErrInvalidRequest
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return deviceconfig.ErrInvalidRequest
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(body), maxDeviceConfigBodyBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return deviceconfig.ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return deviceconfig.ErrInvalidRequest
	}
	return nil
}

func isEmptyJSONObject(body []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(body, &object) == nil && len(object) == 0
}

func trimConfigOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeDeviceConfigError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, deviceconfig.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, deviceconfig.ErrInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, deviceconfig.ErrConflict):
		return ErrConflict
	case errors.Is(err, deviceconfig.ErrDeviceOffline):
		return ErrDeviceOffline
	case errors.Is(err, deviceconfig.ErrCapabilityNotSupported), errors.Is(err, deviceconfig.ErrCapabilityUnknown):
		return ErrCapabilityNotSupported
	default:
		return ErrUnavailable
	}
}

func validConfigPathID(value string) bool {
	return validPathID(value)
}

func validConfigOperationID(value string) bool {
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

package adapters

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	devicesessions "uvplatform.com/uvp-gb28181/app/openapi/device_sessions"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	CaptureCreateScope      = "channel:snapshot:create"
	CaptureReadScope        = "channel:snapshot:read"
	CaptureImageReadScope   = "channel:snapshot:image:read"
	CaptureCreateAdapterKey = "device.capture.create.v1"
	CaptureReadAdapterKey   = "device.capture.read.v1"
	CaptureImageAdapterKey  = "device.capture.image-content.v1"
)

type captureOperation uint8

const (
	captureCreate captureOperation = iota + 1
	captureRead
	captureImageRead
)

type DeviceSessionService interface {
	CreateCapture(context.Context, devicesessions.CaptureRequest) (devicesessions.CaptureSession, error)
	GetCapture(context.Context, int64, resource.DepartmentScope, string, string, string) (devicesessions.CaptureSession, error)
	ReadImage(context.Context, int64, resource.DepartmentScope, string, string, string, string) (devicesessions.ImageContent, error)
}

type deviceSessionAdapter struct {
	key       string
	scope     string
	method    string
	operation captureOperation
	service   DeviceSessionService
}

var _ catalogruntime.Adapter = (*deviceSessionAdapter)(nil)
var _ DeviceSessionService = (*devicesessions.Service)(nil)

func newDeviceSessionAdapters(service DeviceSessionService) []*deviceSessionAdapter {
	return []*deviceSessionAdapter{
		&deviceSessionAdapter{key: CaptureCreateAdapterKey, scope: CaptureCreateScope, method: "POST", operation: captureCreate, service: service},
		&deviceSessionAdapter{key: CaptureReadAdapterKey, scope: CaptureReadScope, method: "GET", operation: captureRead, service: service},
		&deviceSessionAdapter{key: CaptureImageAdapterKey, scope: CaptureImageReadScope, method: "GET", operation: captureImageRead, service: service},
	}
}

func NewDeviceSessionAdapterRegistrations(service DeviceSessionService) []catalogruntime.AdapterRegistration {
	adapters := newDeviceSessionAdapters(service)
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(adapters))
	for _, adapter := range adapters {
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: adapter.Key(), ContractVersion: ResourceAdapterContractVersion, Adapter: adapter,
		})
	}
	return registrations
}

func (adapter *deviceSessionAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

func (adapter *deviceSessionAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (data any, err error) {
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
	deviceID, channelID := invocation.Params["deviceId"], invocation.Params["channelId"]
	if !validPathID(deviceID) || !validPathID(channelID) {
		return nil, ErrInvalidRequest
	}
	switch adapter.operation {
	case captureCreate:
		input, decodeErr := decodeCaptureInput(invocation.Body)
		if decodeErr != nil || strings.TrimSpace(request.IdempotencyKey) == "" {
			return nil, ErrInvalidRequest
		}
		data, err := adapter.service.CreateCapture(ctx, devicesessions.CaptureRequest{
			ClientID: request.ClientID, Scope: request.ResourceScope,
			DeviceID: deviceID, ChannelID: channelID, IdempotencyKey: request.IdempotencyKey,
			SnapNum: input.SnapNum, Interval: input.Interval,
		})
		return data, normalizeDeviceSessionError(err)
	case captureRead:
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		sessionID := invocation.Params["sessionId"]
		if !validSessionID(sessionID) {
			return nil, ErrInvalidRequest
		}
		data, err := adapter.service.GetCapture(ctx, request.ClientID, request.ResourceScope, deviceID, channelID, sessionID)
		return data, normalizeDeviceSessionError(err)
	case captureImageRead:
		if len(invocation.Body) != 0 || len(invocation.Query) != 0 {
			return nil, ErrInvalidRequest
		}
		sessionID, fileName := invocation.Params["sessionId"], invocation.Params["fileName"]
		if !validSessionID(sessionID) || fileName == "" {
			return nil, ErrInvalidRequest
		}
		content, readErr := adapter.service.ReadImage(ctx, request.ClientID, request.ResourceScope, deviceID, channelID, sessionID, fileName)
		if readErr != nil {
			return nil, normalizeDeviceSessionError(readErr)
		}
		return struct {
			FileName      string `json:"fileName"`
			ContentType   string `json:"contentType"`
			Size          int    `json:"size"`
			MD5           string `json:"md5"`
			ContentBase64 string `json:"contentBase64"`
		}{
			FileName: content.FileName, ContentType: content.ContentType, Size: len(content.Bytes), MD5: content.MD5,
			ContentBase64: base64.StdEncoding.EncodeToString(content.Bytes),
		}, nil
	default:
		return nil, ErrUnavailable
	}
}

func normalizeDeviceSessionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, devicesessions.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, devicesessions.ErrInvalidRequest):
		return ErrInvalidRequest
	case errors.Is(err, devicesessions.ErrConflict):
		return ErrConflict
	case errors.Is(err, devicesessions.ErrDeviceOffline):
		return ErrDeviceOffline
	case errors.Is(err, devicesessions.ErrCapabilityNotSupported), errors.Is(err, devicesessions.ErrStopUnsupported):
		return ErrCapabilityNotSupported
	default:
		return ErrUnavailable
	}
}

type captureInput struct {
	SnapNum  int `json:"snapNum"`
	Interval int `json:"interval"`
}

func decodeCaptureInput(body []byte) (captureInput, error) {
	var input captureInput
	if len(body) == 0 || len(body) > 4096 {
		return input, ErrInvalidRequest
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(body), 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return captureInput{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return captureInput{}, ErrInvalidRequest
	}
	if input.SnapNum < 1 || input.SnapNum > 10 || input.Interval < 1 || input.Interval > 3600 {
		return captureInput{}, ErrInvalidRequest
	}
	return input, nil
}

func validResourceScope(scope resource.DepartmentScope) bool {
	return scope.OwnerDeptID > 0 && (scope.DataScope == resource.DataScopeDepartment || scope.DataScope == resource.DataScopeDepartmentAndChildren)
}

func validPathID(value string) bool {
	return len(value) == 20 && allDecimal(value)
}

func validSessionID(value string) bool {
	if len(value) != 36 {
		return false
	}
	_, err := uuid.Parse(value)
	return err == nil
}

func allDecimal(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

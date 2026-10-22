package adapters

import (
	"context"
	"errors"
	"strings"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	DeviceAlarmsScope                = "device:alarms"
	DeviceStatusEventsScope          = "device:status-events"
	DeviceMaintenanceListScope       = "device:maintenance:list"
	ChannelPreciseStatusScope        = "channel:precise-status"
	ChannelMountsReadScope           = "channel:mounts:read"
	ChannelTimelineReadScope         = "channel:timeline:read"
	ChannelHomePositionReadScope     = "ptz:home-position:read"
	ChannelCruiseTracksReadScope     = "ptz:cruise:read"
	ChannelTargetTrackReadScope      = "channel:target-track:read"
	DeviceAlarmsAdapterKey           = "devicequery.device.alarms.v1"
	DeviceStatusEventsAdapterKey     = "devicequery.device.status-events.v1"
	DeviceMaintenanceListAdapterKey  = "devicequery.device.maintenance-operations.v1"
	ChannelPreciseStatusAdapterKey   = "devicequery.channel.precise-status.v1"
	ChannelMountsReadAdapterKey      = "devicequery.channel.mounts.v1"
	ChannelTimelineReadAdapterKey    = "devicequery.channel.timeline.v1"
	ChannelHomePositionAdapterKey    = "devicequery.ptz.home-position.v1"
	ChannelCruiseTracksAdapterKey    = "devicequery.ptz.cruise-tracks.v1"
	ChannelTargetTrackReadAdapterKey = "devicequery.channel.target-track.v1"
)

type DeviceQueryReader interface {
	ListDeviceAlarmsInScope(context.Context, resource.DepartmentScope, string, resource.DeviceListOptions) (devicequery.AlarmPage, error)
	ListDeviceStatusEventsInScope(context.Context, resource.DepartmentScope, string, int, string) (devicequery.StatusEventPage, error)
	ListDeviceMaintenanceOperationsInScope(context.Context, resource.DepartmentScope, string, resource.DeviceListOptions) (devicequery.MaintenanceOperationPage, error)
	GetChannelPreciseStatusInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.PreciseStatusSnapshot, error)
	ListChannelMountsInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.ChannelMountList, error)
	GetChannelTimelineInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.ChannelTimeline, error)
	GetChannelHomePositionInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.HomePositionSnapshot, error)
	ListChannelCruiseTracksInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.CruiseTrackSnapshot, error)
	GetChannelTargetTrackInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.TargetTrackSnapshot, error)
}

type deviceQueryOperation uint8

const (
	listDeviceAlarms deviceQueryOperation = iota + 1
	listDeviceStatusEvents
	listDeviceMaintenance
	getChannelPreciseStatus
	listChannelMounts
	getChannelTimeline
	getChannelHomePosition
	listChannelCruiseTracks
	getChannelTargetTrack
)

type deviceQueryAdapter struct {
	key       string
	scope     string
	operation deviceQueryOperation
	reader    DeviceQueryReader
}

var _ Adapter = (*deviceQueryAdapter)(nil)
var _ catalogruntime.Adapter = (*deviceQueryAdapter)(nil)
var _ DeviceQueryReader = (*devicequery.Service)(nil)

func NewDeviceQueryAdapters(reader DeviceQueryReader) []Adapter {
	return []Adapter{
		&deviceQueryAdapter{key: DeviceAlarmsAdapterKey, scope: DeviceAlarmsScope, operation: listDeviceAlarms, reader: reader},
		&deviceQueryAdapter{key: DeviceStatusEventsAdapterKey, scope: DeviceStatusEventsScope, operation: listDeviceStatusEvents, reader: reader},
		&deviceQueryAdapter{key: DeviceMaintenanceListAdapterKey, scope: DeviceMaintenanceListScope, operation: listDeviceMaintenance, reader: reader},
		&deviceQueryAdapter{key: ChannelPreciseStatusAdapterKey, scope: ChannelPreciseStatusScope, operation: getChannelPreciseStatus, reader: reader},
		&deviceQueryAdapter{key: ChannelMountsReadAdapterKey, scope: ChannelMountsReadScope, operation: listChannelMounts, reader: reader},
		&deviceQueryAdapter{key: ChannelTimelineReadAdapterKey, scope: ChannelTimelineReadScope, operation: getChannelTimeline, reader: reader},
		&deviceQueryAdapter{key: ChannelHomePositionAdapterKey, scope: ChannelHomePositionReadScope, operation: getChannelHomePosition, reader: reader},
		&deviceQueryAdapter{key: ChannelCruiseTracksAdapterKey, scope: ChannelCruiseTracksReadScope, operation: listChannelCruiseTracks, reader: reader},
		&deviceQueryAdapter{key: ChannelTargetTrackReadAdapterKey, scope: ChannelTargetTrackReadScope, operation: getChannelTargetTrack, reader: reader},
	}
}

func NewDeviceQueryAdapterRegistrations(reader DeviceQueryReader) []catalogruntime.AdapterRegistration {
	registered := NewDeviceQueryAdapters(reader)
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(registered))
	for _, adapter := range registered {
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: adapter.Key(), ContractVersion: ResourceAdapterContractVersion, Adapter: adapter,
		})
	}
	return registrations
}

func (adapter *deviceQueryAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

func (adapter *deviceQueryAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (data any, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, ErrUnavailable
		}
	}()
	if adapter == nil || adapter.reader == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != "" && invocation.Scope != adapter.scope {
		return nil, ErrInvalidRequest
	}
	if invocation.Method != "" && strings.ToUpper(invocation.Method) != "GET" {
		return nil, ErrInvalidRequest
	}
	if len(invocation.Body) != 0 {
		return nil, ErrInvalidRequest
	}
	request, ok := invocation.Value.(Request)
	if !ok {
		return nil, ErrInvalidRequest
	}
	if !sameDeviceQueryParams(adapter.operation, invocation.Params, request) {
		return nil, ErrInvalidRequest
	}
	if !sameDeviceQuery(invocation.Query, request.Query) {
		return nil, ErrInvalidRequest
	}
	return adapter.Dispatch(ctx, request)
}

func sameDeviceQueryParams(operation deviceQueryOperation, params map[string]string, request Request) bool {
	expected := map[string]string{"deviceId": request.DeviceID}
	switch operation {
	case getChannelPreciseStatus, listChannelMounts, getChannelTimeline, getChannelHomePosition, listChannelCruiseTracks, getChannelTargetTrack:
		expected["channelId"] = request.ChannelID
	case listDeviceAlarms, listDeviceStatusEvents, listDeviceMaintenance:
	default:
		return false
	}
	if request.DeviceID == "" || (len(expected) == 2 && request.ChannelID == "") || len(params) != len(expected) {
		return false
	}
	for key, value := range expected {
		if params[key] != value {
			return false
		}
	}
	return true
}

func sameDeviceQuery(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func (adapter *deviceQueryAdapter) Dispatch(ctx context.Context, request Request) (any, error) {
	if adapter == nil || adapter.reader == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, err
	}
	if request.List.Keyword != "" || request.List.Status != "" {
		return nil, ErrInvalidRequest
	}
	var data any
	var err error
	switch adapter.operation {
	case listDeviceAlarms:
		if request.ChannelID != "" || request.Cursor != "" {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.reader.ListDeviceAlarmsInScope(ctx, request.ResourceScope, request.DeviceID, resource.DeviceListOptions{
			Page: request.List.Page, PageSize: request.List.PageSize,
		})
	case listDeviceStatusEvents:
		if request.ChannelID != "" || request.List.Page < 0 || request.List.Page > 1 {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.reader.ListDeviceStatusEventsInScope(ctx, request.ResourceScope, request.DeviceID, request.List.PageSize, request.Cursor)
	case listDeviceMaintenance:
		if request.ChannelID != "" || request.Cursor != "" {
			return nil, ErrInvalidRequest
		}
		data, err = adapter.reader.ListDeviceMaintenanceOperationsInScope(ctx, request.ResourceScope, request.DeviceID, resource.DeviceListOptions{
			Page: request.List.Page, PageSize: request.List.PageSize,
		})
	case listChannelMounts:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.ListChannelMountsInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	case getChannelTimeline:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.GetChannelTimelineInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	case getChannelPreciseStatus:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.GetChannelPreciseStatusInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	case getChannelHomePosition:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.GetChannelHomePositionInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	case listChannelCruiseTracks:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.ListChannelCruiseTracksInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	case getChannelTargetTrack:
		if err = validateChannelReadRequest(request); err == nil {
			data, err = adapter.reader.GetChannelTargetTrackInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
		}
	default:
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, normalizeDeviceQueryError(err)
	}
	return data, nil
}

func validateChannelReadRequest(request Request) error {
	if request.DeviceID == "" || request.ChannelID == "" || request.Cursor != "" || request.List.Page < 0 || request.List.Page > 1 {
		return ErrInvalidRequest
	}
	return nil
}

func normalizeDeviceQueryError(err error) error {
	switch {
	case errors.Is(err, resource.ErrResourceNotFound):
		return ErrNotFound
	case errors.Is(err, resource.ErrInvalidListOptions), errors.Is(err, resource.ErrInvalidDepartmentScope):
		return ErrInvalidRequest
	case errors.Is(err, resource.ErrResourceUnavailable):
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}

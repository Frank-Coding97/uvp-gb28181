package adapters

import (
	"context"
	"errors"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	DeviceListScope               = "device:list"
	DeviceDetailScope             = "device:detail"
	DeviceStatusScope             = "device:status"
	ChannelListScope              = "channel:list"
	ChannelDetailScope            = "channel:detail"
	ChannelStatusScope            = "channel:status"
	ChannelStorageCardsScope      = "channel:storage-cards"
	DeviceListAdapterKey          = "resource.device.list.v1"
	DeviceDetailAdapterKey        = "resource.device.detail.v1"
	DeviceStatusAdapterKey        = "resource.device.status.v1"
	ChannelListAdapterKey         = "resource.channel.list.v1"
	ChannelDetailAdapterKey       = "resource.channel.detail.v1"
	ChannelStatusAdapterKey       = "resource.channel.status.v1"
	ChannelStorageCardsAdapterKey = "resource.channel.storage-cards.v1"
	ChannelCapabilitiesScope      = "channel:capabilities"
	ChannelCapabilitiesAdapterKey = "resource.channel.capabilities.v1"
)

type resourceOperation uint8

const (
	deviceList resourceOperation = iota + 1
	deviceDetail
	deviceStatus
	channelList
	channelDetail
	channelStatus
	channelStorageCards
	channelCapabilities
)

// ResourceReader is the application-service contract reused by the external
// device/channel surface. It purposefully has no controller or HTTP methods.
type ResourceReader interface {
	ListDevicesInScope(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DevicePage, error)
	GetDeviceInScope(context.Context, resource.DepartmentScope, string) (resource.Device, error)
	GetDeviceStatusInScope(context.Context, resource.DepartmentScope, string) (resource.DeviceStatus, error)
	ListChannelsInScope(context.Context, resource.DepartmentScope, string, resource.ChannelListOptions) (resource.ChannelPage, error)
	GetChannelInScope(context.Context, resource.DepartmentScope, string, string) (resource.Channel, error)
	GetChannelStatusInScope(context.Context, resource.DepartmentScope, string, string) (resource.ChannelStatus, error)
	GetChannelStorageCardsInScope(context.Context, resource.DepartmentScope, string, string) (resource.StorageCardSnapshot, error)
	GetChannelControlCapabilitiesInScope(context.Context, resource.DepartmentScope, string, string) (manscdp.ControlCapabilities, error)
}

type resourceReadAdapter struct {
	key       string
	scope     string
	operation resourceOperation
	reader    ResourceReader
}

var _ Adapter = (*resourceReadAdapter)(nil)
var _ catalogruntime.Adapter = (*resourceReadAdapter)(nil)
var _ ResourceReader = (*resource.Service)(nil)

// NewDeviceChannelAdapters returns the complete code-owned device/channel
// read surface. Capability release metadata maps individual external scopes
// to these keys; it does not supply executable behavior.
func NewDeviceChannelAdapters(reader ResourceReader) []Adapter {
	return []Adapter{
		&resourceReadAdapter{key: DeviceListAdapterKey, scope: DeviceListScope, operation: deviceList, reader: reader},
		&resourceReadAdapter{key: DeviceDetailAdapterKey, scope: DeviceDetailScope, operation: deviceDetail, reader: reader},
		&resourceReadAdapter{key: DeviceStatusAdapterKey, scope: DeviceStatusScope, operation: deviceStatus, reader: reader},
		&resourceReadAdapter{key: ChannelListAdapterKey, scope: ChannelListScope, operation: channelList, reader: reader},
		&resourceReadAdapter{key: ChannelDetailAdapterKey, scope: ChannelDetailScope, operation: channelDetail, reader: reader},
		&resourceReadAdapter{key: ChannelStatusAdapterKey, scope: ChannelStatusScope, operation: channelStatus, reader: reader},
		&resourceReadAdapter{key: ChannelStorageCardsAdapterKey, scope: ChannelStorageCardsScope, operation: channelStorageCards, reader: reader},
		&resourceReadAdapter{key: ChannelCapabilitiesAdapterKey, scope: ChannelCapabilitiesScope, operation: channelCapabilities, reader: reader},
	}
}

func (adapter *resourceReadAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

// Execute is the shared catalog runtime contract. The authenticated gateway
// puts the typed Request in Invocation.Value; route maps and HTTP bodies are
// intentionally not interpreted by this application-service adapter.
func (adapter *resourceReadAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (data any, err error) {
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
	if !validResourceQuery(adapter.scope, invocation.Query, request.Query) {
		return nil, ErrInvalidRequest
	}
	return adapter.Dispatch(ctx, request)
}

func validResourceQuery(scope string, invocationQuery, requestQuery map[string]string) bool {
	if len(invocationQuery) != len(requestQuery) {
		return false
	}
	for key, value := range invocationQuery {
		if requestQuery[key] != value {
			return false
		}
	}

	var (
		allowed map[string]bool
		found   bool
	)
	for _, operation := range contracts.Operations() {
		if operation.Scope != scope {
			continue
		}
		found = true
		allowed = make(map[string]bool, len(operation.QueryAllowlist))
		for _, name := range operation.QueryAllowlist {
			allowed[name] = true
		}
		break
	}
	if !found {
		return false
	}
	for key, values := range invocationQuery {
		if !allowed[key] {
			return false
		}
		if key != "keyword" && values == "" {
			return false
		}
	}
	return true
}

func (adapter *resourceReadAdapter) Dispatch(ctx context.Context, request Request) (any, error) {
	if adapter == nil || adapter.reader == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, err
	}
	var (
		data any
		err  error
	)
	switch adapter.operation {
	case deviceList:
		data, err = adapter.reader.ListDevicesInScope(ctx, request.ResourceScope, resource.DeviceListOptions{
			Page: request.List.Page, PageSize: request.List.PageSize, Keyword: request.List.Keyword, Status: request.List.Status,
		})
	case deviceDetail:
		data, err = adapter.reader.GetDeviceInScope(ctx, request.ResourceScope, request.DeviceID)
	case deviceStatus:
		data, err = adapter.reader.GetDeviceStatusInScope(ctx, request.ResourceScope, request.DeviceID)
	case channelList:
		data, err = adapter.reader.ListChannelsInScope(ctx, request.ResourceScope, request.DeviceID, resource.ChannelListOptions{
			Page: request.List.Page, PageSize: request.List.PageSize, Keyword: request.List.Keyword, Status: request.List.Status,
		})
	case channelDetail:
		data, err = adapter.reader.GetChannelInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	case channelStatus:
		data, err = adapter.reader.GetChannelStatusInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	case channelStorageCards:
		data, err = adapter.reader.GetChannelStorageCardsInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	case channelCapabilities:
		data, err = adapter.reader.GetChannelControlCapabilitiesInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	default:
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, normalizeResourceError(err)
	}
	return data, nil
}

func validateResourceScope(scope resource.DepartmentScope) error {
	if scope.OwnerDeptID == 0 {
		return ErrNotFound
	}
	if scope.DataScope != resource.DataScopeDepartment && scope.DataScope != resource.DataScopeDepartmentAndChildren {
		return ErrInvalidRequest
	}
	return nil
}

func normalizeResourceError(err error) error {
	switch {
	case errors.Is(err, resource.ErrResourceNotFound):
		return ErrNotFound
	case errors.Is(err, resource.ErrInvalidListOptions), errors.Is(err, resource.ErrInvalidDepartmentScope):
		return ErrInvalidRequest
	default:
		return ErrUnavailable
	}
}

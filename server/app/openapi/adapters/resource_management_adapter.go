package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	DeviceStatisticsScope       = "device:statistics"
	ChannelStatisticsScope      = "channel:statistics"
	DeviceUpdateScope           = "device:update"
	ChannelUpdateScope          = "channel:update"
	DeviceStatisticsAdapterKey  = "device.statistics.v1"
	ChannelStatisticsAdapterKey = "device.channel-statistics.v1"
	DeviceUpdateAdapterKey      = "device.resource-update.v1"
	ChannelUpdateAdapterKey     = "device.channel-resource-update.v1"
)

type ResourceManagementService interface {
	GetDeviceStatisticsInScope(context.Context, resource.DepartmentScope, resource.DeviceListOptions) (resource.DeviceStatistics, error)
	GetChannelStatisticsInScope(context.Context, resource.DepartmentScope, string, resource.ChannelListOptions) (resource.ChannelStatistics, error)
	UpdateDeviceInScope(context.Context, resource.DepartmentScope, string, resource.DeviceUpdate) (resource.Device, error)
	UpdateChannelInScope(context.Context, resource.DepartmentScope, string, string, resource.ChannelUpdate) (resource.Channel, error)
}

var _ ResourceManagementService = (*resource.Service)(nil)

type resourceManagementAdapter struct {
	key, scope, method string
	service            ResourceManagementService
}

func NewResourceManagementAdapterRegistrations(service ResourceManagementService) []catalogruntime.AdapterRegistration {
	definitions := []resourceManagementAdapter{
		{DeviceStatisticsAdapterKey, DeviceStatisticsScope, "GET", service},
		{ChannelStatisticsAdapterKey, ChannelStatisticsScope, "GET", service},
		{DeviceUpdateAdapterKey, DeviceUpdateScope, "POST", service},
		{ChannelUpdateAdapterKey, ChannelUpdateScope, "POST", service},
	}
	result := make([]catalogruntime.AdapterRegistration, 0, len(definitions))
	for i := range definitions {
		a := &definitions[i]
		result = append(result, catalogruntime.AdapterRegistration{Key: a.key, ContractVersion: ResourceAdapterContractVersion, Adapter: a})
	}
	return result
}
func (a *resourceManagementAdapter) Execute(ctx context.Context, inv catalogruntime.Invocation) (data any, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, ErrUnavailable
		}
	}()
	if a == nil || a.service == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	req, ok := inv.Value.(Request)
	if !ok || req.ClientID <= 0 || inv.Scope != a.scope || strings.ToUpper(inv.Method) != a.method {
		return nil, ErrInvalidRequest
	}
	if err = validateResourceScope(req.ResourceScope); err != nil {
		return nil, err
	}
	expected := 0
	if a.scope != DeviceStatisticsScope {
		expected++
		if !validConfigPathID(req.DeviceID) || inv.Params["deviceId"] != req.DeviceID {
			return nil, ErrInvalidRequest
		}
	}
	if a.scope == ChannelUpdateScope {
		expected++
		if !validConfigPathID(req.ChannelID) || inv.Params["channelId"] != req.ChannelID {
			return nil, ErrInvalidRequest
		}
	}
	if len(inv.Params) != expected {
		return nil, ErrInvalidRequest
	}
	if a.method == "GET" {
		if len(inv.Body) != 0 || !validStatisticsQuery(a.scope, inv.Query, req.Query) {
			return nil, ErrInvalidRequest
		}
	} else if len(inv.Query) != 0 || len(req.Query) != 0 {
		return nil, ErrInvalidRequest
	}
	switch a.scope {
	case DeviceStatisticsScope:
		options, e := deviceResourceOptions(req)
		if e != nil {
			return nil, e
		}
		data, err = a.service.GetDeviceStatisticsInScope(ctx, req.ResourceScope, options)
	case ChannelStatisticsScope:
		options, e := channelResourceOptions(req)
		if e != nil {
			return nil, e
		}
		data, err = a.service.GetChannelStatisticsInScope(ctx, req.ResourceScope, req.DeviceID, options)
	case DeviceUpdateScope:
		var input resource.DeviceUpdate
		if decodeResourceUpdate(inv.Body, &input) != nil || input.Validate() != nil {
			return nil, ErrInvalidRequest
		}
		data, err = a.service.UpdateDeviceInScope(ctx, req.ResourceScope, req.DeviceID, input)
	case ChannelUpdateScope:
		var input resource.ChannelUpdate
		if decodeResourceUpdate(inv.Body, &input) != nil || input.Validate() != nil {
			return nil, ErrInvalidRequest
		}
		data, err = a.service.UpdateChannelInScope(ctx, req.ResourceScope, req.DeviceID, req.ChannelID, input)
	default:
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, normalizeResourceError(err)
	}
	return data, nil
}
func validStatisticsQuery(scope string, inv, req map[string]string) bool {
	if len(inv) != len(req) {
		return false
	}
	allowed := map[string]bool{"keyword": true, "status": true, "manufacturer": true, "model": true}
	if scope == DeviceStatisticsScope {
		allowed["transport"], allowed["protocolVersion"] = true, true
	} else {
		allowed["ptzType"], allowed["audioEnabled"], allowed["streamTransport"] = true, true, true
	}
	for k, v := range inv {
		if !allowed[k] || req[k] != v || v == "" && k != "keyword" {
			return false
		}
	}
	return true
}
func decodeResourceUpdate(body []byte, target any) error {
	if !utf8.Valid(body) || hasDuplicateMaintenanceJSONMember(body) || decodeStrictDeviceConfigJSON(body, target) != nil {
		return ErrInvalidRequest
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil || len(members) == 0 {
		return ErrInvalidRequest
	}
	for _, v := range members {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalidRequest
		}
	}
	return nil
}
func deviceResourceOptions(req Request) (resource.DeviceListOptions, error) {
	o := resource.DeviceListOptions{Page: req.List.Page, PageSize: req.List.PageSize, Keyword: req.List.Keyword, Status: req.List.Status, Manufacturer: req.Query["manufacturer"], Model: req.Query["model"], Transport: req.Query["transport"], ProtocolVersion: req.Query["protocolVersion"], Sort: req.Query["sort"], Order: req.Query["order"]}
	// Statistics invocations use the query map without the paginated read metadata path.
	if v, ok := req.Query["keyword"]; ok {
		o.Keyword = v
	}
	if v, ok := req.Query["status"]; ok {
		o.Status = v
	}
	if o.Validate() != nil {
		return o, ErrInvalidRequest
	}
	return o, nil
}
func channelResourceOptions(req Request) (resource.ChannelListOptions, error) {
	o := resource.ChannelListOptions{Page: req.List.Page, PageSize: req.List.PageSize, Keyword: req.List.Keyword, Status: req.List.Status, Manufacturer: req.Query["manufacturer"], Model: req.Query["model"], StreamTransport: req.Query["streamTransport"], Sort: req.Query["sort"], Order: req.Query["order"]}
	if v, ok := req.Query["keyword"]; ok {
		o.Keyword = v
	}
	if v, ok := req.Query["status"]; ok {
		o.Status = v
	}
	if v, ok := req.Query["ptzType"]; ok {
		n, e := strconv.ParseInt(v, 10, 8)
		if e != nil || strconv.FormatInt(n, 10) != v {
			return o, ErrInvalidRequest
		}
		value := int8(n)
		o.PTZType = &value
	}
	if v, ok := req.Query["audioEnabled"]; ok {
		if v != "true" && v != "false" {
			return o, ErrInvalidRequest
		}
		value := v == "true"
		o.AudioEnabled = &value
	}
	if o.Validate() != nil {
		return o, ErrInvalidRequest
	}
	return o, nil
}

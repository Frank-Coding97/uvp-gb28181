package cloudrecord

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

const (
	ListScope       = "channel:cloud-record:list"
	InfoScope       = "channel:cloud-record:info"
	DatesScope      = "channel:cloud-record:dates"
	ListAdapterKey  = "device.cloudrecord.list.v1"
	InfoAdapterKey  = "device.cloudrecord.info.v1"
	DatesAdapterKey = "device.cloudrecord.dates.v1"
)

type operationAdapter struct {
	service *Service
	scope   string
}

func NewAdapterRegistrations(service *Service) []catalogruntime.AdapterRegistration {
	return []catalogruntime.AdapterRegistration{
		{Key: ListAdapterKey, ContractVersion: "v1", Adapter: operationAdapter{service, ListScope}},
		{Key: InfoAdapterKey, ContractVersion: "v1", Adapter: operationAdapter{service, InfoScope}},
		{Key: DatesAdapterKey, ContractVersion: "v1", Adapter: operationAdapter{service, DatesScope}},
	}
}
func (a operationAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a.service == nil {
		return nil, adapters.ErrUnavailable
	}
	request, ok := invocation.Value.(adapters.Request)
	if !ok || request.ClientID <= 0 || invocation.Scope != a.scope || invocation.Method != http.MethodGet || len(invocation.Body) != 0 || request.DeviceID == "" || request.ChannelID == "" || invocation.Params["deviceId"] != request.DeviceID || invocation.Params["channelId"] != request.ChannelID {
		return nil, adapters.ErrInvalidRequest
	}
	if len(invocation.Query) != len(request.Query) {
		return nil, adapters.ErrInvalidRequest
	}
	for key, value := range invocation.Query {
		if actual, exists := request.Query[key]; !exists || actual != value {
			return nil, adapters.ErrInvalidRequest
		}
	}
	switch a.scope {
	case ListScope:
		if len(invocation.Params) != 2 {
			return nil, adapters.ErrInvalidRequest
		}
		options, err := listOptions(invocation.Query)
		if err != nil {
			return nil, err
		}
		return a.service.List(ctx, request.ResourceScope, request.DeviceID, request.ChannelID, options)
	case InfoScope:
		if len(invocation.Query) != 0 || len(invocation.Params) != 3 {
			return nil, adapters.ErrInvalidRequest
		}
		return a.service.Info(ctx, request.ResourceScope, request.DeviceID, request.ChannelID, invocation.Params["fileId"])
	case DatesScope:
		if len(invocation.Params) != 2 || len(invocation.Query) != 2 || invocation.Query["startDate"] == "" || invocation.Query["endDate"] == "" {
			return nil, adapters.ErrInvalidRequest
		}
		return a.service.Dates(ctx, request.ResourceScope, request.DeviceID, request.ChannelID, DateOptions{StartDate: invocation.Query["startDate"], EndDate: invocation.Query["endDate"]})
	default:
		return nil, adapters.ErrUnavailable
	}
}
func listOptions(query map[string]string) (Options, error) {
	options := Options{Page: 1, PageSize: 20, MetadataState: query["metadataState"]}
	for key := range query {
		switch key {
		case "page", "pageSize", "startTime", "endTime", "metadataState":
		default:
			return options, adapters.ErrInvalidRequest
		}
	}
	for _, item := range []struct {
		key    string
		target *int
	}{{"page", &options.Page}, {"pageSize", &options.PageSize}} {
		if value, ok := query[item.key]; ok {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 {
				return options, adapters.ErrInvalidRequest
			}
			*item.target = parsed
		}
	}
	for _, item := range []struct {
		key    string
		target **time.Time
	}{{"startTime", &options.StartTime}, {"endTime", &options.EndTime}} {
		if value, ok := query[item.key]; ok {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return options, adapters.ErrInvalidRequest
			}
			*item.target = &parsed
		}
	}
	return options, nil
}

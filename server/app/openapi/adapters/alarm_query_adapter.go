package adapters

import (
	"context"
	"strconv"
	"time"

	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const DeviceAlarmDetailScope = "device:alarm:detail"
const DeviceAlarmDetailAdapterKey = "devicequery.device.alarm-detail.v1"

type DeviceAlarmReader interface {
	SearchDeviceAlarmsInScope(context.Context, resource.DepartmentScope, string, devicequery.AlarmFilter) (devicequery.AlarmPage, error)
	GetDeviceAlarmInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.DeviceAlarm, error)
}

type alarmDetailAdapter struct{ reader DeviceAlarmReader }

func NewDeviceAlarmAdapterRegistrations(reader DeviceAlarmReader) []catalogruntime.AdapterRegistration {
	return []catalogruntime.AdapterRegistration{{Key: DeviceAlarmDetailAdapterKey, ContractVersion: "v1", Adapter: &alarmDetailAdapter{reader: reader}}}
}

func (a *alarmDetailAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a == nil || a.reader == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	request, ok := invocation.Value.(Request)
	if !ok || invocation.Scope != DeviceAlarmDetailScope || invocation.Method != "GET" || len(invocation.Body) != 0 || len(invocation.Query) != 0 || len(request.Query) != 0 || len(invocation.Params) != 2 || invocation.Params["deviceId"] != request.DeviceID || invocation.Params["alarmId"] == "" || request.ChannelID != "" {
		return nil, ErrInvalidRequest
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, err
	}
	data, err := a.reader.GetDeviceAlarmInScope(ctx, request.ResourceScope, request.DeviceID, invocation.Params["alarmId"])
	if err != nil {
		return nil, normalizeDeviceQueryError(err)
	}
	return data, nil
}

func alarmFilterFromRequest(request Request) (devicequery.AlarmFilter, error) {
	filter := devicequery.AlarmFilter{DeviceListOptions: resource.DeviceListOptions{Page: request.List.Page, PageSize: request.List.PageSize, Keyword: request.List.Keyword}, SourceCode: request.Query["sourceCode"]}
	for key := range request.Query {
		switch key {
		case "page", "pageSize", "keyword", "sourceCode", "alarmFrom", "alarmTo", "priority", "method", "alarmType":
		default:
			return filter, ErrInvalidRequest
		}
	}
	for _, item := range []struct {
		key    string
		target **int
	}{{"priority", &filter.Priority}, {"method", &filter.Method}, {"alarmType", &filter.AlarmType}} {
		if value, ok := request.Query[item.key]; ok {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return filter, ErrInvalidRequest
			}
			*item.target = &parsed
		}
	}
	for _, item := range []struct {
		key    string
		target **time.Time
	}{{"alarmFrom", &filter.AlarmFrom}, {"alarmTo", &filter.AlarmTo}} {
		if value, ok := request.Query[item.key]; ok {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return filter, ErrInvalidRequest
			}
			*item.target = &parsed
		}
	}
	return filter, nil
}

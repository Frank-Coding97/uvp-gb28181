package adapters

import (
	"context"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const ChannelControlStatusScope = "channel:control-status:read"
const ChannelControlStatusAdapterKey = "devicequery.channel.control-status.v1"

type ChannelControlStatusReader interface {
	GetChannelControlStatusInScope(context.Context, resource.DepartmentScope, string, string) (devicequery.ChannelControlStatus, error)
}
type controlStatusAdapter struct{ reader ChannelControlStatusReader }

func NewChannelControlStatusAdapterRegistrations(reader ChannelControlStatusReader) []catalogruntime.AdapterRegistration {
	return []catalogruntime.AdapterRegistration{{Key: ChannelControlStatusAdapterKey, ContractVersion: "v1", Adapter: &controlStatusAdapter{reader: reader}}}
}
func (a *controlStatusAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if a == nil || a.reader == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	request, ok := invocation.Value.(Request)
	if !ok || invocation.Scope != ChannelControlStatusScope || invocation.Method != "GET" || len(invocation.Body) != 0 || len(invocation.Query) != 0 || len(request.Query) != 0 || len(invocation.Params) != 2 || invocation.Params["deviceId"] != request.DeviceID || invocation.Params["channelId"] != request.ChannelID {
		return nil, ErrInvalidRequest
	}
	if err := validateResourceScope(request.ResourceScope); err != nil {
		return nil, err
	}
	data, err := a.reader.GetChannelControlStatusInScope(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	if err != nil {
		return nil, normalizeDeviceQueryError(err)
	}
	return data, nil
}

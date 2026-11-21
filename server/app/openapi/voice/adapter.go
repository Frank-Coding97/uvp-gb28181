package voice

import (
	"context"
	"encoding/json"
	"net/http"

	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

type Adapter struct {
	scope      string
	dispatcher *Dispatcher
	uplink     *http.Client
}

func NewAdapter(scope string, dispatcher *Dispatcher) *Adapter {
	return &Adapter{scope: scope, dispatcher: dispatcher}
}

func NewMediaAdapter(scope string, dispatcher *Dispatcher, uplink *http.Client) *Adapter {
	return &Adapter{scope: scope, dispatcher: dispatcher, uplink: uplink}
}

func (adapter *Adapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if adapter == nil || adapter.dispatcher == nil || !adapter.dispatcher.Ready() {
		return nil, adapters.ErrUnavailable
	}
	if invocation.Scope != adapter.scope || invocation.Method != methodForScope(adapter.scope) || len(invocation.Query) != 0 {
		return nil, adapters.ErrInvalidRequest
	}
	request, ok := invocation.Value.(Request)
	if !ok {
		base, baseOK := invocation.Value.(adapters.Request)
		if !baseOK {
			return nil, adapters.ErrInvalidRequest
		}
		request = Request{
			ClientID: base.ClientID, IdempotencyKey: base.IdempotencyKey, ResourceScope: base.ResourceScope,
			DeviceID: invocation.Params["deviceId"], ChannelID: invocation.Params["channelId"], SessionID: invocation.Params["sessionId"],
		}
	}
	if !paramsMatch(adapter.scope, request, invocation.Params) {
		return nil, adapters.ErrInvalidRequest
	}
	if isMediaScope(adapter.scope) {
		var body struct {
			SDP string `json:"sdp"`
		}
		if json.Unmarshal(invocation.Body, &body) != nil || !validSDP([]byte(body.SDP)) {
			return nil, adapters.ErrInvalidRequest
		}
		uplink := adapter.uplink
		if uplink == nil {
			uplink = talk.NewUplinkProxyClient()
		}
		return adapter.dispatcher.ProxyMedia(ctx, adapter.scope, request, []byte(body.SDP), uplink)
	}
	if !validEmptyBody(adapter.scope, invocation.Body) {
		return nil, adapters.ErrInvalidRequest
	}
	return adapter.dispatcher.Handle(ctx, adapter.scope, request)
}

func validEmptyBody(scope string, body []byte) bool {
	if methodForScope(scope) == http.MethodGet {
		return len(body) == 0
	}
	var fields map[string]json.RawMessage
	return len(body) > 0 && json.Unmarshal(body, &fields) == nil && fields != nil && len(fields) == 0
}

func NewAdapterRegistrations(dispatcher *Dispatcher) []catalogruntime.AdapterRegistration {
	entries := []struct{ key, scope string }{
		{TalkCreateAdapterKey, ScopeTalkCreate}, {TalkReadAdapterKey, ScopeTalkRead}, {TalkEndAdapterKey, ScopeTalkEnd},
		{BroadcastCreateAdapterKey, ScopeBroadcastCreate}, {BroadcastReadAdapterKey, ScopeBroadcastRead}, {BroadcastEndAdapterKey, ScopeBroadcastEnd},
		{TalkMediaAdapterKey, ScopeTalkMedia}, {BroadcastMediaAdapterKey, ScopeBroadcastMedia},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(entries))
	for _, entry := range entries {
		adapter := NewAdapter(entry.scope, dispatcher)
		if isMediaScope(entry.scope) {
			adapter = NewMediaAdapter(entry.scope, dispatcher, talk.NewUplinkProxyClient())
		}
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key: entry.key, ContractVersion: "v1", Adapter: adapter,
		})
	}
	return registrations
}

func methodForScope(scope string) string {
	switch scope {
	case ScopeTalkCreate, ScopeBroadcastCreate, ScopeTalkEnd, ScopeBroadcastEnd, ScopeTalkMedia, ScopeBroadcastMedia:
		return http.MethodPost
	case ScopeTalkRead, ScopeBroadcastRead:
		return http.MethodGet
	default:
		return ""
	}
}

func isMediaScope(scope string) bool {
	return scope == ScopeTalkMedia || scope == ScopeBroadcastMedia
}

func paramsMatch(scope string, request Request, params map[string]string) bool {
	expected := map[string]string{"deviceId": request.DeviceID, "channelId": request.ChannelID}
	if scope != ScopeTalkCreate && scope != ScopeBroadcastCreate {
		expected["sessionId"] = request.SessionID
	}
	if request.DeviceID == "" || request.ChannelID == "" || (scope != ScopeTalkCreate && scope != ScopeBroadcastCreate && request.SessionID == "") || len(params) != len(expected) {
		return false
	}
	for key, value := range expected {
		if params[key] != value {
			return false
		}
	}
	return true
}

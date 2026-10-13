package deviceevents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	SubscriptionCreateScope  = "device:subscription:create"
	SubscriptionReadScope    = "device:subscription:read"
	SubscriptionRenewScope   = "device:subscription:renew"
	SubscriptionCancelScope  = "device:subscription:cancel"
	DeviceEventsReadScope    = "device:events:read"
	DevicePositionReadScope  = "device:position:read"
	PositionHistoryReadScope = "device:position:history:read"

	SubscriptionCreateAdapterKey = "device.events.subscription.create.v1"
	SubscriptionReadAdapterKey   = "device.events.subscription.read.v1"
	SubscriptionRenewAdapterKey  = "device.events.subscription.renew.v1"
	SubscriptionCancelAdapterKey = "device.events.subscription.cancel.v1"
	DeviceEventsReadAdapterKey   = "device.events.read.v1"
	DevicePositionReadAdapterKey = "device.events.position.latest.v1"
	PositionHistoryAdapterKey    = "device.events.position.history.v1"
)

type operation uint8

const (
	createSubscription operation = iota + 1
	readSubscription
	renewSubscription
	cancelSubscription
	pollEvents
	readLatestPositions
	readPositionHistory
)

type Dispatcher struct{ service *Service }

func NewDispatcher(service *Service) *Dispatcher { return &Dispatcher{service: service} }

func (dispatcher *Dispatcher) Dispatch(ctx context.Context, scope string, request Request) (any, error) {
	if dispatcher == nil || dispatcher.service == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	switch scope {
	case SubscriptionCreateScope:
		return dispatcher.service.CreateSubscription(ctx, request)
	case SubscriptionReadScope:
		return dispatcher.service.GetSubscription(ctx, request)
	case SubscriptionRenewScope:
		return dispatcher.service.RenewSubscription(ctx, request)
	case SubscriptionCancelScope:
		return dispatcher.service.CancelSubscription(ctx, request)
	case DeviceEventsReadScope:
		return dispatcher.service.PollEvents(ctx, request)
	case DevicePositionReadScope:
		return dispatcher.service.GetLatestPositions(ctx, request)
	case PositionHistoryReadScope:
		return dispatcher.service.ListPositionHistory(ctx, request)
	default:
		return nil, ErrInvalidRequest
	}
}

type operationAdapter struct {
	key        string
	scope      string
	method     string
	action     operation
	dispatcher *Dispatcher
}

var _ catalogruntime.Adapter = (*operationAdapter)(nil)

func NewAdapterRegistrations(dispatcher *Dispatcher) []catalogruntime.AdapterRegistration {
	definitions := []struct {
		key, scope, method string
		action             operation
	}{
		{SubscriptionCreateAdapterKey, SubscriptionCreateScope, http.MethodPost, createSubscription},
		{SubscriptionReadAdapterKey, SubscriptionReadScope, http.MethodGet, readSubscription},
		{SubscriptionRenewAdapterKey, SubscriptionRenewScope, http.MethodPost, renewSubscription},
		{SubscriptionCancelAdapterKey, SubscriptionCancelScope, http.MethodDelete, cancelSubscription},
		{DeviceEventsReadAdapterKey, DeviceEventsReadScope, http.MethodGet, pollEvents},
		{DevicePositionReadAdapterKey, DevicePositionReadScope, http.MethodGet, readLatestPositions},
		{PositionHistoryAdapterKey, PositionHistoryReadScope, http.MethodGet, readPositionHistory},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(definitions))
	for _, definition := range definitions {
		adapter := &operationAdapter{key: definition.key, scope: definition.scope, method: definition.method, action: definition.action, dispatcher: dispatcher}
		registrations = append(registrations, catalogruntime.AdapterRegistration{Key: definition.key, ContractVersion: "v1", Adapter: adapter})
	}
	return registrations
}

func (adapter *operationAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	if adapter == nil || adapter.dispatcher == nil || ctx == nil {
		return nil, ErrUnavailable
	}
	if invocation.Scope != adapter.scope {
		return nil, ErrInvalidRequest
	}
	if !strings.EqualFold(invocation.Method, adapter.method) {
		return nil, ErrInvalidRequest
	}
	request, err := buildRequest(adapter.action, invocation)
	if err != nil {
		return nil, err
	}
	return adapter.dispatcher.Dispatch(ctx, adapter.scope, request)
}

func buildRequest(action operation, invocation catalogruntime.Invocation) (Request, error) {
	transport, ok := invocation.Value.(adapters.Request)
	if !ok || transport.ClientID <= 0 || transport.ResourceScope.OwnerDeptID == 0 || !validDataScope(transport.ResourceScope.DataScope) {
		return Request{}, ErrInvalidRequest
	}
	deviceID := invocation.Params["deviceId"]
	if deviceID == "" || strings.TrimSpace(deviceID) != deviceID || transport.DeviceID != deviceID {
		return Request{}, ErrInvalidRequest
	}
	request := Request{
		OpenAPIClientID: transport.ClientID,
		ResourceScope:   transport.ResourceScope,
		DeviceID:        deviceID,
		Cursor:          transport.Cursor,
	}
	if !sameStringMap(invocation.Query, transport.Query) {
		return Request{}, ErrInvalidRequest
	}
	params := map[string]string{"deviceId": deviceID}
	queryAllowed := false
	bodyAllowed := false
	switch action {
	case createSubscription:
		bodyAllowed = true
	case readSubscription, renewSubscription, cancelSubscription, pollEvents:
		request.SubscriptionID = invocation.Params["subscriptionId"]
		params["subscriptionId"] = request.SubscriptionID
		if request.SubscriptionID == "" || strings.TrimSpace(request.SubscriptionID) != request.SubscriptionID {
			return Request{}, ErrInvalidRequest
		}
		bodyAllowed = action == renewSubscription
		queryAllowed = action == pollEvents
	case readLatestPositions, readPositionHistory:
		queryAllowed = action == readPositionHistory
	default:
		return Request{}, ErrInvalidRequest
	}
	if !exactParams(invocation.Params, params) || (!bodyAllowed && len(invocation.Body) != 0) {
		return Request{}, ErrInvalidRequest
	}
	if !queryAllowed && len(invocation.Query) != 0 {
		return Request{}, ErrInvalidRequest
	}
	if action == createSubscription {
		if len(invocation.Body) == 0 {
			return Request{}, ErrInvalidRequest
		}
		input, err := decodeSubscriptionInput(invocation.Body)
		if err != nil {
			return Request{}, err
		}
		request.Kind = input.Kind
		request.ExpiresSeconds = input.ExpiresSeconds
		request.IntervalSeconds = input.IntervalSeconds
	}
	if action == renewSubscription && !validEmptyJSONObject(invocation.Body) {
		return Request{}, ErrInvalidRequest
	}
	if queryAllowed {
		if len(invocation.Query) > 2 {
			return Request{}, ErrInvalidRequest
		}
		for key, value := range invocation.Query {
			switch key {
			case "cursor":
				if value == "" || value != request.Cursor {
					return Request{}, ErrInvalidRequest
				}
			case "pageSize":
				if !decimalString(value) {
					return Request{}, ErrInvalidRequest
				}
				pageSize, err := strconv.Atoi(value)
				if err != nil || pageSize < 1 || pageSize > maxPageSize {
					return Request{}, ErrInvalidRequest
				}
				request.PageSize = pageSize
			default:
				return Request{}, ErrInvalidRequest
			}
		}
		if _, hasCursor := invocation.Query["cursor"]; !hasCursor && request.Cursor != "" {
			return Request{}, ErrInvalidRequest
		}
	}
	return request, nil
}

func validEmptyJSONObject(body []byte) bool {
	var fields map[string]json.RawMessage
	return json.Unmarshal(body, &fields) == nil && fields != nil && len(fields) == 0
}

func exactParams(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		if strings.TrimSpace(value) == "" || actual[key] != value {
			return false
		}
	}
	return true
}

func sameStringMap(left, right map[string]string) bool {
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

func validDataScope(scope int8) bool {
	return scope == resource.DataScopeDepartment || scope == resource.DataScopeDepartmentAndChildren
}

func decimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

type subscriptionInput struct {
	Kind            EventKind `json:"kind"`
	ExpiresSeconds  int       `json:"expiresSeconds"`
	IntervalSeconds int       `json:"intervalSeconds"`
}

func decodeSubscriptionInput(body []byte) (subscriptionInput, error) {
	var input subscriptionInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return subscriptionInput{}, ErrInvalidRequest
	}
	if _, exists := fields["kind"]; !exists {
		return subscriptionInput{}, ErrInvalidRequest
	}
	for name := range fields {
		if name != "kind" && name != "expiresSeconds" && name != "intervalSeconds" {
			return subscriptionInput{}, ErrInvalidRequest
		}
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return subscriptionInput{}, ErrInvalidRequest
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return subscriptionInput{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return subscriptionInput{}, ErrInvalidRequest
	}
	if !supportedKind(input.Kind) {
		return subscriptionInput{}, ErrInvalidRequest
	}
	return input, nil
}

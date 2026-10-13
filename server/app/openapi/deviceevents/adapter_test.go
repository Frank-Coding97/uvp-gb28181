package deviceevents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

func TestAdapterBuildsEventRequestFromAuthenticatedRequestAndHTTPInputs(t *testing.T) {
	upstream := &subscriptionStub{}
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, upstream)
	registrations := NewAdapterRegistrations(NewDispatcher(service))
	var create catalogruntime.Adapter
	for _, registration := range registrations {
		if registration.Key == SubscriptionCreateAdapterKey {
			create = registration.Adapter
		}
	}
	require.NotNil(t, create)
	body, err := json.Marshal(map[string]any{"kind": EventAlarm, "expiresSeconds": 600})
	require.NoError(t, err)
	value, err := create.Execute(context.Background(), catalogruntime.Invocation{
		Scope: SubscriptionCreateScope, Method: "POST",
		Params: map[string]string{"deviceId": deviceEventsRootDevice},
		Query:  map[string]string{}, Body: body,
		Value: adapters.Request{ClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice},
	})
	require.NoError(t, err)
	subscription, ok := value.(ConfiguredSubscription)
	require.True(t, ok)
	assert.Equal(t, deviceEventsRootDevice, subscription.DeviceID)
	assert.Equal(t, EventAlarm, subscription.Kind)
	assert.Equal(t, 600, subscription.ExpiresSeconds)
	assert.Equal(t, 1, upstream.configureCalls)
}

func TestSubscriptionAdapterBindsSubscriptionToPathDevice(t *testing.T) {
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, &subscriptionStub{})
	created, err := service.CreateSubscription(context.Background(), Request{
		OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice,
		Kind: EventAlarm, ExpiresSeconds: 600,
	})
	require.NoError(t, err)
	registrations := NewAdapterRegistrations(NewDispatcher(service))
	var read catalogruntime.Adapter
	for _, registration := range registrations {
		if registration.Key == SubscriptionReadAdapterKey {
			read = registration.Adapter
		}
	}
	require.NotNil(t, read)
	_, err = read.Execute(context.Background(), catalogruntime.Invocation{
		Scope: SubscriptionReadScope, Method: "GET",
		Params: map[string]string{"deviceId": deviceEventsOtherCode, "subscriptionId": created.ID},
		Query:  map[string]string{},
		Value:  adapters.Request{ClientID: 71, ResourceScope: scope, DeviceID: deviceEventsOtherCode},
	})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRenewSubscriptionAdapterAcceptsSignedEmptyJSONBody(t *testing.T) {
	service, _, scope, _, _, _ := newDeviceEventsTestService(t, nil)
	subscription, err := service.CreateSubscription(context.Background(), Request{
		OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice,
		Kind: EventDeviceStatus, ExpiresSeconds: 600,
	})
	require.NoError(t, err)
	var renew catalogruntime.Adapter
	for _, registration := range NewAdapterRegistrations(NewDispatcher(service)) {
		if registration.Key == SubscriptionRenewAdapterKey {
			renew = registration.Adapter
		}
	}
	require.NotNil(t, renew)
	invocation := catalogruntime.Invocation{
		Scope: SubscriptionRenewScope, Method: "POST",
		Params: map[string]string{"deviceId": deviceEventsRootDevice, "subscriptionId": subscription.ID},
		Query:  map[string]string{}, Body: []byte(`{}`),
		Value: adapters.Request{ClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice},
	}
	invocation.Body = []byte(`{"unexpected":true}`)
	_, err = renew.Execute(context.Background(), invocation)
	assert.ErrorIs(t, err, ErrInvalidRequest)
	invocation.Body = []byte(`{}`)
	value, err := renew.Execute(context.Background(), invocation)
	require.NoError(t, err)
	assert.Equal(t, "enabled", value.(ConfiguredSubscription).Status)
}

func TestEventAdapterRejectsMalformedBodyAndUnexpectedQuery(t *testing.T) {
	registrations := NewAdapterRegistrations(NewDispatcher(nil))
	var create catalogruntime.Adapter
	for _, registration := range registrations {
		if registration.Key == SubscriptionCreateAdapterKey {
			create = registration.Adapter
		}
	}
	require.NotNil(t, create)
	base := catalogruntime.Invocation{
		Scope: SubscriptionCreateScope, Method: "POST",
		Params: map[string]string{"deviceId": deviceEventsRootDevice},
		Value:  adapters.Request{ClientID: 71, ResourceScope: resource.DepartmentScope{OwnerDeptID: deviceEventsRootDept, DataScope: resource.DataScopeDepartment}, DeviceID: deviceEventsRootDevice},
	}
	base.Body = []byte(`{"kind":"alarm","unexpected":true}`)
	_, err := create.Execute(context.Background(), base)
	assert.ErrorIs(t, err, ErrInvalidRequest)
	base.Body = []byte(`{"kind":"alarm"}`)
	base.Query = map[string]string{"unexpected": "1"}
	_, err = create.Execute(context.Background(), base)
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestEventAdapterUsesPageSizeAndSignedCursorFromQuery(t *testing.T) {
	service, db, scope, rootID, _, _ := newDeviceEventsTestService(t, nil)
	subscription, err := service.CreateSubscription(context.Background(), Request{
		OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice,
		Kind: EventDeviceStatus, ExpiresSeconds: 600,
	})
	require.NoError(t, err)
	base := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&gbmodels.GbDeviceStatusEvent{
			DeviceID: rootID, DeviceCode: deviceEventsRootDevice, EventType: gbmodels.DeviceEventRegisterOnline,
			ToStatus: 1, Source: gbmodels.DeviceEventSourceRegister, OccurredAt: base.Add(time.Duration(i) * time.Second),
		}).Error)
	}
	var read catalogruntime.Adapter
	for _, registration := range NewAdapterRegistrations(NewDispatcher(service)) {
		if registration.Key == DeviceEventsReadAdapterKey {
			read = registration.Adapter
		}
	}
	require.NotNil(t, read)
	query := map[string]string{"pageSize": "1"}
	value := adapters.Request{ClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Query: query}
	firstValue, err := read.Execute(context.Background(), catalogruntime.Invocation{
		Scope: DeviceEventsReadScope, Method: "GET",
		Params: map[string]string{"deviceId": deviceEventsRootDevice, "subscriptionId": subscription.ID},
		Query:  query, Value: value,
	})
	require.NoError(t, err)
	first := firstValue.(EventPage)
	require.Len(t, first.Items, 1)
	assert.True(t, first.HasMore)

	query = map[string]string{"pageSize": "1", "cursor": first.NextCursor}
	value.Query, value.Cursor = query, first.NextCursor
	secondValue, err := read.Execute(context.Background(), catalogruntime.Invocation{
		Scope: DeviceEventsReadScope, Method: "GET",
		Params: map[string]string{"deviceId": deviceEventsRootDevice, "subscriptionId": subscription.ID},
		Query:  query, Value: value,
	})
	require.NoError(t, err)
	second := secondValue.(EventPage)
	require.Len(t, second.Items, 1)
	assert.NotEqual(t, first.Items[0].ID, second.Items[0].ID)
}

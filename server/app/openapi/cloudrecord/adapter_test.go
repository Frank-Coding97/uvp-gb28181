package cloudrecord

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

func TestAdapterValidatesTransportAndReturnsRealScopedEmptyIndex(t *testing.T) {
	service, _, scope, _ := fixture(t)
	for _, registration := range NewAdapterRegistrations(service) {
		request := adapters.Request{ClientID: 91, DeviceID: deviceCode, ChannelID: channelCode, ResourceScope: scope}
		params := map[string]string{"deviceId": deviceCode, "channelId": channelCode}
		query := map[string]string{}
		var expectedScope string
		switch registration.Key {
		case ListAdapterKey:
			expectedScope = ListScope
			query = map[string]string{"page": "2", "pageSize": "10"}
		case InfoAdapterKey:
			expectedScope = InfoScope
			params["fileId"] = "123"
		case DatesAdapterKey:
			expectedScope = DatesScope
			query = map[string]string{"startDate": "2026-11-01", "endDate": "2026-12-01"}
		}
		request.Query = query
		invocation := catalogruntime.Invocation{Scope: expectedScope, Method: "GET", Params: params, Query: query, Value: request}
		_, err := registration.Adapter.Execute(context.Background(), invocation)
		if registration.Key == InfoAdapterKey {
			require.ErrorIs(t, err, adapters.ErrNotFound)
		} else {
			require.NoError(t, err)
		}
		invocation.Method = "POST"
		_, err = registration.Adapter.Execute(context.Background(), invocation)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest)
		invocation.Method = "GET"
		invocation.Body = []byte(`{}`)
		_, err = registration.Adapter.Execute(context.Background(), invocation)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	}
	for _, query := range []map[string]string{{"page": "abc"}, {"pageSize": "0"}, {"startTime": "yesterday"}, {"secret": "x"}} {
		_, err := listOptions(query)
		require.ErrorIs(t, err, adapters.ErrInvalidRequest)
	}
}

func TestCloudAdapterRejectsMismatchedEmptyQueryKeys(t *testing.T) {
	service, _, scope, _ := fixture(t)
	invocation := catalogruntime.Invocation{Scope: ListScope, Method: "GET", Params: map[string]string{"deviceId": deviceCode, "channelId": channelCode}, Query: map[string]string{"metadataState": ""}, Value: adapters.Request{ClientID: 91, DeviceID: deviceCode, ChannelID: channelCode, ResourceScope: scope, Query: map[string]string{"different": ""}}}
	_, err := NewAdapterRegistrations(service)[0].Adapter.Execute(context.Background(), invocation)
	require.ErrorIs(t, err, adapters.ErrInvalidRequest)
}

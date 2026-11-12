package routes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

type liveReaderBindingSpy struct {
	ready bool
	reads []auth.PlayRequest
	lists []auth.PlayListRequest
}

func (s *liveReaderBindingSpy) Ready() bool { return s.ready }
func (s *liveReaderBindingSpy) Start(context.Context, auth.PlayRequest) (any, error) {
	panic("read binding must not start media")
}
func (s *liveReaderBindingSpy) Stop(context.Context, auth.PlayRequest) (any, error) {
	panic("read binding must not stop media")
}
func (s *liveReaderBindingSpy) Read(_ context.Context, r auth.PlayRequest) (any, error) {
	s.reads = append(s.reads, r)
	return map[string]string{"kind": "info"}, nil
}
func (s *liveReaderBindingSpy) List(_ context.Context, r auth.PlayListRequest) (any, error) {
	s.lists = append(s.lists, r)
	return map[string]string{"kind": "list"}, nil
}

func TestDelegatedLiveReadBindingsPreserveIdentityAndRejectWrongScopeOrValue(t *testing.T) {
	reader := &liveReaderBindingSpy{ready: true}
	ptz := &expandedPTZSpy{}
	bindings := delegatedRuntimeBindings(ptz, reader)
	request := auth.PlayRequest{ClientID: 41, OwnerDeptID: 10, DataScope: 3, DeviceID: expandedDevice, ChannelID: expandedChannel}
	list := auth.PlayListRequest{PlayRequest: request, Page: 2, PageSize: 7}
	for _, test := range []struct {
		key, scope string
		value      any
	}{{adapters.PlayLiveReadAdapterKey, auth.PlayLiveReadScope, request}, {adapters.PlayLiveListAdapterKey, auth.PlayLiveListScope, list}} {
		adapter := bindings[test.key]
		require.NotNil(t, adapter)
		_, err := adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: test.scope, Value: test.value})
		require.NoError(t, err)
		_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: auth.PlayLiveScope, Value: test.value})
		require.ErrorIs(t, err, auth.ErrPlayUnavailable)
		_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: test.scope, Value: adapters.Request{ClientID: 41}})
		require.ErrorIs(t, err, auth.ErrPlayUnavailable)
		reader.ready = false
		_, err = adapter.Execute(context.Background(), catalogruntime.Invocation{Scope: test.scope, Value: test.value})
		require.ErrorIs(t, err, auth.ErrPlayUnavailable)
		reader.ready = true
	}
	require.Equal(t, []auth.PlayRequest{request}, reader.reads)
	require.Equal(t, []auth.PlayListRequest{list}, reader.lists)
	require.Zero(t, ptz.calls)
	bindings = delegatedRuntimeBindings(ptz)
	require.NotContains(t, bindings, adapters.PlayLiveReadAdapterKey)
	require.NotContains(t, bindings, adapters.PlayLiveListAdapterKey)
}

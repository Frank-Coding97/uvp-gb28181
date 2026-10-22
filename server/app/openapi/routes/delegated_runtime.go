package routes

import (
	"context"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	catalogruntime "uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
)

type delegatedRuntimeAdapter func(context.Context, catalogruntime.Invocation) (any, error)

func (adapter delegatedRuntimeAdapter) Execute(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
	return adapter(ctx, invocation)
}

// Publication can use unbound declarations. The serving process binds only
// its already constructed domain dispatchers; catalog data supplies no code.
func delegatedRuntimeBindings(ptz auth.PTZDispatcher, playback ...auth.PlayDispatcher) map[string]catalogruntime.Adapter {
	bindings := make(map[string]catalogruntime.Adapter)
	if ptz != nil {
		for _, registration := range adapters.NewDelegatedPlaneRegistrations() {
			if registration.Key == adapters.PlayLiveAdapterKey {
				continue
			}
			bindings[registration.Key] = delegatedRuntimeAdapter(func(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
				request, ok := invocation.Value.(auth.PTZRequest)
				if !ok || request.Scope != invocation.Scope || !ptz.Ready() {
					return nil, auth.ErrPTZUnavailable
				}
				return ptz.Handle(ctx, request)
			})
		}
	}
	if len(playback) > 0 && playback[0] != nil {
		dispatcher := playback[0]
		bindings[adapters.PlayLiveAdapterKey] = delegatedRuntimeAdapter(func(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
			request, ok := invocation.Value.(auth.PlayRequest)
			if !ok || invocation.Scope != auth.PlayLiveScope || !dispatcher.Ready() {
				return nil, auth.ErrPlayUnavailable
			}
			return dispatcher.Start(ctx, request)
		})
		bindings[adapters.PlayLiveStopAdapterKey] = delegatedRuntimeAdapter(func(ctx context.Context, invocation catalogruntime.Invocation) (any, error) {
			request, ok := invocation.Value.(auth.PlayRequest)
			if !ok || invocation.Scope != auth.PlayLiveStopScope || !dispatcher.Ready() {
				return nil, auth.ErrPlayUnavailable
			}
			return dispatcher.Stop(ctx, request)
		})
	}
	return bindings
}

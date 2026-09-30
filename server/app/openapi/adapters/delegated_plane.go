package adapters

import (
	"context"
	"errors"
	"fmt"

	catalogruntime "uvplatform.cn/uvp-gb28181/app/openapi/catalog/runtime"
)

// The external OpenAPI surface is served by two dispatch planes.
//
// Plane A — the catalog dispatcher. A published operation names an adapter in
// this package, and auth/readMetadata hands the typed Request to
// CatalogRuntime.Dispatch. The device/channel read surface works this way.
//
// Plane B — a dedicated gateway path. The ptz:* scopes are handled by
// handlePTZ before the metadata dispatcher is ever reached because PTZ owns
// durable operation state. Its executable behaviour therefore lives in
// PTZDispatcher, which is wired into the gateway as a gateway option.
//
// Both planes gate on client.ScopePublished, which reads the immutable active
// release. The declarations below are what makes the PTZ operations publishable.
const (
	DelegatedPTZPlane  = "ptz"
	DelegatedPlayPlane = "play"
)

// Adapter keys for the plane B declarations. Like every other key in this
// package they are code-owned: a published operation may select one, it can
// never install one. They are per-scope rather than per-plane so a release
// item still says which operation it belongs to.
const (
	PTZPresetListAdapterKey    = "delegated.ptz.preset-list.v1"
	PTZPresetSaveAdapterKey    = "delegated.ptz.preset-save.v1"
	PTZPresetCallAdapterKey    = "delegated.ptz.preset-call.v1"
	PTZPresetDeleteAdapterKey  = "delegated.ptz.preset-delete.v1"
	PTZOperationReadAdapterKey = "delegated.ptz.operation-read.v1"
	PlayLiveAdapterKey         = "delegated.play.live.v1"
)

// DelegatedPlaneContractVersion is separate from ResourceAdapterContractVersion
// on purpose: it versions the declaration contract, not a resource DTO. The
// value is still "v1" because that is what catalog.store normalizes an unset
// column to, and both planes must agree on the stored form.
const DelegatedPlaneContractVersion = "v1"

// ErrDelegatedPlane reports that a plane B scope reached the generic catalog
// dispatcher. Nothing in the request path does that today — handleMedia and
// handlePTZ consume these scopes first — so this error means a routing
// regression, and it fails closed instead of pretending the operation ran.
var ErrDelegatedPlane = errors.New("openapi scope is dispatched by a dedicated gateway plane")

// delegatedPlaneAdapter is a registration-only declaration. It deliberately
// does not implement the device/channel Adapter interface: it has no
// Dispatch(Request), because it carries no resource behaviour.
type delegatedPlaneAdapter struct {
	key   string
	plane string
}

var _ catalogruntime.Adapter = (*delegatedPlaneAdapter)(nil)

func (adapter *delegatedPlaneAdapter) Key() string {
	if adapter == nil {
		return ""
	}
	return adapter.key
}

func (adapter *delegatedPlaneAdapter) Execute(_ context.Context, invocation catalogruntime.Invocation) (any, error) {
	if adapter == nil {
		return nil, ErrUnavailable
	}
	subject := adapter.plane
	if scope := invocation.Scope; scope != "" {
		subject = adapter.plane + ":" + scope
	}
	return nil, fmt.Errorf("%w: %s", ErrDelegatedPlane, subject)
}

// NewDelegatedPlaneRegistrations returns the plane B declarations. They are
// no PTZ dispatcher is installed: the published scope set is a code-owned
// contract and must not depend on which plane happens to be wired in the
// process that performs the publication. A request for a scope whose plane is
// absent still fails closed — handleMedia/handlePTZ answer 503 before dispatch,
// which is the accurate answer ("the capability exists, its plane is not
// available") rather than a misleading 403 capability denial.
func NewDelegatedPlaneRegistrations() []catalogruntime.AdapterRegistration {
	adapters := []*delegatedPlaneAdapter{
		{key: PlayLiveAdapterKey, plane: DelegatedPlayPlane},
		{key: PTZPresetListAdapterKey, plane: DelegatedPTZPlane},
		{key: PTZPresetSaveAdapterKey, plane: DelegatedPTZPlane},
		{key: PTZPresetCallAdapterKey, plane: DelegatedPTZPlane},
		{key: PTZPresetDeleteAdapterKey, plane: DelegatedPTZPlane},
		{key: PTZOperationReadAdapterKey, plane: DelegatedPTZPlane},
	}
	registrations := make([]catalogruntime.AdapterRegistration, 0, len(adapters))
	for _, adapter := range adapters {
		registrations = append(registrations, catalogruntime.AdapterRegistration{
			Key:             adapter.Key(),
			ContractVersion: DelegatedPlaneContractVersion,
			Adapter:         adapter,
		})
	}
	return registrations
}

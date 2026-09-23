package media

import (
	"context"
	"strings"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
)

// NodeControlBinding is operator-controlled revocation trust, not a node DTO
// or playback qualification. BindingRevision changes with trust configuration;
// it is independent of meta_node.revision and normal node lifecycle changes.
type NodeControlBinding struct {
	NodeID          int64
	NodeUUID        string
	BindingRevision uint64
	Enabled         bool
	UseSystemRoots  bool
	TLS             zlm.OpenAPIControlTLS
	HookBase        string
}

// NodeControlConfigProvider returns immutable snapshots, including the roots.
// Root owns the source and its authorization; no legacy URL or IP inference.
type NodeControlConfigProvider interface {
	Lookup(context.Context, string) (NodeControlBinding, error)
}

type RevocationNodeRegistry interface {
	GetByUUID(string) (*node.Node, bool)
}

type TrustedRevocationFactory struct {
	registry RevocationNodeRegistry
	bindings NodeControlConfigProvider
	store    *config.NodeRuntimeStore
}

func NewTrustedRevocationFactory(registry RevocationNodeRegistry, bindings NodeControlConfigProvider, store *config.NodeRuntimeStore) *TrustedRevocationFactory {
	return &TrustedRevocationFactory{registry: registry, bindings: bindings, store: store}
}

func (f *TrustedRevocationFactory) Resolve(ctx context.Context, uuid string) (RevocationRuntime, error) {
	unavailable := config.ErrNodeRuntimeUnavailable
	if f == nil || f.registry == nil || f.bindings == nil || f.store == nil || ctx == nil || ctx.Err() != nil || uuid == "" {
		return RevocationRuntime{}, unavailable
	}
	binding, err := f.bindings.Lookup(ctx, uuid)
	if err != nil || !validRevocationBinding(binding, uuid) {
		return RevocationRuntime{}, unavailable
	}
	if binding.TLS.Roots != nil {
		binding.TLS.Roots = binding.TLS.Roots.Clone()
	}
	n, found := f.registry.GetByUUID(uuid)
	if !found || !validRevocationNode(n, binding) {
		return RevocationRuntime{}, unavailable
	}
	start := *n
	control, err := zlm.NewOpenAPIRuntimeControl(start, binding.TLS)
	if err != nil {
		return RevocationRuntime{}, unavailable
	}
	retained := false
	defer func() {
		if !retained {
			control.Close()
		}
	}()
	ref := config.NodeRuntimeRef{NodeID: start.ID, NodeUUID: uuid, NodeRevision: start.Revision}
	var readback zlm.RuntimeConfiguration
	_, err = config.NewNodeProbeCoordinator(f.store).Probe(ctx, ref, func(ctx context.Context) (config.NodeRuntimeObservation, error) {
		var err error
		readback, err = control.ProbeConfiguration(ctx, binding.HookBase)
		if err != nil || !f.unchanged(ctx, start, binding) {
			return config.NodeRuntimeObservation{}, unavailable
		}
		return config.NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: readback.BootNonce, ProtocolVersion: int64(readback.ProtocolVersion)}, nil
	})
	if err != nil || !f.unchanged(ctx, start, binding) {
		return RevocationRuntime{}, unavailable
	}
	snapshot, err := f.store.Load(ctx, ref)
	if err != nil || ctx.Err() != nil || snapshot.IdentityStatus != config.NodeRuntimeStatusActive || snapshot.RuntimeProtocolVersion != config.NodeRuntimeProtocolV1 || snapshot.RuntimeConfirmedRevision != ref.NodeRevision || snapshot.CurrentBootNonce != readback.BootNonce {
		return RevocationRuntime{}, unavailable
	}
	// The exact control that passed preflight is transferred to the worker.
	// A changed boot never closes any old viewer: the worker compares its
	// captured boot and retains pending until actual disappearance is proven.
	retained = true
	var release sync.Once
	return RevocationRuntime{Control: control, CurrentBootNonce: readback.BootNonce, HookBudget: readback.HookBudget, Trusted: true,
		Release: func() { release.Do(control.Close) }}, nil
}

func validRevocationBinding(b NodeControlBinding, uuid string) bool {
	return b.Enabled && b.NodeID > 0 && b.NodeUUID == uuid && b.BindingRevision > 0 &&
		((b.UseSystemRoots && b.TLS.Roots == nil) || (!b.UseSystemRoots && b.TLS.Roots != nil))
}

func validRevocationNode(n *node.Node, b NodeControlBinding) bool {
	return n != nil && n.ID == b.NodeID && n.MediaServerUUID == b.NodeUUID && n.Revision > 0 && n.IsActive() && !n.RecoveryRequired && strings.TrimSpace(n.APISecret) != ""
}

func (f *TrustedRevocationFactory) unchanged(ctx context.Context, start node.Node, binding NodeControlBinding) bool {
	if ctx.Err() != nil {
		return false
	}
	n, found := f.registry.GetByUUID(start.MediaServerUUID)
	if !found || !validRevocationNode(n, binding) || n.Revision != start.Revision || n.APISecret != start.APISecret {
		return false
	}
	current, err := f.bindings.Lookup(ctx, start.MediaServerUUID)
	if err != nil || ctx.Err() != nil || !validRevocationBinding(current, start.MediaServerUUID) || current.NodeID != binding.NodeID || current.BindingRevision != binding.BindingRevision || current.UseSystemRoots != binding.UseSystemRoots || current.HookBase != binding.HookBase || current.TLS.Endpoint != binding.TLS.Endpoint || current.TLS.SPKISHA256 != binding.TLS.SPKISHA256 {
		return false
	}
	if binding.UseSystemRoots {
		return true
	}
	return current.TLS.Roots.Equal(binding.TLS.Roots)
}

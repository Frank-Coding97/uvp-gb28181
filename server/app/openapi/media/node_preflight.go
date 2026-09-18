// Package media assembles OpenAPI-only media controls. It does not register
// public endpoints or inherit ordinary backend node playback eligibility.
package media

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
)

type NodePreflight struct {
	coordinator *config.NodeProbeCoordinator
}

func NewNodePreflight(store *config.NodeRuntimeStore) *NodePreflight {
	return &NodePreflight{coordinator: config.NewNodeProbeCoordinator(store)}
}

// Probe binds an explicit operator-provided TLS endpoint and node snapshot to
// fresh readback and a durable revision-checked identity update. It performs no
// configuration mutation, latch activation, stream creation, or session kick.
// Callers must separately qualify deployment topology/protocols and actual
// Hook delivery; this result alone is never an external playback permit.
func (p *NodePreflight) Probe(ctx context.Context, n node.Node, tls zlm.OpenAPIControlTLS, hookBase string) (config.NodeRuntimeSnapshot, error) {
	if p == nil || p.coordinator == nil {
		return config.NodeRuntimeSnapshot{}, config.ErrNodeRuntimeUnavailable
	}
	ref := config.NodeRuntimeRef{NodeID: n.ID, NodeUUID: n.MediaServerUUID, NodeRevision: n.Revision}
	return p.coordinator.Probe(ctx, ref, func(ctx context.Context) (config.NodeRuntimeObservation, error) {
		control, err := zlm.NewOpenAPIRuntimeControl(n, tls)
		if err != nil {
			return config.NodeRuntimeObservation{}, config.ErrNodeRuntimeUnavailable
		}
		defer control.Close()
		identity, err := control.ProbeConfiguration(ctx, hookBase)
		if err != nil {
			return config.NodeRuntimeObservation{}, config.ErrNodeRuntimeUnavailable
		}
		return config.NodeRuntimeObservation{NodeRuntimeRef: ref, BootNonce: identity.BootNonce, ProtocolVersion: int64(identity.ProtocolVersion)}, nil
	})
}

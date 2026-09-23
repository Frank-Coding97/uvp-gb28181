package media

import "context"

// deploymentQualification is an operator-controlled declaration of completed
// deployment acceptance, not evidence obtained by reading node configuration.
// It never grants playback on its own; runtime probes and admission still apply.
type deploymentQualification struct {
	ID           string            `yaml:"id"`
	Topology     string            `yaml:"topology"`
	MediaOrigins map[string]string `yaml:"media_origins"`
}

// QualifiedBinding contains only values. Callers cannot mutate the startup
// allowlist, and absent protocols are never inferred from another protocol.
type QualifiedBinding struct {
	NodeID          int64
	NodeUUID        string
	BindingRevision uint64
	QualificationID string
	Protocol        string
	MediaOrigin     string
}

func validDeploymentQualification(q deploymentQualification) bool {
	if !validApplicationUUID(q.ID) || q.Topology != "direct-http1" || len(q.MediaOrigins) == 0 || len(q.MediaOrigins) > 2 {
		return false
	}
	for protocol, origin := range q.MediaOrigins {
		if !validApplicationProtocol(protocol) {
			return false
		}
		if _, ok := trustedOrigin(origin, protocol); !ok {
			return false
		}
	}
	return true
}

func (p *FileNodeControlBindings) Qualification(ctx context.Context, nodeUUID, protocol string) (QualifiedBinding, error) {
	binding, err := p.Lookup(ctx, nodeUUID)
	if err != nil || !binding.Enabled {
		return QualifiedBinding{}, ErrRevocationBindingsUnavailable
	}
	q, ok := p.qualifications[nodeUUID]
	if !ok {
		return QualifiedBinding{}, ErrRevocationBindingsUnavailable
	}
	origin, ok := q.MediaOrigins[protocol]
	if !ok {
		return QualifiedBinding{}, ErrRevocationBindingsUnavailable
	}
	return QualifiedBinding{NodeID: binding.NodeID, NodeUUID: binding.NodeUUID, BindingRevision: binding.BindingRevision,
		QualificationID: q.ID, Protocol: protocol, MediaOrigin: origin}, nil
}

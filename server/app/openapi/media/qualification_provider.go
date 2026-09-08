package media

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
)

const qualificationCapacity = 1024
const qualificationLifetime = 10 * time.Second

// NodeQualificationProvider binds deployment declarations to fresh control
// probes. Tickets are process-local, bounded and non-renewable, not API tokens.
type NodeQualificationProvider struct {
	now      func() time.Time
	registry RevocationNodeRegistry
	bindings *FileNodeControlBindings
	store    *config.NodeRuntimeStore
	factory  *TrustedRevocationFactory
	mu       sync.Mutex
	pending  int
	tickets  map[string]qualifiedTicketRecord
}

type qualifiedTicketRecord struct {
	target  QualificationRequest
	ticket  QualificationTicket
	binding QualifiedBinding
}

var _ QualificationProvider = (*NodeQualificationProvider)(nil)

func NewNodeQualificationProvider(registry RevocationNodeRegistry, bindings *FileNodeControlBindings, store *config.NodeRuntimeStore) *NodeQualificationProvider {
	return &NodeQualificationProvider{now: time.Now, registry: registry, bindings: bindings, store: store,
		factory: NewTrustedRevocationFactory(registry, bindings, store), tickets: make(map[string]qualifiedTicketRecord)}
}

func (p *NodeQualificationProvider) Prepare(ctx context.Context, target QualificationRequest) (QualificationTicket, error) {
	if !p.available(ctx) || !validGBIDForApplication(target.DeviceID) || !validGBIDForApplication(target.ChannelID) || !validApplicationProtocol(target.Protocol) {
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	// Reserve capacity before I/O; simultaneous probes cannot exceed the bound.
	p.mu.Lock()
	for id, record := range p.tickets {
		if !p.now().Before(record.ticket.ExpiresAt) {
			delete(p.tickets, id)
		}
	}
	if len(p.tickets)+p.pending >= qualificationCapacity {
		p.mu.Unlock()
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	p.pending++
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.pending--; p.mu.Unlock() }()
	ids := make([]string, 0, len(p.bindings.qualifications))
	for id := range p.bindings.qualifications {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		binding, err := p.bindings.Qualification(ctx, id, target.Protocol)
		if err != nil {
			continue
		}
		n, ok := p.registry.GetByUUID(id)
		if !ok || n == nil || !n.IsActive() || n.RecoveryRequired || n.ID != binding.NodeID || n.MediaServerUUID != id || n.Revision == 0 {
			continue
		}
		start := *n
		// Resolve reuses the existing serialized probe with CA/SPKI, Hook
		// configuration and durable boot checks. No kick/stream operation runs.
		runtime, err := p.factory.Resolve(ctx, id)
		if err != nil {
			return QualificationTicket{}, ErrLiveApplicationUnavailable
		}
		if runtime.Release != nil {
			runtime.Release()
		}
		if !runtime.Trusted {
			return QualificationTicket{}, ErrLiveApplicationUnavailable
		}
		randomID, err := uuid.NewRandom()
		if err != nil {
			return QualificationTicket{}, ErrLiveApplicationUnavailable
		}
		ticket := QualificationTicket{QualificationID: randomID.String(), NodeID: start.ID, NodeUUID: id, NodeRevision: start.Revision,
			BootNonce: runtime.CurrentBootNonce, Protocol: target.Protocol, MediaOrigin: binding.MediaOrigin, ExpiresAt: p.now().UTC().Add(qualificationLifetime)}
		record := qualifiedTicketRecord{target: target, ticket: ticket, binding: binding}
		if !p.currentLocal(ctx, record) || p.currentRuntime(ctx, ticket) != nil {
			return QualificationTicket{}, ErrLiveApplicationUnavailable
		}
		p.mu.Lock()
		_, duplicate := p.tickets[ticket.QualificationID]
		if !duplicate {
			p.tickets[ticket.QualificationID] = record
		}
		p.mu.Unlock()
		if duplicate {
			return QualificationTicket{}, ErrLiveApplicationUnavailable
		}
		return ticket, nil
	}
	return QualificationTicket{}, ErrLiveApplicationUnavailable
}

func (p *NodeQualificationProvider) Validate(ctx context.Context, target QualificationRequest, ticket QualificationTicket) error {
	if !p.available(ctx) {
		return ErrLiveApplicationUnavailable
	}
	record, ok := p.record(ticket.QualificationID)
	if !ok || record.target != target || record.ticket != ticket || !p.currentLocal(ctx, record) {
		return ErrLiveApplicationUnavailable
	}
	return p.currentRuntime(ctx, ticket)
}

// The coordinator holds its mutex here: strictly memory-only checks. The
// application rechecks durable boot before/after EnsureLive, and grant/viewer
// authorization independently checks it in its own database transaction.
func (p *NodeQualificationProvider) PlayValidator() play.QualifiedNodeValidator {
	return play.QualifiedNodeValidatorFunc(func(ctx context.Context, request play.Request, snapshot play.NodeQualificationSnapshot) error {
		if !p.available(ctx) {
			return ErrLiveApplicationUnavailable
		}
		record, ok := p.record(request.QualificationID)
		if !ok || record.target != (QualificationRequest{DeviceID: request.DeviceID, ChannelID: request.ChannelID, Protocol: request.RequiredProtocol}) ||
			request.RequiredNode != record.ticket.NodeID || snapshot.ID != record.ticket.NodeID || snapshot.MediaServerUUID != record.ticket.NodeUUID ||
			snapshot.Revision != record.ticket.NodeRevision || snapshot.State != "active" || !p.currentLocal(ctx, record) {
			return ErrLiveApplicationUnavailable
		}
		return nil
	})
}

func (p *NodeQualificationProvider) available(ctx context.Context) bool {
	return p != nil && ctx != nil && ctx.Err() == nil && p.now != nil && p.registry != nil && p.bindings != nil && p.store != nil && p.factory != nil
}

func (p *NodeQualificationProvider) record(id string) (qualifiedTicketRecord, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	record, ok := p.tickets[id]
	return record, ok
}

func (p *NodeQualificationProvider) currentLocal(ctx context.Context, record qualifiedTicketRecord) bool {
	if !validQualificationTicket(record.ticket, record.target.Protocol, p.now()) || ctx.Err() != nil {
		return false
	}
	binding, err := p.bindings.Qualification(ctx, record.ticket.NodeUUID, record.target.Protocol)
	if err != nil || binding != record.binding {
		return false
	}
	n, ok := p.registry.GetByUUID(record.ticket.NodeUUID)
	return ok && n != nil && n.IsActive() && !n.RecoveryRequired && n.ID == record.ticket.NodeID && n.MediaServerUUID == record.ticket.NodeUUID && n.Revision == record.ticket.NodeRevision
}

func (p *NodeQualificationProvider) currentRuntime(ctx context.Context, ticket QualificationTicket) error {
	snapshot, err := p.store.Load(ctx, config.NodeRuntimeRef{NodeID: ticket.NodeID, NodeUUID: ticket.NodeUUID, NodeRevision: ticket.NodeRevision})
	if err != nil || ctx.Err() != nil || snapshot.IdentityStatus != config.NodeRuntimeStatusActive || snapshot.CurrentBootNonce != ticket.BootNonce ||
		snapshot.RuntimeProtocolVersion != config.NodeRuntimeProtocolV1 || snapshot.RuntimeConfirmedRevision != ticket.NodeRevision {
		return ErrLiveApplicationUnavailable
	}
	return nil
}

package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// NodeImpactAction is the small set of high-risk node actions that must be
// confirmed against a fresh impact snapshot. The action is part of the
// fingerprint, so a confirmation for one operation cannot be replayed for
// another operation.
type NodeImpactAction string

const (
	NodeImpactActionDelete      NodeImpactAction = "delete"
	NodeImpactActionMaintenance NodeImpactAction = "maintenance"
	NodeImpactActionKick        NodeImpactAction = "kick"

	// MaxNodeImpactItems bounds data supplied by an optional runtime/business
	// reader before it crosses the service/controller boundary.
	MaxNodeImpactItems = 1000
)

var (
	// ErrNodeImpactProviderUnavailable means that a caller requested the
	// confirmation contract before T14 supplied its runtime/ownership reader.
	ErrNodeImpactProviderUnavailable = errors.New("node impact provider unavailable")
	// ErrNodeImpactConfirmationRequired is returned by legacy action methods
	// when an impact provider is installed. Callers must preflight and use the
	// corresponding *Confirmed method.
	ErrNodeImpactConfirmationRequired = errors.New("node impact confirmation required")
	// ErrNodeImpactChanged is stable and intentionally does not include the
	// old/new snapshots or any upstream detail.
	ErrNodeImpactChanged = errors.New("node impact changed")
	// ErrNodeImpactUnavailable hides provider/upstream details from HTTP error
	// responses and logs while preserving a classifiable cause.
	ErrNodeImpactUnavailable = errors.New("node impact unavailable")
	ErrNodeImpactInvalid     = errors.New("invalid node impact")
	ErrNodeImpactConflict    = errors.New("node has active impact")
)

// NodeImpact is a bounded, secret-free impact summary. The optional provider
// supplies the authoritative counts for streams, recordings and sessions;
// Registry Stats remain the legacy fallback when no provider is installed.
type NodeImpact struct {
	Streams    int  `json:"streams"`
	Recordings int  `json:"recordings"`
	Sessions   int  `json:"sessions"`
	Truncated  bool `json:"truncated"`
}

// NodeImpactPreflight is the value returned before a high-risk action. It is
// safe to expose to a UI: the fingerprint is a one-way digest and no node
// secret or upstream response is included.
type NodeImpactPreflight struct {
	NodeID      int64            `json:"nodeId"`
	Action      NodeImpactAction `json:"action"`
	Impact      NodeImpact       `json:"impact"`
	Fingerprint string           `json:"fingerprint"`
	ObservedAt  time.Time        `json:"observedAt"`
}

// NodeImpactProvider is intentionally narrower than the management package's
// ownership resolver. T14 can adapt its real stream/recording/session reader
// to this interface without introducing a service↔management import cycle.
type NodeImpactProvider interface {
	ReadNodeImpact(context.Context, *node.Node) (NodeImpact, error)
}

// NodeImpactReader is a naming alias for adapters that call the dependency a
// reader rather than a provider.
type NodeImpactReader = NodeImpactProvider

// NodeImpactProbe makes simple function-based injection convenient in tests
// and bootstrap adapters.
type NodeImpactProbe func(context.Context, *node.Node) (NodeImpact, error)

func (p NodeImpactProbe) ReadNodeImpact(ctx context.Context, n *node.Node) (NodeImpact, error) {
	if p == nil {
		return NodeImpact{}, ErrNodeImpactProviderUnavailable
	}
	return p(ctx, n)
}

// ParseNodeImpactAction validates controller-supplied action names without
// echoing arbitrary input in an error returned to the caller.
func ParseNodeImpactAction(value string) (NodeImpactAction, error) {
	switch NodeImpactAction(strings.TrimSpace(value)) {
	case NodeImpactActionDelete, NodeImpactActionMaintenance, NodeImpactActionKick:
		return NodeImpactAction(strings.TrimSpace(value)), nil
	default:
		return "", ErrNodeImpactInvalid
	}
}

func (s *NodeService) SetNodeImpactProvider(provider NodeImpactProvider) {
	s.impactMu.Lock()
	s.impactProvider = provider
	s.impactMu.Unlock()
}

// SetNodeImpactProbe is the function-form counterpart to
// SetNodeImpactProvider, useful for T14 assembly and narrow tests.
func (s *NodeService) SetNodeImpactProbe(probe NodeImpactProbe) {
	if probe == nil {
		s.SetNodeImpactProvider(nil)
		return
	}
	s.SetNodeImpactProvider(probe)
}

func (s *NodeService) nodeImpactProvider() NodeImpactProvider {
	s.impactMu.RLock()
	defer s.impactMu.RUnlock()
	return s.impactProvider
}

func (s *NodeService) HasNodeImpactProvider() bool { return s.nodeImpactProvider() != nil }

// PreflightNodeImpact reads the current node and the injected bounded impact
// source while holding the same per-node lock used by action execution.
func (s *NodeService) PreflightNodeImpact(ctx context.Context, id int64, action NodeImpactAction) (NodeImpactPreflight, error) {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
	return s.preflightNodeImpactLocked(ctx, id, action)
}

func (s *NodeService) PreflightDelete(ctx context.Context, id int64) (NodeImpactPreflight, error) {
	return s.PreflightNodeImpact(ctx, id, NodeImpactActionDelete)
}

func (s *NodeService) PreflightSetMaintenance(ctx context.Context, id int64) (NodeImpactPreflight, error) {
	return s.PreflightNodeImpact(ctx, id, NodeImpactActionMaintenance)
}

func (s *NodeService) PreflightKickSessions(ctx context.Context, id int64) (NodeImpactPreflight, error) {
	return s.PreflightNodeImpact(ctx, id, NodeImpactActionKick)
}

func (s *NodeService) preflightNodeImpactLocked(ctx context.Context, id int64, action NodeImpactAction) (NodeImpactPreflight, error) {
	if _, err := ParseNodeImpactAction(string(action)); err != nil {
		return NodeImpactPreflight{}, err
	}
	cur, ok := s.registry.Get(id)
	if !ok {
		return NodeImpactPreflight{}, ErrNodeNotFound
	}
	provider := s.nodeImpactProvider()
	if provider == nil {
		return NodeImpactPreflight{}, ErrNodeImpactProviderUnavailable
	}
	impact, err := provider.ReadNodeImpact(ctx, cloneNode(cur))
	if err != nil {
		return NodeImpactPreflight{}, ErrNodeImpactUnavailable
	}
	impact, err = normalizeNodeImpact(impact)
	if err != nil {
		return NodeImpactPreflight{}, err
	}
	return NodeImpactPreflight{
		NodeID:      id,
		Action:      action,
		Impact:      impact,
		Fingerprint: fingerprintNodeImpact(cur, action, impact),
		ObservedAt:  time.Now().UTC(),
	}, nil
}

func normalizeNodeImpact(impact NodeImpact) (NodeImpact, error) {
	if impact.Streams < 0 || impact.Recordings < 0 || impact.Sessions < 0 {
		return NodeImpact{}, ErrNodeImpactInvalid
	}
	if impact.Streams > MaxNodeImpactItems {
		impact.Streams = MaxNodeImpactItems
		impact.Truncated = true
	}
	if impact.Recordings > MaxNodeImpactItems {
		impact.Recordings = MaxNodeImpactItems
		impact.Truncated = true
	}
	if impact.Sessions > MaxNodeImpactItems {
		impact.Sessions = MaxNodeImpactItems
		impact.Truncated = true
	}
	return impact, nil
}

func impactHasResources(impact NodeImpact) bool {
	return impact.Streams > 0 || impact.Recordings > 0 || impact.Sessions > 0
}

// fingerprintNodeImpact includes all target fields that can make an action
// stale. APISecret participates only through its digest; the clear value is
// never returned or placed in an error.
func fingerprintNodeImpact(n *node.Node, action NodeImpactAction, impact NodeImpact) string {
	if n == nil {
		return ""
	}
	secretHash := sha256.Sum256([]byte(n.APISecret))
	values := []string{
		strconv.FormatInt(n.ID, 10), string(action), n.Name, n.Host,
		strconv.Itoa(n.APIPort), n.ReceiveHost, n.PlaybackHost,
		n.MediaServerUUID, strconv.Itoa(n.Weight), string(n.State),
		strconv.Itoa(n.RTPPortStart), strconv.Itoa(n.RTPPortEnd),
		n.UpdatedAt.UTC().Format(time.RFC3339Nano), hex.EncodeToString(secretHash[:]),
		strconv.Itoa(impact.Streams), strconv.Itoa(impact.Recordings),
		strconv.Itoa(impact.Sessions), strconv.FormatBool(impact.Truncated),
	}
	hash := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{'|'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func sameFingerprint(expected, actual string) bool {
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// ExecuteNodeAction re-reads the node and impact immediately before invoking
// a mutating action. A changed fingerprint returns ErrNodeImpactChanged before
// any registry or ZLM side effect.
func (s *NodeService) ExecuteNodeAction(ctx context.Context, id int64, action NodeImpactAction, fingerprint string) (NodeActionResult, error) {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
	if s.nodeImpactProvider() == nil {
		return NodeActionResult{}, ErrNodeImpactProviderUnavailable
	}
	if strings.TrimSpace(fingerprint) == "" {
		return NodeActionResult{}, ErrNodeImpactConfirmationRequired
	}
	preflight, err := s.preflightNodeImpactLocked(ctx, id, action)
	if err != nil {
		return NodeActionResult{}, err
	}
	if !sameFingerprint(strings.TrimSpace(fingerprint), preflight.Fingerprint) {
		return NodeActionResult{}, ErrNodeImpactChanged
	}
	cur, ok := s.registry.Get(id)
	if !ok {
		return NodeActionResult{}, ErrNodeNotFound
	}
	result := NodeActionResult{Action: action}
	switch action {
	case NodeImpactActionDelete:
		if cur.State != node.StateMaintenance {
			return result, ErrNodeNotInMaintenance
		}
		if impactHasResources(preflight.Impact) || cur.Stats.SessionCount > 0 || cur.Stats.MediaSourceCount > 0 {
			return result, ErrNodeImpactConflict
		}
		return result, s.registry.Delete(ctx, id)
	case NodeImpactActionMaintenance:
		cur.State = node.StateMaintenance
		return result, s.registry.Update(ctx, *cur)
	case NodeImpactActionKick:
		count, kickErr := s.probe.KickSessions(ctx, cur)
		result.Kicked = count
		return result, kickErr
	default:
		return result, ErrNodeImpactInvalid
	}
}

// NodeActionResult carries the only action-specific result currently needed
// by kick; delete and maintenance intentionally return no mutable snapshot.
type NodeActionResult struct {
	Action NodeImpactAction `json:"action"`
	Kicked int              `json:"kicked,omitempty"`
}

func (s *NodeService) DeleteConfirmed(ctx context.Context, id int64, fingerprint string) error {
	_, err := s.ExecuteNodeAction(ctx, id, NodeImpactActionDelete, fingerprint)
	return err
}

func (s *NodeService) SetMaintenanceConfirmed(ctx context.Context, id int64, fingerprint string) error {
	_, err := s.ExecuteNodeAction(ctx, id, NodeImpactActionMaintenance, fingerprint)
	return err
}

func (s *NodeService) KickAllSessionsConfirmed(ctx context.Context, id int64, fingerprint string) (int, error) {
	result, err := s.ExecuteNodeAction(ctx, id, NodeImpactActionKick, fingerprint)
	return result.Kicked, err
}

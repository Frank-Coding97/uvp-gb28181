// Package businessreadiness reports whether the standalone media path has
// converged and has subsequently proved its Hook identity with a real
// authenticated keepalive.
package businessreadiness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	CompleteInstallationPhase = "complete"
	RunningSIPState           = "running"
	HeartbeatMaxAge           = 90 * time.Second
)

// Reason is intentionally a closed set. It is safe to expose through a
// readiness endpoint without reflecting client errors, credentials, or Hook
// URLs.
type Reason string

const (
	ReasonInstallationPending Reason = "installation_pending"
	ReasonNodeMissing         Reason = "node_missing"
	ReasonNodeAmbiguous       Reason = "node_ambiguous"
	ReasonNodeInactive        Reason = "node_inactive"
	ReasonMediaUnreachable    Reason = "media_unreachable"
	ReasonIdentityMismatch    Reason = "identity_mismatch"
	ReasonConfigNotConverged  Reason = "config_not_converged"
	ReasonHookUnconfirmed     Reason = "hook_unconfirmed"
	ReasonSIPNotRunning       Reason = "sip_not_running"
	ReasonReady               Reason = "ready"
)

// Result is the complete public result. No raw external error, secret,
// capability, or callback URL is returned.
type Result struct {
	BusinessReady bool   `json:"business_ready"`
	Reason        Reason `json:"reason"`
}

// ConfigClient is the read-only portion of the ZLM client used by Probe.
// zlm.ServiceAdapter and zlm.Client adapters can satisfy it without giving
// this package a write path.
type ConfigClient interface {
	GetServerConfig(context.Context, *node.Node) (map[string]string, error)
}

// ProbeConfig supplies dependencies. Now and Client are injectable for
// deterministic tests; a nil Now uses time.Now.
type ProbeConfig struct {
	Client ConfigClient
	Now    func() time.Time
}

// Input is the complete runtime state needed for a standalone business
// readiness decision.
type Input struct {
	InstallationPhase string
	SIPState          string
	ZLM               gbconfig.ZLMConfig
	Media             gbconfig.MediaConfig
	Registry          *node.Registry
}

// Probe is safe for concurrent callers. Checks are serialized so a stale
// external read cannot clear a newer convergence generation.
type Probe struct {
	client ConfigClient
	now    func() time.Time

	checkMu sync.Mutex
	stateMu sync.Mutex
	state   convergenceState
}

type convergenceState struct {
	fingerprint string
	convergedAt time.Time
}

// NewProbe constructs a read-only business readiness probe.
func NewProbe(config ProbeConfig) *Probe {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Probe{client: config.Client, now: now}
}

// Check samples the registry and the current ZLM configuration. It never
// issues SetServerConfig. A matching configuration only starts a new
// generation; readiness still requires a later authenticated keepalive.
func (p *Probe) Check(ctx context.Context, input Input) Result {
	if p == nil {
		return Result{Reason: ReasonNodeMissing}
	}
	p.checkMu.Lock()
	defer p.checkMu.Unlock()

	if input.InstallationPhase != CompleteInstallationPhase {
		p.reset()
		return Result{Reason: ReasonInstallationPending}
	}
	if input.SIPState != RunningSIPState {
		p.reset()
		return Result{Reason: ReasonSIPNotRunning}
	}

	candidate, reason := selectCandidate(input)
	if reason != "" {
		p.reset()
		return Result{Reason: reason}
	}
	if !candidate.IsActive() || candidate.RecoveryRequired {
		p.reset()
		return Result{Reason: ReasonNodeInactive}
	}
	if !input.Registry.IsAutoOnDemandReady(candidate.ID) {
		p.reset()
		return Result{Reason: ReasonConfigNotConverged}
	}
	if p.client == nil {
		p.reset()
		return Result{Reason: ReasonMediaUnreachable}
	}

	expected, err := zlm.ExpectedConfigForNode(candidate, input.Media)
	if err != nil {
		p.reset()
		return Result{Reason: ReasonConfigNotConverged}
	}
	actual, err := p.client.GetServerConfig(ctx, candidate)
	if err != nil {
		p.reset()
		return Result{Reason: ReasonMediaUnreachable}
	}
	if !managedConfigMatches(actual, expected) {
		p.reset()
		return Result{Reason: ReasonConfigNotConverged}
	}

	fingerprint := configFingerprint(candidate, expected)
	convergedAt := p.observe(fingerprint, p.now())

	// Re-sample the candidate set and revision after the external read. This
	// prevents publishing a ready result for a snapshot changed while ZLM was
	// being queried.
	latest, latestReason := selectCandidate(input)
	if latestReason != "" {
		p.reset()
		return Result{Reason: latestReason}
	}
	if latest.ID != candidate.ID || latest.Revision != candidate.Revision {
		p.reset()
		return Result{Reason: classifyChangedCandidate(latest, input.Registry)}
	}
	if !sameIdentity(latest, candidate) {
		p.reset()
		return Result{Reason: ReasonIdentityMismatch}
	}
	if !latest.IsActive() || latest.RecoveryRequired {
		p.reset()
		return Result{Reason: ReasonNodeInactive}
	}
	if !input.Registry.IsAutoOnDemandReady(latest.ID) {
		p.reset()
		return Result{Reason: ReasonConfigNotConverged}
	}

	if !heartbeatConfirmed(p.now(), latest.Stats.LastHeartbeatAt, convergedAt) {
		return Result{Reason: ReasonHookUnconfirmed}
	}
	return Result{BusinessReady: true, Reason: ReasonReady}
}

func selectCandidate(input Input) (*node.Node, Reason) {
	if input.Registry == nil {
		return nil, ReasonNodeMissing
	}
	wantHost, ok := normalizeLiteralHost(input.ZLM.Host)
	if !ok || input.ZLM.HTTPPort <= 0 || input.ZLM.HTTPPort > 65535 {
		return nil, ReasonNodeMissing
	}

	var endpointMatches []*node.Node
	var identityMatches []*node.Node
	for _, candidate := range input.Registry.List() {
		if candidate == nil {
			continue
		}
		host, valid := normalizeLiteralHost(candidate.Host)
		if !valid || host != wantHost || candidate.APIPort != input.ZLM.HTTPPort {
			continue
		}
		endpointMatches = append(endpointMatches, candidate)
		if candidate.APISecret == input.ZLM.Secret {
			identityMatches = append(identityMatches, candidate)
		}
	}
	if len(endpointMatches) == 0 {
		return nil, ReasonNodeMissing
	}
	if len(identityMatches) == 0 {
		return nil, ReasonIdentityMismatch
	}
	if len(identityMatches) != 1 {
		return nil, ReasonNodeAmbiguous
	}
	return identityMatches[0], ""
}

func normalizeLiteralHost(raw string) (string, bool) {
	host := strings.TrimSpace(raw)
	if strings.EqualFold(host, "localhost") {
		// This is an explicit local alias, not a DNS lookup. The standalone
		// config binds the local ZLM to IPv4 loopback by default.
		return "127.0.0.1", true
	}
	host = strings.Trim(host, "[]")
	if strings.Contains(host, "%") {
		return "", false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", false
	}
	return addr.Unmap().String(), true
}

func managedConfigMatches(actual, expected map[string]string) bool {
	if actual == nil {
		return false
	}
	if actual["hook.enable"] != expected["hook.enable"] ||
		actual["general.mediaServerId"] != expected["general.mediaServerId"] {
		return false
	}
	for _, event := range playauth.ManagedHookEvents() {
		key := "hook." + string(event)
		if actual[key] != expected[key] {
			return false
		}
	}
	return true
}

func configFingerprint(n *node.Node, expected map[string]string) string {
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(strconv.FormatInt(n.ID, 10))
	builder.WriteByte(0)
	builder.WriteString(n.Host)
	builder.WriteByte(0)
	builder.WriteString(strconv.Itoa(n.APIPort))
	builder.WriteByte(0)
	builder.WriteString(n.APISecret)
	builder.WriteByte(0)
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(expected[key])
		builder.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func (p *Probe) observe(fingerprint string, now time.Time) time.Time {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.state.fingerprint != fingerprint {
		p.state = convergenceState{fingerprint: fingerprint, convergedAt: now}
	}
	return p.state.convergedAt
}

func (p *Probe) reset() {
	p.stateMu.Lock()
	p.state = convergenceState{}
	p.stateMu.Unlock()
}

func heartbeatConfirmed(now, heartbeatAt, convergedAt time.Time) bool {
	if heartbeatAt.IsZero() || convergedAt.IsZero() || heartbeatAt.Before(convergedAt) || heartbeatAt.After(now) {
		return false
	}
	return now.Sub(heartbeatAt) <= HeartbeatMaxAge
}

func sameIdentity(left, right *node.Node) bool {
	return left != nil && right != nil && left.ID == right.ID &&
		left.Host == right.Host && left.APIPort == right.APIPort &&
		left.APISecret == right.APISecret && left.MediaServerUUID == right.MediaServerUUID
}

func classifyChangedCandidate(candidate *node.Node, registry *node.Registry) Reason {
	if candidate == nil {
		return ReasonNodeMissing
	}
	if !candidate.IsActive() || candidate.RecoveryRequired {
		return ReasonNodeInactive
	}
	if registry != nil && !registry.IsAutoOnDemandReady(candidate.ID) {
		return ReasonConfigNotConverged
	}
	return ReasonConfigNotConverged
}

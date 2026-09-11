package firewall

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

var ErrAllowlisted = errors.New("source is allowlisted")

type Backend interface {
	Add(sourceIP string, expiresAt time.Time) error
	Remove(sourceIP string) error
	List() ([]string, error)
}

type MemoryBackend struct {
	mu    sync.Mutex
	rules map[string]time.Time
}

func NewMemoryBackend() *MemoryBackend { return &MemoryBackend{rules: make(map[string]time.Time)} }
func (b *MemoryBackend) Add(ip string, expiresAt time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rules[ip] = expiresAt
	return nil
}
func (b *MemoryBackend) Remove(ip string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.rules, ip)
	return nil
}
func (b *MemoryBackend) List() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.rules))
	for ip := range b.rules {
		out = append(out, ip)
	}
	return out, nil
}

type Agent struct {
	backend   Backend
	clock     security.Clock
	allowlist []net.IPNet
	mu        sync.Mutex
	rules     map[string]security.BanDecision
	lastError string
}

func New(backend Backend, clock security.Clock, allowlist []net.IPNet) *Agent {
	if backend == nil {
		backend = NewMemoryBackend()
	}
	if clock == nil {
		clock = security.RealClock()
	}
	return &Agent{backend: backend, clock: clock, allowlist: allowlist, rules: make(map[string]security.BanDecision)}
}

func (*Agent) Capability() security.AgentCapabilityState {
	return security.AgentCapabilitySupported
}

func (a *Agent) Ban(decision security.BanDecision) error {
	ip, err := security.ValidateSource(decision.SourceIP)
	if err != nil {
		return err
	}
	for _, network := range a.allowlist {
		if network.Contains(ip) {
			return ErrAllowlisted
		}
	}
	if !decision.Permanent && decision.TTL <= 0 {
		return errors.New("ttl must be positive")
	}
	decision.SourceIP = ip.String()
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = a.clock.Now()
	}
	expiresAt := decision.ExpiresAt()
	if err := a.backend.Add(decision.SourceIP, expiresAt); err != nil {
		a.setError(err)
		return err
	}
	a.mu.Lock()
	a.rules[decision.SourceIP] = decision
	a.lastError = ""
	a.mu.Unlock()
	return nil
}

func (a *Agent) Unban(sourceIP string) error {
	ip, err := security.ValidateSource(sourceIP)
	if err != nil {
		return err
	}
	if err := a.backend.Remove(ip.String()); err != nil {
		a.setError(err)
		return err
	}
	a.mu.Lock()
	delete(a.rules, ip.String())
	a.lastError = ""
	a.mu.Unlock()
	return nil
}

func (a *Agent) Status() security.AgentStatus {
	rules, err := a.backend.List()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.lastError = err.Error()
		return security.AgentStatus{Connected: false, AppliedRules: 0, Capability: security.AgentCapabilitySupported, LastError: a.lastError, CheckedAt: a.clock.Now()}
	}
	if a.lastError != "" {
		a.lastError = ""
	}
	return security.AgentStatus{Connected: true, AppliedRules: len(rules), Capability: security.AgentCapabilitySupported, LastError: "", CheckedAt: a.clock.Now()}
}

func (a *Agent) Reconcile(decisions []security.BanDecision) error {
	now := a.clock.Now()
	desired := make(map[string]security.BanDecision, len(decisions))
	for _, decision := range decisions {
		ip, err := security.ValidateSource(decision.SourceIP)
		if err != nil {
			return err
		}
		allowlisted := false
		for _, network := range a.allowlist {
			if network.Contains(ip) {
				allowlisted = true
				break
			}
		}
		if !allowlisted && decision.ActiveAt(now) {
			decision.SourceIP = ip.String()
			desired[decision.SourceIP] = decision
		}
	}
	existing, err := a.backend.List()
	if err != nil {
		return err
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, sourceIP := range existing {
		existingSet[sourceIP] = struct{}{}
		if _, ok := desired[sourceIP]; !ok {
			if err := a.Unban(sourceIP); err != nil {
				return err
			}
		}
	}
	for sourceIP, decision := range desired {
		if _, ok := existingSet[sourceIP]; ok {
			a.mu.Lock()
			applied, tracked := a.rules[sourceIP]
			a.mu.Unlock()
			if tracked && sameReconcileDecision(applied, decision) {
				continue
			}
		}
		if err := a.Ban(decision); err != nil {
			return err
		}
	}
	return nil
}

func sameReconcileDecision(applied, desired security.BanDecision) bool {
	return applied.DecisionID == desired.DecisionID && applied.ExpiresAt().Equal(desired.ExpiresAt())
}

func (a *Agent) setError(err error) { a.mu.Lock(); a.lastError = err.Error(); a.mu.Unlock() }

var _ security.FirewallAgentCapabilityProvider = (*Agent)(nil)

type request struct {
	Action    string                 `json:"action"`
	Decision  security.BanDecision   `json:"decision"`
	SourceIP  string                 `json:"sourceIp"`
	Decisions []security.BanDecision `json:"decisions,omitempty"`
}
type response struct {
	OK     bool                 `json:"ok"`
	Error  string               `json:"error,omitempty"`
	Status security.AgentStatus `json:"status"`
}

// ServeUnix serves a newline-delimited JSON protocol on a local Unix socket.
// The caller owns process privileges and socket permissions; this function
// never invokes shell commands or opens a network listener.
func (a *Agent) ServeUnix(socketPath string, stop <-chan struct{}) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0750); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0660); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return err
	}
	defer listener.Close()
	defer os.Remove(socketPath)
	for {
		if stop != nil {
			select {
			case <-stop:
				return nil
			default:
			}
		}
		listener.(*net.UnixListener).SetDeadline(time.Now().Add(250 * time.Millisecond))
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go a.handleConn(conn)
	}
}

func (a *Agent) handleConn(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	var req request
	if err := decoder.Decode(&req); err != nil {
		_ = encoder.Encode(response{Error: err.Error()})
		return
	}
	var err error
	switch req.Action {
	case "ban":
		err = a.Ban(req.Decision)
	case "unban":
		err = a.Unban(req.SourceIP)
	case "status":
	case "reconcile":
		err = a.Reconcile(req.Decisions)
	default:
		err = errors.New("unsupported action")
	}
	resp := response{OK: err == nil, Status: a.Status()}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = encoder.Encode(resp)
}

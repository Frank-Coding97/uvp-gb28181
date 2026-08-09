package security

import (
	"bytes"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo/sip"
)

// Admission is the transport-side gate. It deliberately knows nothing about
// SIP parsing or the firewall implementation; it only decides whether bytes
// may continue to Trace/sipgo and emits bounded security events.
type Admission struct {
	policy Policy
	clock  Clock
	trace  sip.TransportReadFilter

	mu      sync.Mutex
	banned  map[string]time.Time
	windows map[string]windowCounter
	conns   map[string]map[string]struct{}
	partial map[string][]byte
	onEvent func(Event)
	rules   *AccessRuleMatcher

	dropped atomic.Int64
	sampled atomic.Int64
}

type windowCounter struct {
	started time.Time
	count   int
}

func NewAdmission(policy Policy, clock Clock, trace sip.TransportReadFilter, onEvent func(Event)) *Admission {
	if clock == nil {
		clock = RealClock()
	}
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Admission{
		policy:  policy,
		clock:   clock,
		trace:   trace,
		banned:  make(map[string]time.Time),
		windows: make(map[string]windowCounter),
		conns:   make(map[string]map[string]struct{}),
		partial: make(map[string][]byte),
		onEvent: onEvent,
	}
}

func (a *Admission) SetTraceFilter(trace sip.TransportReadFilter) { a.trace = trace }
func (a *Admission) SetAccessRules(rules []AccessRule) {
	a.mu.Lock()
	a.rules = NewAccessRuleMatcher(rules)
	a.mu.Unlock()
}
func (a *Admission) SetPolicy(policy Policy) {
	if policy.Validate() == nil {
		a.mu.Lock()
		a.policy = policy
		a.mu.Unlock()
	}
}

func (a *Admission) Ban(sourceIP string, expiresAt time.Time) error {
	ip, err := ValidateSource(sourceIP)
	if err != nil || a.currentPolicy().IsAllowlisted(ip.String()) {
		return ErrInvalidAddress
	}
	a.mu.Lock()
	a.banned[ip.String()] = expiresAt
	a.mu.Unlock()
	return nil
}

func (a *Admission) Unban(sourceIP string) {
	if ip, err := ValidateSource(sourceIP); err == nil {
		a.mu.Lock()
		delete(a.banned, ip.String())
		a.mu.Unlock()
	}
}

func (a *Admission) IsBanned(sourceIP string) bool {
	ip, err := ValidateSource(sourceIP)
	if err != nil {
		return false
	}
	now := a.clock.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.banned[ip.String()]
	if !ok {
		return false
	}
	if !expires.IsZero() && !now.Before(expires) {
		delete(a.banned, ip.String())
		return false
	}
	return true
}

func (a *Admission) Dropped() int64 { return a.dropped.Load() }
func (a *Admission) Sampled() int64 { return a.sampled.Load() }

// Filter is suitable for sip.TransportReadFilter. A zero-length result is a
// drop signal understood by sipgo and prevents both parser and Trace work.
func (a *Admission) Filter(info sip.TransportReadProps, data []byte) ([]byte, error) {
	policy := a.currentPolicy()
	source := sourceIP(info.RemoteAddr)
	if source == "" {
		return a.reject(info, ReasonUnknownMethod, "", "", "")
	}
	now := a.clock.Now()
	userAgent := headerValue(data, "user-agent")
	a.mu.Lock()
	rules := a.rules
	a.mu.Unlock()
	method, complete := a.method(info, source, data)
	allowlisted := policy.IsAllowlisted(source) || rules.IsTrustedSource(source, userAgent, now)
	if _, blocked := rules.BlockedBy(source, userAgent, now); blocked && !allowlisted {
		return a.reject(info, ReasonManualBlacklist, source, method, userAgent)
	}
	if a.IsBanned(source) && policy.Mode != ModeObserve && !allowlisted {
		return a.reject(info, ReasonActiveBan, source, method, userAgent)
	}
	if len(data) > policy.MaxPacketBytes && !allowlisted {
		if policy.Mode != ModeObserve {
			return a.reject(info, ReasonPacketTooLarge, source, "", userAgent)
		}
		a.sample(info, ReasonPacketTooLarge, source, "")
	}

	if complete && strings.EqualFold(method, "INVITE") && !allowlisted {
		if policy.Mode == ModeObserve {
			a.sample(info, ReasonInviteRate, source, method)
		} else {
			return a.reject(info, ReasonInviteRate, source, method, userAgent)
		}
	}
	if complete && shouldRateLimit(method) {
		key := source + ":" + strings.ToUpper(info.Transport) + ":" + method
		reason := ReasonUnknownMethod
		if a.overLimit(key, now, policy.MaxUDPPerWindow) && !allowlisted {
			if policy.Mode != ModeObserve {
				return a.reject(info, reason, source, method, userAgent)
			}
			a.sample(info, reason, source, method)
		}
	}
	if strings.EqualFold(info.Transport, "tcp") || strings.EqualFold(info.Transport, "tls") {
		endpoint := remoteEndpoint(info.RemoteAddr)
		a.mu.Lock()
		connections := a.conns[source]
		if connections == nil {
			connections = make(map[string]struct{})
			a.conns[source] = connections
		}
		_, known := connections[endpoint]
		if !known {
			connections[endpoint] = struct{}{}
		}
		count := len(connections)
		a.mu.Unlock()
		if count > policy.MaxTCPConnections && policy.Mode == ModeStrict && !allowlisted {
			a.mu.Lock()
			if !known {
				delete(connections, endpoint)
			}
			a.mu.Unlock()
			return a.reject(info, ReasonConnectionRate, source, method, userAgent)
		}
	}

	if a.trace == nil {
		return append([]byte(nil), data...), nil
	}
	return a.trace(info, data)
}

func (a *Admission) method(info sip.TransportReadProps, source string, data []byte) (string, bool) {
	if strings.EqualFold(info.Transport, "tcp") || strings.EqualFold(info.Transport, "tls") {
		key := remoteEndpoint(info.RemoteAddr)
		if key == "" {
			key = source
		}
		policy := a.currentPolicy()
		a.mu.Lock()
		buffer := append(a.partial[key], data...)
		if len(buffer) > policy.MaxPacketBytes {
			delete(a.partial, key)
			a.mu.Unlock()
			return "", true
		}
		lineReady := bytes.IndexByte(buffer, '\n') >= 0
		if lineReady {
			delete(a.partial, key)
		} else {
			a.partial[key] = buffer
		}
		a.mu.Unlock()
		if !lineReady {
			return "", false
		}
		return firstMethod(buffer)
	}
	return firstMethod(data)
}

func (a *Admission) ConnectionClosed(info sip.TransportReadProps) {
	source := sourceIP(info.RemoteAddr)
	if source == "" {
		return
	}
	endpoint := remoteEndpoint(info.RemoteAddr)
	a.mu.Lock()
	delete(a.partial, endpoint)
	if connections := a.conns[source]; connections != nil {
		delete(connections, endpoint)
		if len(connections) == 0 {
			delete(a.conns, source)
		}
	}
	a.mu.Unlock()
}

func (a *Admission) overLimit(key string, now time.Time, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.windows[key]
	if c.started.IsZero() || now.Sub(c.started) >= a.policy.Window {
		c = windowCounter{started: now}
	}
	c.count++
	a.windows[key] = c
	return limit > 0 && c.count > limit
}

func (a *Admission) reject(info sip.TransportReadProps, reason Reason, source, method, userAgent string) ([]byte, error) {
	a.dropped.Add(1)
	if a.onEvent != nil {
		a.onEvent(Event{SourceIP: source, Transport: strings.ToUpper(info.Transport), Method: method, UserAgent: userAgent, Reason: reason, Action: ActionDrop, Occurred: a.clock.Now()})
	}
	return nil, nil
}

func headerValue(data []byte, name string) string {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.ToLower(strings.TrimSpace(key)) == wanted {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *Admission) sample(info sip.TransportReadProps, reason Reason, source, method string) {
	a.sampled.Add(1)
	if a.onEvent != nil {
		a.onEvent(Event{SourceIP: source, Transport: strings.ToUpper(info.Transport), Method: method, Reason: reason, Action: ActionSample, Occurred: a.clock.Now()})
	}
}

func (a *Admission) currentPolicy() Policy {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.policy
}

func shouldRateLimit(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "INVITE":
		return false
	case "REGISTER", "MESSAGE", "NOTIFY", "SUBSCRIBE", "ACK", "BYE", "CANCEL", "OPTIONS", "INFO", "PRACK", "UPDATE", "REFER", "PUBLISH":
		return false
	case "":
		return false
	default:
		return true
	}
}

func sourceIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	text := strings.TrimSpace(addr.String())
	if host, _, err := net.SplitHostPort(text); err == nil {
		text = host
	}
	ip, err := ValidateSource(text)
	if err != nil {
		return ""
	}
	return ip.String()
}

func remoteEndpoint(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return strings.TrimSpace(addr.String())
}

func firstMethod(data []byte) (string, bool) {
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 {
		return "", false
	}
	line := strings.TrimSpace(string(data[:lineEnd]))
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", true
	}
	return strings.ToUpper(fields[0]), true
}

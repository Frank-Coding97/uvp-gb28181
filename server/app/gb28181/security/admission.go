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
	conns   map[string]int
	partial map[string][]byte
	onEvent func(Event)

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
		conns:   make(map[string]int),
		partial: make(map[string][]byte),
		onEvent: onEvent,
	}
}

func (a *Admission) SetTraceFilter(trace sip.TransportReadFilter) { a.trace = trace }
func (a *Admission) SetPolicy(policy Policy) {
	if policy.Validate() == nil {
		a.mu.Lock()
		a.policy = policy
		a.mu.Unlock()
	}
}

func (a *Admission) Ban(sourceIP string, expiresAt time.Time) error {
	ip, err := ValidateSource(sourceIP)
	if err != nil || a.policy.IsAllowlisted(ip.String()) {
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
	source := sourceIP(info.RemoteAddr)
	if source == "" {
		return a.reject(info, ReasonUnknownMethod, "")
	}
	allowlisted := a.policy.IsAllowlisted(source)
	if a.IsBanned(source) && a.policy.Mode != ModeObserve && !allowlisted {
		return a.reject(info, ReasonConnectionRate, source)
	}
	if len(data) > a.policy.MaxPacketBytes && a.policy.Mode != ModeObserve && !allowlisted {
		return a.reject(info, ReasonPacketTooLarge, source)
	}

	now := a.clock.Now()
	method, complete := a.method(info, source, data)
	if complete {
		key := source + ":" + strings.ToUpper(info.Transport) + ":" + method
		if a.overLimit(key, now, a.policy.MaxUDPPerWindow) && a.policy.Mode != ModeObserve && !allowlisted {
			return a.reject(info, ReasonUnknownMethod, source)
		}
	}
	if strings.EqualFold(info.Transport, "tcp") || strings.EqualFold(info.Transport, "tls") {
		a.mu.Lock()
		a.conns[source]++
		connections := a.conns[source]
		a.mu.Unlock()
		if connections > a.policy.MaxTCPConnections && a.policy.Mode == ModeStrict {
			return a.reject(info, ReasonConnectionRate, source)
		}
	}

	if a.trace == nil {
		return append([]byte(nil), data...), nil
	}
	return a.trace(info, data)
}

func (a *Admission) method(info sip.TransportReadProps, source string, data []byte) (string, bool) {
	if strings.EqualFold(info.Transport, "tcp") || strings.EqualFold(info.Transport, "tls") {
		a.mu.Lock()
		buffer := append(a.partial[source], data...)
		if len(buffer) > a.policy.MaxPacketBytes {
			delete(a.partial, source)
			a.mu.Unlock()
			return "", true
		}
		lineReady := bytes.IndexByte(buffer, '\n') >= 0
		if lineReady {
			delete(a.partial, source)
		} else {
			a.partial[source] = buffer
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
	a.mu.Lock()
	delete(a.partial, source)
	if n := a.conns[source]; n <= 1 {
		delete(a.conns, source)
	} else {
		a.conns[source] = n - 1
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

func (a *Admission) reject(info sip.TransportReadProps, reason Reason, source string) ([]byte, error) {
	a.dropped.Add(1)
	if a.onEvent != nil {
		a.onEvent(Event{SourceIP: source, Transport: strings.ToUpper(info.Transport), Reason: reason, Action: ActionDrop, Occurred: a.clock.Now()})
	}
	return nil, nil
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

package security

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/emiago/sipgo/sip"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

// Admission is the transport-side gate. It uses the shared bounded trace
// framer for stream transports, decides whether complete messages may reach
// sipgo, and emits bounded security events.
type Admission struct {
	policy Policy
	clock  Clock
	trace  sip.TransportReadFilter

	mu        sync.Mutex
	banned    map[string]time.Time
	windows   map[string]windowCounter
	conns     map[string]map[string]struct{}
	connProps map[string]sip.TransportReadProps
	onEvent   func(Event)
	rules     *AccessRuleMatcher
	trusted   func(sourceIP, transport, deviceID string) bool
	tcpConns  int

	framerMu sync.Mutex
	framer   *gbtrace.FrameAssembler

	dropped atomic.Int64
	sampled atomic.Int64
}

type windowCounter struct {
	started time.Time
	count   int
}

type messageIdentity struct {
	method        string
	complete      bool
	response      bool
	deviceID      string
	transactionID string
}

func NewAdmission(policy Policy, clock Clock, trace sip.TransportReadFilter, onEvent func(Event)) *Admission {
	if clock == nil {
		clock = RealClock()
	}
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Admission{
		policy:    policy,
		clock:     clock,
		trace:     trace,
		banned:    make(map[string]time.Time),
		windows:   make(map[string]windowCounter),
		conns:     make(map[string]map[string]struct{}),
		connProps: make(map[string]sip.TransportReadProps),
		framer:    gbtrace.NewFrameAssembler(policy.MaxPacketBytes),
		onEvent:   onEvent,
	}
}

func (a *Admission) SetTraceFilter(trace sip.TransportReadFilter) {
	a.mu.Lock()
	a.trace = trace
	a.mu.Unlock()
}

func (a *Admission) SetTrustedSource(trusted func(sourceIP, transport, deviceID string) bool) {
	a.mu.Lock()
	a.trusted = trusted
	a.mu.Unlock()
}

func (a *Admission) SetAccessRules(rules []AccessRule) {
	a.mu.Lock()
	a.rules = NewAccessRuleMatcher(rules)
	a.mu.Unlock()
}
func (a *Admission) SetPolicy(policy Policy) {
	if policy.Validate() == nil {
		a.mu.Lock()
		previousMaxPacketBytes := a.policy.MaxPacketBytes
		a.policy = policy
		a.mu.Unlock()

		if previousMaxPacketBytes != policy.MaxPacketBytes {
			// FrameAssembler keeps the configured per-frame limit internally.
			a.framerMu.Lock()
			a.framer = gbtrace.NewFrameAssembler(policy.MaxPacketBytes)
			a.framerMu.Unlock()
		}
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
	ip, err := ValidateSource(sourceIP)
	if err != nil {
		return
	}
	source := ip.String()
	frameProps := make([]sip.TransportReadProps, 0)

	a.mu.Lock()
	delete(a.banned, source)
	for key := range a.windows {
		if strings.HasPrefix(key, source+"\x00") {
			delete(a.windows, key)
		}
	}
	if connections := a.conns[source]; connections != nil {
		for endpoint := range connections {
			stateKey := connectionStateKey(source, endpoint)
			if props, ok := a.connProps[stateKey]; ok {
				frameProps = append(frameProps, props)
			}
			delete(a.connProps, stateKey)
		}
		a.tcpConns -= len(connections)
		if a.tcpConns < 0 {
			a.tcpConns = 0
		}
		delete(a.conns, source)
	}
	a.mu.Unlock()

	a.forgetFrames(frameProps)
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

// Filter is suitable for sip.TransportReadFilter. TCP/TLS reads are framed
// first; an incomplete read returns nil and a sticky read is reduced to the
// concatenation of complete frames that pass admission.
func (a *Admission) Filter(info sip.TransportReadProps, data []byte) ([]byte, error) {
	policy := a.currentPolicy()
	source := sourceIP(info.RemoteAddr)
	now := a.clock.Now()
	if source == "" {
		return a.reject(info, ReasonUnknownMethod, "", messageIdentity{method: "UNKNOWN"}, "")
	}
	a.cleanupWindows(now, policy.Window)

	if isStreamTransport(info.Transport) {
		tracked, globalCapacity := a.trackTCPConnection(info, source, policy)
		if !tracked {
			if globalCapacity {
				a.dropped.Add(1)
				return nil, nil
			}
			identity := messageIdentity{method: "UNKNOWN"}
			if policy.Mode == ModeObserve {
				a.sample(info, ReasonConnectionRate, source, identity, "")
				return nil, nil
			}
			return a.reject(info, ReasonConnectionRate, source, identity, "")
		}
		if isTCPKeepalive(data) {
			return a.forward(info, data)
		}

		frames := a.pushFrames(info, data, policy.MaxPacketBytes)
		if len(frames) == 0 {
			return nil, nil
		}
		allowed := make([]byte, 0, len(data))
		for _, frame := range frames {
			if frame.Malformed {
				identity := messageIdentity{method: "UNKNOWN"}
				reason := ReasonUnknownMethod
				if frameTooLarge(frame) {
					reason = ReasonPacketTooLarge
				}
				if policy.Mode == ModeObserve {
					a.sample(info, reason, source, identity, "")
				} else {
					_, _ = a.reject(info, reason, source, identity, "")
				}
				continue
			}

			identity := parseMessageIdentity(frame.Data, len(frame.Data) <= policy.MaxPacketBytes)
			pass, err := a.admitMessage(info, policy, now, source, frame.Data, identity)
			if err != nil {
				return nil, err
			}
			if pass {
				allowed = append(allowed, frame.Data...)
			}
		}
		if len(allowed) == 0 {
			return nil, nil
		}
		return a.forward(info, allowed)
	}

	identity := parseMessageIdentity(data, len(data) <= policy.MaxPacketBytes)
	pass, err := a.admitMessage(info, policy, now, source, data, identity)
	if err != nil || !pass {
		return nil, err
	}
	return a.forward(info, data)
}

func (a *Admission) admitMessage(info sip.TransportReadProps, policy Policy, now time.Time, source string, data []byte, identity messageIdentity) (bool, error) {
	userAgent := headerValue(data, "user-agent")
	a.mu.Lock()
	rules := a.rules
	trusted := a.trusted
	a.mu.Unlock()
	allowlisted := policy.IsAllowlisted(source) || rules.IsTrustedSource(source, userAgent, now)

	if _, blocked := rules.BlockedBy(source, userAgent, now); blocked && !allowlisted {
		return a.dropMessage(info, ReasonManualBlacklist, source, identity, userAgent)
	}
	if a.IsBanned(source) && policy.Mode != ModeObserve && !allowlisted {
		return a.dropMessage(info, ReasonActiveBan, source, identity, userAgent)
	}
	if len(data) > policy.MaxPacketBytes && !allowlisted {
		identity = identityWithoutMetadata(identity)
		if policy.Mode != ModeObserve {
			return a.dropMessage(info, ReasonPacketTooLarge, source, identity, userAgent)
		}
		a.sample(info, ReasonPacketTooLarge, source, identity, userAgent)
		return true, nil
	}

	trustedInvite := false
	if identity.complete && identity.method == "INVITE" && identity.deviceID != "" && trusted != nil {
		trustedInvite = trusted(source, normalizedTransport(info.Transport), identity.deviceID)
	}
	if identity.complete && identity.method == "INVITE" && !allowlisted && !trustedInvite {
		if policy.Mode == ModeObserve {
			a.sample(info, ReasonInviteRate, source, identity, userAgent)
		} else {
			return a.dropMessage(info, ReasonInviteRate, source, identity, userAgent)
		}
	}

	if identity.complete && !identity.response && shouldRateLimit(identity.method) && !allowlisted {
		reason := ReasonUnknownMethod
		key := rateWindowKey(source, info.Transport, reason)
		if a.overLimit(key, now, policy.MaxUDPPerWindow, policy.Window, policy.MaxEventKeys) {
			if policy.Mode == ModeObserve {
				a.sample(info, reason, source, identity, userAgent)
			} else {
				return a.dropMessage(info, reason, source, identity, userAgent)
			}
		}
	}
	return true, nil
}

func (a *Admission) trackTCPConnection(info sip.TransportReadProps, source string, policy Policy) (bool, bool) {
	endpoint := remoteEndpoint(info.RemoteAddr)
	if endpoint == "" {
		endpoint = source
	}
	stateKey := connectionStateKey(source, endpoint)

	a.mu.Lock()
	defer a.mu.Unlock()
	connections := a.conns[source]
	if connections != nil {
		if _, known := connections[endpoint]; known {
			a.connProps[stateKey] = info
			return true, false
		}
	}
	if a.tcpConns >= policy.MaxEventKeys {
		return false, true
	}
	if policy.Mode == ModeStrict && connections != nil && len(connections) >= policy.MaxTCPConnections {
		return false, false
	}
	if connections == nil {
		connections = make(map[string]struct{})
		a.conns[source] = connections
	}
	connections[endpoint] = struct{}{}
	a.connProps[stateKey] = info
	a.tcpConns++
	return true, false
}

func (a *Admission) pushFrames(info sip.TransportReadProps, data []byte, maxSize int) []gbtrace.Frame {
	a.framerMu.Lock()
	defer a.framerMu.Unlock()
	if a.framer == nil {
		a.framer = gbtrace.NewFrameAssembler(maxSize)
	}
	a.framer.SweepIdle(time.Now())
	return a.framer.Push(info, data)
}

func (a *Admission) forward(info sip.TransportReadProps, data []byte) ([]byte, error) {
	a.mu.Lock()
	trace := a.trace
	a.mu.Unlock()
	if trace == nil {
		return append([]byte(nil), data...), nil
	}
	return trace(info, data)
}

func (a *Admission) ConnectionClosed(info sip.TransportReadProps) {
	if !isStreamTransport(info.Transport) {
		return
	}
	source := sourceIP(info.RemoteAddr)
	endpoint := remoteEndpoint(info.RemoteAddr)
	if endpoint == "" {
		endpoint = source
	}
	frameProps := []sip.TransportReadProps{info}

	a.mu.Lock()
	if source != "" {
		if connections := a.conns[source]; connections != nil {
			if _, known := connections[endpoint]; known {
				delete(connections, endpoint)
				a.tcpConns--
				if a.tcpConns < 0 {
					a.tcpConns = 0
				}
			}
			if len(connections) == 0 {
				delete(a.conns, source)
			}
		}
		stateKey := connectionStateKey(source, endpoint)
		if props, ok := a.connProps[stateKey]; ok {
			frameProps = append(frameProps, props)
		}
		delete(a.connProps, stateKey)
	}
	a.mu.Unlock()
	a.forgetFrames(frameProps)
}

func (a *Admission) forgetFrames(props []sip.TransportReadProps) {
	if len(props) == 0 {
		return
	}
	a.framerMu.Lock()
	defer a.framerMu.Unlock()
	if a.framer == nil {
		return
	}
	for _, item := range props {
		a.framer.Forget(item)
	}
}

func (a *Admission) cleanupWindows(now time.Time, window time.Duration) {
	a.mu.Lock()
	a.cleanupWindowsLocked(now, window)
	a.mu.Unlock()
}

func (a *Admission) cleanupWindowsLocked(now time.Time, window time.Duration) {
	if window <= 0 {
		return
	}
	for key, counter := range a.windows {
		if counter.started.IsZero() || !now.Before(counter.started.Add(window)) {
			delete(a.windows, key)
		}
	}
}

func (a *Admission) overLimit(key string, now time.Time, limit int, window time.Duration, maxKeys int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cleanupWindowsLocked(now, window)
	counter, exists := a.windows[key]
	if !exists && len(a.windows) >= maxKeys {
		return true
	}
	if !exists || counter.started.IsZero() || !now.Before(counter.started.Add(window)) {
		counter = windowCounter{started: now}
	}
	counter.count++
	a.windows[key] = counter
	return limit > 0 && counter.count > limit
}

func (a *Admission) reject(info sip.TransportReadProps, reason Reason, source string, identity messageIdentity, userAgent string) ([]byte, error) {
	a.dropped.Add(1)
	if a.onEvent != nil {
		a.onEvent(Event{
			TransactionID: identity.transactionID,
			SourceIP:      source,
			Transport:     normalizedTransport(info.Transport),
			Method:        eventMethod(identity),
			DeviceID:      identity.deviceID,
			RiskScope:     ScopeSource,
			UserAgent:     eventUserAgent(userAgent),
			Reason:        reason,
			Action:        ActionDrop,
			Occurred:      a.clock.Now(),
		})
	}
	return nil, nil
}

func (a *Admission) dropMessage(info sip.TransportReadProps, reason Reason, source string, identity messageIdentity, userAgent string) (bool, error) {
	_, err := a.reject(info, reason, source, identity, userAgent)
	return false, err
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

func headerValueAny(data []byte, names ...string) string {
	for _, name := range names {
		if value := headerValue(data, name); value != "" {
			return value
		}
	}
	return ""
}

func (a *Admission) sample(info sip.TransportReadProps, reason Reason, source string, identity messageIdentity, userAgent string) {
	a.sampled.Add(1)
	if a.onEvent != nil {
		a.onEvent(Event{
			TransactionID: identity.transactionID,
			SourceIP:      source,
			Transport:     normalizedTransport(info.Transport),
			Method:        eventMethod(identity),
			DeviceID:      identity.deviceID,
			RiskScope:     ScopeSource,
			UserAgent:     eventUserAgent(userAgent),
			Reason:        reason,
			Action:        ActionSample,
			Occurred:      a.clock.Now(),
		})
	}
}

func (a *Admission) currentPolicy() Policy {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.policy
}

func shouldRateLimit(method string) bool {
	if strings.TrimSpace(method) == "" {
		return false
	}
	return canonicalMethod(method) == "UNKNOWN"
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

func parseMessageIdentity(data []byte, includeMetadata bool) messageIdentity {
	method, complete := firstMethod(data)
	identity := messageIdentity{method: method, complete: complete}
	if !complete {
		return identity
	}
	if isSIPResponse(data) {
		identity.method = ""
		identity.response = true
		return identity
	}
	identity.method = canonicalMethod(method)
	if includeMetadata && identity.method == "INVITE" {
		identity.deviceID = fromDeviceID(data)
		identity.transactionID = inviteTransactionID(data)
	}
	return identity
}

func canonicalMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case "REGISTER", "MESSAGE", "NOTIFY", "SUBSCRIBE", "ACK", "BYE", "CANCEL", "OPTIONS", "INFO", "PRACK", "UPDATE", "REFER", "PUBLISH", "INVITE":
		return method
	default:
		return "UNKNOWN"
	}
}

func isSIPResponse(data []byte) bool {
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd < 0 {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(string(data[:lineEnd])))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "SIP/2.0") {
		return false
	}
	status, err := strconv.Atoi(fields[1])
	return err == nil && status >= 100 && status <= 699
}

func fromDeviceID(data []byte) string {
	return sipURIUser(headerValueAny(data, "from", "f"))
}

func sipURIUser(value string) string {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	start := strings.Index(lower, "sip:")
	schemeLength := len("sip:")
	if start < 0 {
		start = strings.Index(lower, "sips:")
		schemeLength = len("sips:")
	}
	if start < 0 {
		return ""
	}
	uri := trimmed[start+schemeLength:]
	end := len(uri)
	for index, char := range uri {
		if char == '>' || char == ';' || char == ',' || char == ' ' || char == '\t' {
			end = index
			break
		}
	}
	uri = uri[:end]
	at := strings.IndexByte(uri, '@')
	if at <= 0 {
		return ""
	}
	user := strings.TrimSpace(uri[:at])
	if user == "" || len(user) > 64 || strings.ContainsAny(user, "<>\"\r\n") {
		return ""
	}
	return user
}

func eventUserAgent(value string) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= 255 {
		return value
	}
	value = value[:255]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func inviteTransactionID(data []byte) string {
	branch := viaBranch(headerValueAny(data, "via", "v"))
	callID := strings.TrimSpace(headerValueAny(data, "call-id", "i"))
	from := strings.TrimSpace(headerValueAny(data, "from", "f"))
	cseq := strings.Fields(headerValue(data, "cseq"))
	if branch == "" || callID == "" || from == "" || len(cseq) != 2 || !strings.EqualFold(cseq[1], "INVITE") {
		return ""
	}
	if _, err := strconv.ParseUint(cseq[0], 10, 32); err != nil {
		return ""
	}
	material := strings.Join([]string{branch, callID, strings.Join(cseq, " "), from}, "\x00")
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}

func viaBranch(value string) string {
	first := strings.TrimSpace(strings.SplitN(value, ",", 2)[0])
	parts := strings.Split(first, ";")
	for _, part := range parts[1:] {
		key, raw, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "branch") {
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

func identityWithoutMetadata(identity messageIdentity) messageIdentity {
	identity.deviceID = ""
	identity.transactionID = ""
	return identity
}

func eventMethod(identity messageIdentity) string {
	if identity.response {
		return ""
	}
	if strings.TrimSpace(identity.method) == "" {
		return "UNKNOWN"
	}
	return identity.method
}

func normalizedTransport(transport string) string {
	return strings.ToUpper(strings.TrimSpace(transport))
}

func isStreamTransport(transport string) bool {
	return strings.EqualFold(transport, "tcp") || strings.EqualFold(transport, "tls")
}

func isTCPKeepalive(data []byte) bool {
	return len(data) > 0 && len(data) <= 4 && len(bytes.Trim(data, "\r\n")) == 0
}

func frameTooLarge(frame gbtrace.Frame) bool {
	errorText := strings.ToLower(frame.Error)
	return strings.Contains(errorText, "exceed") || strings.Contains(errorText, "limit")
}

func rateWindowKey(source, transport string, reason Reason) string {
	return source + "\x00" + normalizedTransport(transport) + "\x00" + string(reason)
}

func connectionStateKey(source, endpoint string) string {
	return source + "\x00" + endpoint
}

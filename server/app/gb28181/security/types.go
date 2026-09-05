package security

import (
	"errors"
	"net"
	"strings"
	"time"
)

type Mode string

const (
	ModeObserve Mode = "observe"
	ModeProtect Mode = "protect"
	ModeStrict  Mode = "strict"
)

type Action string

const (
	ActionAllow   Action = "allow"
	ActionDrop    Action = "drop"
	ActionSample  Action = "sample"
	ActionBan     Action = "ban"
	ActionUnban   Action = "unban"
	ActionExpired Action = "expired"
)

type RiskScope string

const (
	ScopeSource RiskScope = "source"
	ScopeDevice RiskScope = "device"
)

type Reason string

const (
	ReasonUnknownMethod    Reason = "unknown_method"
	ReasonInviteRate       Reason = "unknown_invite_rate"
	ReasonInvitePersistent Reason = "unauthorized_invite_accumulation"
	ReasonServerMismatch   Reason = "server_id_mismatch"
	ReasonDigestFailure    Reason = "digest_failure"
	ReasonNonceInvalid     Reason = "nonce_invalid"
	ReasonNonceExpired     Reason = "nonce_expired"
	ReasonNonceReplay      Reason = "nonce_replay"
	ReasonNonceStale       Reason = "nonce_stale"
	ReasonUnregisteredMsg  Reason = "unregistered_message"
	ReasonPacketTooLarge   Reason = "packet_too_large"
	ReasonConnectionRate   Reason = "connection_rate"
	ReasonManualBlacklist  Reason = "manual_blacklist"
	ReasonActiveBan        Reason = "active_ban"
)

var (
	ErrInvalidAddress = errors.New("invalid source address")
	ErrInvalidPolicy  = errors.New("invalid security policy")
)

type TTLStep struct {
	Score int           `json:"score"`
	TTL   time.Duration `json:"ttl"`
}

type Policy struct {
	Mode              Mode          `json:"mode"`
	Window            time.Duration `json:"window"`
	BanScore          int           `json:"banScore"`
	MaxPacketBytes    int           `json:"maxPacketBytes"`
	MaxUDPPerWindow   int           `json:"maxUdpPerWindow"`
	MaxTCPConnections int           `json:"maxTcpConnections"`
	MaxEventKeys      int           `json:"maxEventKeys"`
	SamplePerSource   int           `json:"samplePerSource"`
	NonceTTL          time.Duration `json:"nonceTtl"`
	BanTTLs           []TTLStep     `json:"banTTLs"`
	Allowlist         []net.IPNet   `json:"allowlist"`
}

// SecurityPolicy is the public name used by runtime and persistence layers.
// Policy remains as the compact implementation name for compatibility.
type SecurityPolicy = Policy

func DefaultPolicy() Policy {
	return Policy{
		Mode:              ModeProtect,
		Window:            10 * time.Second,
		BanScore:          100,
		MaxPacketBytes:    64 * 1024,
		MaxUDPPerWindow:   120,
		MaxTCPConnections: 32,
		MaxEventKeys:      4096,
		SamplePerSource:   3,
		NonceTTL:          60 * time.Second,
		BanTTLs:           []TTLStep{{Score: 100, TTL: 0}},
		Allowlist:         defaultAllowlist(),
	}
}

func defaultAllowlist() []net.IPNet {
	values := []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	items := make([]net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err == nil {
			items = append(items, *network)
		}
	}
	return items
}

func (p Policy) Validate() error {
	if p.Mode != ModeObserve && p.Mode != ModeProtect && p.Mode != ModeStrict {
		return ErrInvalidPolicy
	}
	if p.Window <= 0 || p.BanScore <= 0 || p.MaxPacketBytes <= 0 || p.MaxEventKeys <= 0 || p.NonceTTL <= 0 {
		return ErrInvalidPolicy
	}
	if p.MaxUDPPerWindow <= 0 || p.MaxTCPConnections <= 0 || p.SamplePerSource < 0 {
		return ErrInvalidPolicy
	}
	if len(p.BanTTLs) == 0 {
		return ErrInvalidPolicy
	}
	lastScore := 0
	for i, step := range p.BanTTLs {
		if step.Score <= lastScore || step.TTL < 0 || (step.TTL == 0 && i != len(p.BanTTLs)-1) {
			return ErrInvalidPolicy
		}
		lastScore = step.Score
	}
	return nil
}

func (p Policy) TTLForScore(score int) time.Duration {
	ttl, _ := p.BanForScore(score)
	return ttl
}

func (p Policy) BanForScore(score int) (time.Duration, bool) {
	var selected time.Duration
	matched := false
	for _, step := range p.BanTTLs {
		if score >= step.Score {
			selected = step.TTL
			matched = true
		}
	}
	return selected, matched
}

func (p Policy) PermanentForScore(score int) bool {
	ttl, matched := p.BanForScore(score)
	return matched && ttl == 0
}

// WithPermanentAutoBan applies the current automatic-ban policy. Historical
// decisions retain their original expiry.
func (p Policy) WithPermanentAutoBan() Policy {
	p.BanTTLs = []TTLStep{{Score: p.BanScore, TTL: 0}}
	return p
}

func (p Policy) IsAllowlisted(raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	for _, network := range p.Allowlist {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func ValidateSource(raw string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, ErrInvalidAddress
	}
	return ip, nil
}

type Event struct {
	TransactionID string    `json:"-"`
	SourceIP      string    `json:"sourceIp"`
	Transport     string    `json:"transport"`
	Method        string    `json:"method"`
	DeviceID      string    `json:"deviceId"`
	RiskScope     RiskScope `json:"riskScope"`
	UserAgent     string    `json:"userAgent"`
	Reason        Reason    `json:"reason"`
	Action        Action    `json:"action"`
	Score         int       `json:"score"`
	Occurred      time.Time `json:"occurredAt"`
}

type BanDecision struct {
	DecisionID       string        `json:"decisionId"`
	SourceIP         string        `json:"sourceIp"`
	DeviceID         string        `json:"deviceId,omitempty"`
	RiskScope        RiskScope     `json:"riskScope"`
	Reason           Reason        `json:"reason"`
	Score            int           `json:"score"`
	TTL              time.Duration `json:"ttl"`
	Permanent        bool          `json:"permanent"`
	CreatedAt        time.Time     `json:"createdAt"`
	TriggerMethod    string        `json:"triggerMethod"`
	TriggerCount     int           `json:"triggerCount"`
	TriggerThreshold int           `json:"triggerThreshold"`
	WindowSeconds    int           `json:"windowSeconds"`
	PolicyMode       Mode          `json:"policyMode"`
}

func (d BanDecision) ExpiresAt() time.Time {
	if d.Permanent {
		return time.Time{}
	}
	return d.CreatedAt.Add(d.TTL)
}

func (d BanDecision) ActiveAt(now time.Time) bool {
	if d.Permanent {
		return true
	}
	expiresAt := d.ExpiresAt()
	return !expiresAt.IsZero() && now.Before(expiresAt)
}

type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func RealClock() Clock { return realClock{} }

type FirewallAgentClient interface {
	Ban(BanDecision) error
	Unban(sourceIP string) error
	Status() AgentStatus
}

type AgentStatus struct {
	Connected    bool      `json:"connected"`
	AppliedRules int       `json:"appliedRules"`
	LastError    string    `json:"lastError,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
}

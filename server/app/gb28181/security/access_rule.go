package security

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
)

type AccessListType string
type AccessMatchType string
type AccessRuleStatus string
type EnforcementLayer string

const (
	ListBlacklist AccessListType = "blacklist"
	ListAllowlist AccessListType = "allowlist"

	MatchIP        AccessMatchType = "ip"
	MatchCIDR      AccessMatchType = "cidr"
	MatchUserAgent AccessMatchType = "user_agent"

	RuleEnabled  AccessRuleStatus = "enabled"
	RuleDisabled AccessRuleStatus = "disabled"

	EnforcementApplication  EnforcementLayer = "application"
	EnforcementHostFirewall EnforcementLayer = "host_firewall"
)

type AccessRule struct {
	ID         uint64           `json:"id"`
	ListType   AccessListType   `json:"listType"`
	MatchType  AccessMatchType  `json:"matchType"`
	MatchValue string           `json:"matchValue"`
	Scope      string           `json:"scope"`
	Status     AccessRuleStatus `json:"status"`
	ExpiresAt  *time.Time       `json:"expiresAt,omitempty"`
	Note       string           `json:"note"`
	CreatedBy  string           `json:"createdBy"`
	CreatedAt  time.Time        `json:"createdAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
}

func (r AccessRule) Validate() error {
	if r.ListType != ListBlacklist && r.ListType != ListAllowlist {
		return errors.New("invalid access list type")
	}
	value := strings.TrimSpace(r.MatchValue)
	if value == "" {
		return errors.New("access rule value is required")
	}
	switch r.MatchType {
	case MatchIP:
		if net.ParseIP(value) == nil {
			return ErrInvalidAddress
		}
	case MatchCIDR:
		if _, _, err := net.ParseCIDR(value); err != nil {
			return ErrInvalidAddress
		}
	case MatchUserAgent:
		if len(value) > 255 || strings.ContainsAny(value, "\r\n") {
			return errors.New("invalid user agent rule")
		}
	default:
		return errors.New("invalid access match type")
	}
	if r.Status != "" && r.Status != RuleEnabled && r.Status != RuleDisabled {
		return errors.New("invalid access rule status")
	}
	return nil
}

func (r AccessRule) Matches(sourceIP, userAgent string, now time.Time) bool {
	if r.Status != RuleEnabled || (r.ExpiresAt != nil && !now.Before(*r.ExpiresAt)) {
		return false
	}
	switch r.MatchType {
	case MatchIP:
		left, right := net.ParseIP(sourceIP), net.ParseIP(strings.TrimSpace(r.MatchValue))
		return left != nil && right != nil && left.Equal(right)
	case MatchCIDR:
		ip := net.ParseIP(sourceIP)
		_, network, err := net.ParseCIDR(strings.TrimSpace(r.MatchValue))
		return err == nil && ip != nil && network.Contains(ip)
	case MatchUserAgent:
		pattern := regexp.QuoteMeta(strings.ToLower(strings.TrimSpace(r.MatchValue)))
		pattern = strings.ReplaceAll(pattern, `\*`, ".*")
		matched, err := regexp.MatchString("^"+pattern+"$", strings.ToLower(strings.TrimSpace(userAgent)))
		return err == nil && matched
	default:
		return false
	}
}

func (r AccessRule) EnforcementLayer() EnforcementLayer {
	return EnforcementApplication
}

type AccessRuleMatcher struct{ rules []AccessRule }

func NewAccessRuleMatcher(rules []AccessRule) *AccessRuleMatcher {
	return &AccessRuleMatcher{rules: append([]AccessRule(nil), rules...)}
}

func (m *AccessRuleMatcher) IsTrustedSource(sourceIP, userAgent string, now time.Time) bool {
	if m == nil {
		return false
	}
	for _, rule := range m.rules {
		if rule.ListType == ListAllowlist && rule.MatchType != MatchUserAgent && rule.Matches(sourceIP, userAgent, now) {
			return true
		}
	}
	return false
}

func (m *AccessRuleMatcher) BlockedBy(sourceIP, userAgent string, now time.Time) (AccessRule, bool) {
	if m == nil {
		return AccessRule{}, false
	}
	for _, rule := range m.rules {
		if rule.ListType == ListBlacklist && rule.Matches(sourceIP, userAgent, now) {
			return rule, true
		}
	}
	return AccessRule{}, false
}

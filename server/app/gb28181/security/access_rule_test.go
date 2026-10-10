package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccessRuleValidationAndMatching(t *testing.T) {
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	ipRule := AccessRule{ListType: ListBlacklist, MatchType: MatchCIDR, MatchValue: "203.0.113.0/24", Status: RuleEnabled}
	require.NoError(t, ipRule.Validate())
	require.True(t, ipRule.Matches("203.0.113.8", "", now))
	require.False(t, ipRule.Matches("198.51.100.8", "", now))

	uaRule := AccessRule{ListType: ListBlacklist, MatchType: MatchUserAgent, MatchValue: "friendly-scanner*", Status: RuleEnabled}
	require.NoError(t, uaRule.Validate())
	require.True(t, uaRule.Matches("198.51.100.8", "Friendly-Scanner/1.0", now))
	require.Equal(t, EnforcementApplication, uaRule.EnforcementLayer())

	expired := ipRule
	expiresAt := now.Add(-time.Second)
	expired.ExpiresAt = &expiresAt
	require.False(t, expired.Matches("203.0.113.8", "", now))
}

func TestAccessRulesKeepUserAgentAllowlistOutOfTrustBypass(t *testing.T) {
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	rules := NewAccessRuleMatcher([]AccessRule{{ListType: ListAllowlist, MatchType: MatchUserAgent, MatchValue: "Hikvision*", Status: RuleEnabled}})
	require.False(t, rules.IsTrustedSource("198.51.100.8", "Hikvision-SIP/5.0", now))
}

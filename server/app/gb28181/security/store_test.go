package security

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

func newSecurityStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("disable_raise_record_not_found", gormhelper.MaskNotDataError))
	require.NoError(t, db.AutoMigrate(&securityEventRow{}, &securityBanRow{}, &securityPolicyRow{}, &securityAuditRow{}, &securityAccessRuleRow{}))
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS gb_device (device_id TEXT, transport TEXT, ip TEXT, register_time DATETIME, register_expire_at DATETIME, deleted_at DATETIME)`).Error)
	return db
}

func TestGormStoreMissingPolicyUsesDefaultsWithProductionQueryHook(t *testing.T) {
	store := NewGormStore(newSecurityStoreTestDB(t))
	policy, err := store.LoadPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, DefaultPolicy(), policy)
}

func TestGormStoreLoadsPolicyAndPersistsUpdateAudit(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	p := DefaultPolicy()
	p.Mode = ModeProtect
	p.Allowlist = mustNetworks(t, "10.0.0.0/8", "198.51.100.7/32")
	require.NoError(t, store.SavePolicy(context.Background(), p, "42"))

	got, err := store.LoadPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, ModeProtect, got.Mode)
	require.True(t, got.IsAllowlisted("198.51.100.7"))
	require.Equal(t, p.BanTTLs, got.BanTTLs)

	var audits []securityAuditRow
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "policy.update", audits[0].Action)
	require.Equal(t, "42", audits[0].Actor)
}

func TestGormStoreIncrementsAggregatesAcrossFlushes(t *testing.T) {
	store := NewGormStore(newSecurityStoreTestDB(t))
	at := time.Date(2026, 8, 9, 12, 0, 5, 0, time.UTC)
	event := EventAggregate{
		BucketAt: at.Truncate(time.Minute), SourceIP: "198.51.100.10", Transport: "UDP",
		Method: "INVITE", Reason: ReasonInviteRate, Action: ActionSample, Count: 2,
		ScoreDelta: 20, FirstSeenAt: at, LastSeenAt: at.Add(time.Second),
	}
	require.NoError(t, store.IncrementEvents(context.Background(), []EventAggregate{event}))
	event.Count = 3
	event.ScoreDelta = 30
	event.FirstSeenAt = at.Add(2 * time.Second)
	event.LastSeenAt = at.Add(3 * time.Second)
	require.NoError(t, store.IncrementEvents(context.Background(), []EventAggregate{event}))

	items, err := store.RecentEvents(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(5), items[0].Count)
	require.Equal(t, int64(50), items[0].ScoreDelta)
	require.Equal(t, at, items[0].FirstSeenAt)
	require.Equal(t, at.Add(3*time.Second), items[0].LastSeenAt)
}

func TestGormStoreRestoresAndUnbansActiveDecision(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	store := NewGormStore(db)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	active := FirewallBan{Decision: BanDecision{DecisionID: "d1", SourceIP: "203.0.113.8", Reason: ReasonInviteRate, Score: 120, CreatedAt: now, TTL: time.Hour}, Status: BanActive, RuleID: "d1", Origin: "auto", AgentState: "applied"}
	expired := FirewallBan{Decision: BanDecision{DecisionID: "d2", SourceIP: "203.0.113.9", Reason: ReasonInviteRate, Score: 120, CreatedAt: now.Add(-time.Hour), TTL: time.Minute}, Status: BanActive, RuleID: "d2", Origin: "auto", AgentState: "applied"}
	require.NoError(t, store.SaveBan(context.Background(), active))
	require.NoError(t, store.SaveBan(context.Background(), expired))

	items, err := store.ActiveBans(context.Background(), now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "d1", items[0].Decision.DecisionID)

	require.NoError(t, store.Unban(context.Background(), "203.0.113.8", "admin", now.Add(2*time.Minute)))
	require.NoError(t, store.Unban(context.Background(), "203.0.113.8", "admin", now.Add(3*time.Minute)))
	items, err = store.ActiveBans(context.Background(), now.Add(4*time.Minute))
	require.NoError(t, err)
	require.Empty(t, items)
	var audits []securityAuditRow
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "ban.unban", audits[0].Action)
}

func TestGormStoreCRUDsAccessRules(t *testing.T) {
	db := newSecurityStoreTestDB(t)
	require.NoError(t, db.AutoMigrate(&securityAccessRuleRow{}))
	store := NewGormStore(db)
	rule := AccessRule{ListType: ListBlacklist, MatchType: MatchIP, MatchValue: "203.0.113.8", Scope: "all_sip", Status: RuleEnabled, Note: "manual test", CreatedBy: "7"}
	require.NoError(t, store.CreateAccessRule(context.Background(), &rule))
	require.NotZero(t, rule.ID)

	items, err := store.ListAccessRules(context.Background(), ListBlacklist)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, EnforcementApplication, items[0].EnforcementLayer())

	rule.Status = RuleDisabled
	require.NoError(t, store.UpdateAccessRule(context.Background(), rule))
	require.NoError(t, store.DeleteAccessRule(context.Background(), rule.ID, "7"))
	items, err = store.ListAccessRules(context.Background(), ListBlacklist)
	require.NoError(t, err)
	require.Empty(t, items)
}

func mustNetworks(t *testing.T, values ...string) []net.IPNet {
	t.Helper()
	items := make([]net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		require.NoError(t, err)
		items = append(items, *network)
	}
	return items
}

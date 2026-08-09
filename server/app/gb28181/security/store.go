package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const globalPolicyScope = "global"

type Store interface {
	LoadPolicy(context.Context) (Policy, error)
	SavePolicy(context.Context, Policy, string) error
	IncrementEvents(context.Context, []EventAggregate) error
	RecentEvents(context.Context, int) ([]EventAggregate, error)
	SaveBan(context.Context, FirewallBan) error
	ActiveBans(context.Context, time.Time) ([]FirewallBan, error)
	RecentBans(context.Context, int, time.Time) ([]FirewallBan, error)
	Unban(context.Context, string, string, time.Time) error
	ListAccessRules(context.Context, AccessListType) ([]AccessRule, error)
	CreateAccessRule(context.Context, *AccessRule) error
	UpdateAccessRule(context.Context, AccessRule) error
	DeleteAccessRule(context.Context, uint64, string) error
}

type GormStore struct{ db *gorm.DB }

func NewGormStore(db *gorm.DB) *GormStore { return &GormStore{db: db} }

type securityEventRow struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	BucketAt      time.Time `gorm:"column:bucket_at;not null;uniqueIndex:uk_gb_sip_security_event,priority:1"`
	SourceIP      string    `gorm:"column:source_ip;size:64;not null;uniqueIndex:uk_gb_sip_security_event,priority:2"`
	AddressFamily string    `gorm:"column:address_family;size:8;not null"`
	Transport     string    `gorm:"column:transport;size:8;not null;uniqueIndex:uk_gb_sip_security_event,priority:3"`
	Method        string    `gorm:"column:method;size:16;not null;uniqueIndex:uk_gb_sip_security_event,priority:4"`
	UserAgent     string    `gorm:"column:user_agent;size:255;not null"`
	Reason        string    `gorm:"column:reason;size:32;not null;uniqueIndex:uk_gb_sip_security_event,priority:5"`
	Action        string    `gorm:"column:action;size:16;not null;uniqueIndex:uk_gb_sip_security_event,priority:6"`
	Count         int64     `gorm:"column:count;not null"`
	ScoreDelta    int64     `gorm:"column:score_delta;not null"`
	FirstSeenAt   time.Time `gorm:"column:first_seen_at;not null"`
	LastSeenAt    time.Time `gorm:"column:last_seen_at;not null"`
	SampleEventID string    `gorm:"column:sample_event_id;size:64;not null"`
}

func (securityEventRow) TableName() string { return "gb_sip_security_event" }

type securityBanRow struct {
	ID                   uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SourceIP             string     `gorm:"column:source_ip;size:64;not null;index:idx_gb_sip_security_ban_source_status,priority:1"`
	AddressFamily        string     `gorm:"column:address_family;size:8;not null"`
	Status               string     `gorm:"column:status;size:16;not null;index:idx_gb_sip_security_ban_source_status,priority:2"`
	Reason               string     `gorm:"column:reason;size:32;not null"`
	RuleID               string     `gorm:"column:rule_id;size:64;not null"`
	Score                int        `gorm:"column:score;not null"`
	CreatedAt            time.Time  `gorm:"column:created_at;not null"`
	ExpiresAt            *time.Time `gorm:"column:expires_at;index:idx_gb_sip_security_ban_expiry"`
	UnbannedAt           *time.Time `gorm:"column:unbanned_at"`
	UnbannedBy           string     `gorm:"column:unbanned_by;size:64;not null"`
	Origin               string     `gorm:"column:origin;size:16;not null"`
	AgentState           string     `gorm:"column:agent_state;size:16;not null"`
	DecisionID           string     `gorm:"column:decision_id;size:64;not null;uniqueIndex:uk_gb_sip_security_ban_decision"`
	LastError            string     `gorm:"column:last_error;size:512;not null"`
	TriggerMethod        string     `gorm:"column:trigger_method;size:16;not null"`
	TriggerCount         int        `gorm:"column:trigger_count;not null"`
	TriggerThreshold     int        `gorm:"column:trigger_threshold;not null"`
	WindowSeconds        int        `gorm:"column:window_seconds;not null"`
	PolicyMode           string     `gorm:"column:policy_mode;size:16;not null"`
	FirewallAppliedAt    *time.Time `gorm:"column:firewall_applied_at"`
	BlockedCountAfterBan int64      `gorm:"column:blocked_count_after_ban;not null"`
	LastBlockedAt        *time.Time `gorm:"column:last_blocked_at"`
}

func (securityBanRow) TableName() string { return "gb_sip_security_ban" }

type securityPolicyRow struct {
	ID                uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ScopeKey          string    `gorm:"column:scope_key;size:32;not null;uniqueIndex:uk_gb_sip_security_policy_scope"`
	Mode              string    `gorm:"column:mode;size:16;not null"`
	WindowSeconds     int       `gorm:"column:window_seconds;not null"`
	BanScore          int       `gorm:"column:ban_score;not null"`
	MaxPacketBytes    int       `gorm:"column:max_packet_bytes;not null"`
	MaxUDPPerWindow   int       `gorm:"column:max_udp_per_window;not null"`
	MaxTCPConnections int       `gorm:"column:max_tcp_connections;not null"`
	SamplePerSource   int       `gorm:"column:sample_per_source;not null"`
	NonceTTLSeconds   int       `gorm:"column:nonce_ttl_seconds;not null"`
	BanTTLSteps       string    `gorm:"column:ban_ttl_steps;size:1024;not null"`
	AllowlistText     string    `gorm:"column:allowlist_text;size:4096;not null"`
	UpdatedBy         uint64    `gorm:"column:updated_by;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

func (securityPolicyRow) TableName() string { return "gb_sip_security_policy" }

type securityAuditRow struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Actor      string    `gorm:"column:actor;size:64;not null"`
	Action     string    `gorm:"column:action;size:32;not null"`
	Target     string    `gorm:"column:target;size:128;not null"`
	Reason     string    `gorm:"column:reason;size:255;not null"`
	DecisionID string    `gorm:"column:decision_id;size:64;not null"`
	CreatedAt  time.Time `gorm:"column:created_at;not null;index:idx_gb_sip_security_audit_time"`
}

func (securityAuditRow) TableName() string { return "gb_sip_security_audit" }

type securityAccessRuleRow struct {
	ID         uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	ListType   string     `gorm:"column:list_type;size:16;not null;index:idx_gb_sip_security_access_rule_list_status,priority:1"`
	MatchType  string     `gorm:"column:match_type;size:16;not null"`
	MatchValue string     `gorm:"column:match_value;size:255;not null"`
	Scope      string     `gorm:"column:scope;size:32;not null"`
	Status     string     `gorm:"column:status;size:16;not null;index:idx_gb_sip_security_access_rule_list_status,priority:2"`
	ExpiresAt  *time.Time `gorm:"column:expires_at"`
	Note       string     `gorm:"column:note;size:255;not null"`
	CreatedBy  string     `gorm:"column:created_by;size:64;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null"`
}

func (securityAccessRuleRow) TableName() string { return "gb_sip_security_access_rule" }

func (s *GormStore) LoadPolicy(ctx context.Context) (Policy, error) {
	if s == nil || s.db == nil {
		return DefaultPolicy(), errors.New("security store unavailable")
	}
	var row securityPolicyRow
	err := s.db.WithContext(ctx).Where("scope_key = ?", globalPolicyScope).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultPolicy(), nil
	}
	if err != nil {
		return Policy{}, err
	}
	return row.policy()
}

func (s *GormStore) SavePolicy(ctx context.Context, policy Policy, actor string) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	row := policyRow(policy, actor)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "scope_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"mode", "window_seconds", "ban_score", "max_packet_bytes", "max_udp_per_window", "max_tcp_connections", "sample_per_source", "nonce_ttl_seconds", "ban_ttl_steps", "allowlist_text", "updated_by", "updated_at"}),
		}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Create(&securityAuditRow{Actor: actorOrSystem(actor), Action: "policy.update", Target: globalPolicyScope, Reason: "security policy updated", CreatedAt: row.UpdatedAt}).Error
	})
}

func (s *GormStore) IncrementEvents(ctx context.Context, items []EventAggregate) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range items {
			row := eventRow(item)
			var existing securityEventRow
			err := tx.Where("bucket_at = ? AND source_ip = ? AND transport = ? AND method = ? AND reason = ? AND action = ?", row.BucketAt, row.SourceIP, row.Transport, row.Method, row.Reason, row.Action).First(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			firstSeen := existing.FirstSeenAt
			if row.FirstSeenAt.Before(firstSeen) {
				firstSeen = row.FirstSeenAt
			}
			lastSeen := existing.LastSeenAt
			if row.LastSeenAt.After(lastSeen) {
				lastSeen = row.LastSeenAt
			}
			if err := tx.Model(&securityEventRow{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
				"count":         gorm.Expr("count + ?", row.Count),
				"score_delta":   gorm.Expr("score_delta + ?", row.ScoreDelta),
				"first_seen_at": firstSeen,
				"last_seen_at":  lastSeen,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *GormStore) RecentEvents(ctx context.Context, limit int) ([]EventAggregate, error) {
	limit = boundedLimit(limit, 500)
	var rows []securityEventRow
	if err := s.db.WithContext(ctx).Order("last_seen_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]EventAggregate, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		items = append(items, rows[i].aggregate())
	}
	return items, nil
}

func (s *GormStore) SaveBan(ctx context.Context, item FirewallBan) error {
	row := banRow(item)
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "decision_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "rule_id", "unbanned_at", "unbanned_by", "agent_state", "last_error", "firewall_applied_at", "blocked_count_after_ban", "last_blocked_at"}),
	}).Create(&row).Error
}

func (s *GormStore) ActiveBans(ctx context.Context, now time.Time) ([]FirewallBan, error) {
	if err := s.expireBans(ctx, now); err != nil {
		return nil, err
	}
	var rows []securityBanRow
	if err := s.db.WithContext(ctx).Where("status IN ? AND (expires_at IS NULL OR expires_at > ?)", []string{string(BanActive), string(BanAgentFailed)}, now).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return bansFromRows(rows), nil
}

func (s *GormStore) RecentBans(ctx context.Context, limit int, now time.Time) ([]FirewallBan, error) {
	if err := s.expireBans(ctx, now); err != nil {
		return nil, err
	}
	var rows []securityBanRow
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(boundedLimit(limit, 500)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return bansFromRows(rows), nil
}

func (s *GormStore) Unban(ctx context.Context, identifier, actor string, at time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row securityBanRow
		err := tx.Where("(source_ip = ? OR decision_id = ?) AND status IN ?", identifier, identifier, []string{string(BanActive), string(BanAgentFailed)}).Order("created_at DESC").First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&securityBanRow{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"status": string(BanUnbanned), "unbanned_at": at, "unbanned_by": actorOrSystem(actor), "agent_state": "removed"}).Error; err != nil {
			return err
		}
		return tx.Create(&securityAuditRow{Actor: actorOrSystem(actor), Action: "ban.unban", Target: row.SourceIP, Reason: "manual unban", DecisionID: row.DecisionID, CreatedAt: at}).Error
	})
}

func (s *GormStore) ListAccessRules(ctx context.Context, listType AccessListType) ([]AccessRule, error) {
	query := s.db.WithContext(ctx).Model(&securityAccessRuleRow{})
	if listType != "" {
		query = query.Where("list_type = ?", listType)
	}
	var rows []securityAccessRuleRow
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]AccessRule, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.rule())
	}
	return items, nil
}

func (s *GormStore) CreateAccessRule(ctx context.Context, rule *AccessRule) error {
	if rule == nil {
		return errors.New("access rule is required")
	}
	normalizeAccessRule(rule)
	if err := rule.Validate(); err != nil {
		return err
	}
	now := time.Now()
	rule.CreatedAt, rule.UpdatedAt = now, now
	row := accessRuleRow(*rule)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		rule.ID = row.ID
		return tx.Create(&securityAuditRow{Actor: actorOrSystem(rule.CreatedBy), Action: "access_rule.create", Target: strconv.FormatUint(row.ID, 10), Reason: string(rule.ListType) + ":" + string(rule.MatchType), CreatedAt: now}).Error
	})
}

func (s *GormStore) UpdateAccessRule(ctx context.Context, rule AccessRule) error {
	if rule.ID == 0 {
		return errors.New("access rule id is required")
	}
	normalizeAccessRule(&rule)
	if err := rule.Validate(); err != nil {
		return err
	}
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&securityAccessRuleRow{}).Where("id = ?", rule.ID).Updates(map[string]interface{}{
			"list_type": rule.ListType, "match_type": rule.MatchType, "match_value": rule.MatchValue,
			"scope": rule.Scope, "status": rule.Status, "expires_at": rule.ExpiresAt,
			"note": rule.Note, "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(&securityAuditRow{Actor: actorOrSystem(rule.CreatedBy), Action: "access_rule.update", Target: strconv.FormatUint(rule.ID, 10), Reason: string(rule.ListType) + ":" + string(rule.MatchType), CreatedAt: now}).Error
	})
}

func (s *GormStore) DeleteAccessRule(ctx context.Context, id uint64, actor string) error {
	if id == 0 {
		return errors.New("access rule id is required")
	}
	now := time.Now()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(&securityAccessRuleRow{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(&securityAuditRow{Actor: actorOrSystem(actor), Action: "access_rule.delete", Target: strconv.FormatUint(id, 10), Reason: "manual access rule removed", CreatedAt: now}).Error
	})
}

func (s *GormStore) expireBans(ctx context.Context, now time.Time) error {
	return s.db.WithContext(ctx).Model(&securityBanRow{}).Where("status IN ? AND expires_at IS NOT NULL AND expires_at <= ?", []string{string(BanActive), string(BanAgentFailed)}, now).Updates(map[string]interface{}{"status": string(BanExpired), "agent_state": "expired"}).Error
}

func policyRow(policy Policy, actor string) securityPolicyRow {
	updatedBy, _ := strconv.ParseUint(strings.TrimSpace(actor), 10, 64)
	return securityPolicyRow{ScopeKey: globalPolicyScope, Mode: string(policy.Mode), WindowSeconds: int(policy.Window / time.Second), BanScore: policy.BanScore, MaxPacketBytes: policy.MaxPacketBytes, MaxUDPPerWindow: policy.MaxUDPPerWindow, MaxTCPConnections: policy.MaxTCPConnections, SamplePerSource: policy.SamplePerSource, NonceTTLSeconds: int(policy.NonceTTL / time.Second), BanTTLSteps: encodeTTLSteps(policy.BanTTLs), AllowlistText: encodeAllowlist(policy.Allowlist), UpdatedBy: updatedBy, UpdatedAt: time.Now()}
}

func (r securityPolicyRow) policy() (Policy, error) {
	ttls, err := decodeTTLSteps(r.BanTTLSteps)
	if err != nil {
		return Policy{}, err
	}
	allowlist, err := decodeAllowlist(r.AllowlistText)
	if err != nil {
		return Policy{}, err
	}
	p := DefaultPolicy()
	p.Mode = Mode(r.Mode)
	p.Window = time.Duration(r.WindowSeconds) * time.Second
	p.BanScore = r.BanScore
	p.MaxPacketBytes = r.MaxPacketBytes
	p.MaxUDPPerWindow = r.MaxUDPPerWindow
	p.MaxTCPConnections = r.MaxTCPConnections
	p.SamplePerSource = r.SamplePerSource
	p.NonceTTL = time.Duration(r.NonceTTLSeconds) * time.Second
	p.BanTTLs = ttls
	p.Allowlist = allowlist
	return p, p.Validate()
}

func eventRow(item EventAggregate) securityEventRow {
	return securityEventRow{BucketAt: item.BucketAt, SourceIP: item.SourceIP, AddressFamily: addressFamily(item.SourceIP), Transport: strings.ToUpper(item.Transport), Method: strings.ToUpper(item.Method), UserAgent: item.UserAgent, Reason: string(item.Reason), Action: string(item.Action), Count: item.Count, ScoreDelta: item.ScoreDelta, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt}
}

func (r securityEventRow) aggregate() EventAggregate {
	return EventAggregate{BucketAt: r.BucketAt, SourceIP: r.SourceIP, Transport: r.Transport, Method: r.Method, UserAgent: r.UserAgent, Reason: Reason(r.Reason), Action: Action(r.Action), Count: r.Count, ScoreDelta: r.ScoreDelta, FirstSeenAt: r.FirstSeenAt, LastSeenAt: r.LastSeenAt}
}

func banRow(item FirewallBan) securityBanRow {
	var unbannedAt *time.Time
	if !item.UnbannedAt.IsZero() {
		value := item.UnbannedAt
		unbannedAt = &value
	}
	var firewallAppliedAt, lastBlockedAt *time.Time
	if !item.FirewallAppliedAt.IsZero() {
		value := item.FirewallAppliedAt
		firewallAppliedAt = &value
	}
	if !item.LastBlockedAt.IsZero() {
		value := item.LastBlockedAt
		lastBlockedAt = &value
	}
	var expiresAt *time.Time
	if value := item.Decision.ExpiresAt(); !value.IsZero() {
		expiresAt = &value
	}
	return securityBanRow{SourceIP: item.Decision.SourceIP, AddressFamily: addressFamily(item.Decision.SourceIP), Status: string(item.Status), Reason: string(item.Decision.Reason), RuleID: item.RuleID, Score: item.Decision.Score, CreatedAt: item.Decision.CreatedAt, ExpiresAt: expiresAt, UnbannedAt: unbannedAt, UnbannedBy: item.UnbannedBy, Origin: item.Origin, AgentState: item.AgentState, DecisionID: item.Decision.DecisionID, LastError: item.LastError, TriggerMethod: item.Decision.TriggerMethod, TriggerCount: item.Decision.TriggerCount, TriggerThreshold: item.Decision.TriggerThreshold, WindowSeconds: item.Decision.WindowSeconds, PolicyMode: string(item.Decision.PolicyMode), FirewallAppliedAt: firewallAppliedAt, BlockedCountAfterBan: item.BlockedCountAfterBan, LastBlockedAt: lastBlockedAt}
}

func accessRuleRow(rule AccessRule) securityAccessRuleRow {
	return securityAccessRuleRow{ID: rule.ID, ListType: string(rule.ListType), MatchType: string(rule.MatchType), MatchValue: rule.MatchValue, Scope: rule.Scope, Status: string(rule.Status), ExpiresAt: rule.ExpiresAt, Note: rule.Note, CreatedBy: rule.CreatedBy, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt}
}

func (r securityAccessRuleRow) rule() AccessRule {
	return AccessRule{ID: r.ID, ListType: AccessListType(r.ListType), MatchType: AccessMatchType(r.MatchType), MatchValue: r.MatchValue, Scope: r.Scope, Status: AccessRuleStatus(r.Status), ExpiresAt: r.ExpiresAt, Note: r.Note, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func normalizeAccessRule(rule *AccessRule) {
	rule.MatchValue = strings.TrimSpace(rule.MatchValue)
	rule.Scope = strings.TrimSpace(rule.Scope)
	if rule.Scope == "" {
		rule.Scope = "all_sip"
	}
	if rule.Status == "" {
		rule.Status = RuleEnabled
	}
	if rule.MatchType == MatchIP {
		if ip := net.ParseIP(rule.MatchValue); ip != nil {
			rule.MatchValue = ip.String()
		}
	}
	if rule.MatchType == MatchCIDR {
		if _, network, err := net.ParseCIDR(rule.MatchValue); err == nil {
			rule.MatchValue = network.String()
		}
	}
}

func (r securityBanRow) ban() FirewallBan {
	decision := BanDecision{DecisionID: r.DecisionID, SourceIP: r.SourceIP, Reason: Reason(r.Reason), Score: r.Score, CreatedAt: r.CreatedAt, TriggerMethod: r.TriggerMethod, TriggerCount: r.TriggerCount, TriggerThreshold: r.TriggerThreshold, WindowSeconds: r.WindowSeconds, PolicyMode: Mode(r.PolicyMode)}
	if r.ExpiresAt == nil {
		decision.Permanent = true
	} else {
		decision.TTL = r.ExpiresAt.Sub(r.CreatedAt)
	}
	item := FirewallBan{Decision: decision, Status: BanStatus(r.Status), RuleID: r.RuleID, Origin: r.Origin, AgentState: r.AgentState, UnbannedBy: r.UnbannedBy, LastError: r.LastError, BlockedCountAfterBan: r.BlockedCountAfterBan}
	if r.UnbannedAt != nil {
		item.UnbannedAt = *r.UnbannedAt
	}
	if r.FirewallAppliedAt != nil {
		item.FirewallAppliedAt = *r.FirewallAppliedAt
	}
	if r.LastBlockedAt != nil {
		item.LastBlockedAt = *r.LastBlockedAt
	}
	return item
}

func bansFromRows(rows []securityBanRow) []FirewallBan {
	items := make([]FirewallBan, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.ban())
	}
	return items
}

func encodeTTLSteps(steps []TTLStep) string {
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		parts = append(parts, fmt.Sprintf("%d:%d", step.Score, int64(step.TTL/time.Second)))
	}
	return strings.Join(parts, ";")
}

func decodeTTLSteps(raw string) ([]TTLStep, error) {
	parts := strings.Split(strings.TrimSpace(raw), ";")
	steps := make([]TTLStep, 0, len(parts))
	for _, part := range parts {
		fields := strings.Split(strings.TrimSpace(part), ":")
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid ban ttl step %q", part)
		}
		score, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, err
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, err
		}
		steps = append(steps, TTLStep{Score: score, TTL: time.Duration(seconds) * time.Second})
	}
	return steps, nil
}

func encodeAllowlist(items []net.IPNet) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.String())
	}
	sort.Strings(values)
	return strings.Join(values, "\n")
}

func decodeAllowlist(raw string) ([]net.IPNet, error) {
	var items []net.IPNet
	for _, line := range strings.Fields(raw) {
		_, network, err := net.ParseCIDR(line)
		if err != nil {
			return nil, err
		}
		items = append(items, *network)
	}
	return items, nil
}

func addressFamily(sourceIP string) string {
	ip := net.ParseIP(sourceIP)
	if ip != nil && ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

func boundedLimit(limit, maximum int) int {
	if limit <= 0 || limit > maximum {
		return maximum
	}
	return limit
}

func actorOrSystem(actor string) string {
	if strings.TrimSpace(actor) == "" {
		return "system"
	}
	return strings.TrimSpace(actor)
}

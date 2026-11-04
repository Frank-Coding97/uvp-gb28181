package controllers

import (
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"uvplatform.com/uvp-gb28181/app/utils/response"

	"github.com/gin-gonic/gin"
	appcontrollers "uvplatform.com/uvp-gb28181/app/controllers"
	gbsecurity "uvplatform.com/uvp-gb28181/app/gb28181/security"
)

type SecuritySnapshot struct {
	Mode    gbsecurity.Mode             `json:"mode"`
	Dropped int64                       `json:"dropped"`
	Sampled int64                       `json:"sampled"`
	Events  []gbsecurity.EventAggregate `json:"events"`
	Bans    []gbsecurity.FirewallBan    `json:"bans"`
	Agent   gbsecurity.AgentStatus      `json:"agent"`
	AsOf    time.Time                   `json:"asOf"`
}

// SecurityEventView 在聚合数据上补一个服务端判定的风险等级。
// 台账页按等级翻页时，筛选与展示必须同源，不能让前端各判一套。
type SecurityEventView struct {
	gbsecurity.EventAggregate
	Severity gbsecurity.EventSeverity `json:"severity"`
}

func securityEventViews(items []gbsecurity.EventAggregate) []SecurityEventView {
	views := make([]SecurityEventView, 0, len(items))
	for _, item := range items {
		views = append(views, SecurityEventView{
			EventAggregate: item,
			Severity:       gbsecurity.EventSeverityFor(item.Action, item.Reason),
		})
	}
	return views
}

// securityKeyword 统一收口：去空白、转小写，空白关键字视为不过滤。
func securityKeyword(ctx *gin.Context) string {
	return strings.ToLower(strings.TrimSpace(ctx.Query("keyword")))
}

func securityKeywordMatch(keyword string, fields ...string) bool {
	if keyword == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), keyword) {
			return true
		}
	}
	return false
}

// banFirewallState 把 agentState 归一成台账上展示的三档，口径与前端一致。
func banFirewallState(ban gbsecurity.FirewallBan) string {
	switch ban.AgentState {
	case "applied":
		return "applied"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}

func securityBadQuery(ctx *gin.Context, message string) {
	response.SetBusinessResult(ctx, http.StatusBadRequest, false)
	ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": message})
}

type SecurityPolicyView struct {
	Mode              gbsecurity.Mode `json:"mode"`
	Window            int             `json:"window"`
	BanScore          int             `json:"banScore"`
	MaxPacketBytes    int             `json:"maxPacketBytes"`
	MaxUDPPerWindow   int             `json:"maxUdpPerWindow"`
	MaxTCPConnections int             `json:"maxTcpConnections"`
	SamplePerSource   int             `json:"samplePerSource"`
	NonceTTL          int             `json:"nonceTtl"`
	PermanentAutoBan  bool            `json:"permanentAutoBan"`
	BanTTLs           []struct {
		Score int `json:"score"`
		TTL   int `json:"ttl"`
	} `json:"banTTLs"`
	Allowlist []string `json:"allowlist"`
}

func securityPolicyView(policy gbsecurity.SecurityPolicy) SecurityPolicyView {
	view := SecurityPolicyView{Mode: policy.Mode, Window: int(policy.Window / time.Second), BanScore: policy.BanScore, MaxPacketBytes: policy.MaxPacketBytes, MaxUDPPerWindow: policy.MaxUDPPerWindow, MaxTCPConnections: policy.MaxTCPConnections, SamplePerSource: policy.SamplePerSource, NonceTTL: int(policy.NonceTTL / time.Second), PermanentAutoBan: policy.PermanentForScore(policy.BanScore)}
	for _, step := range policy.BanTTLs {
		view.BanTTLs = append(view.BanTTLs, struct {
			Score int `json:"score"`
			TTL   int `json:"ttl"`
		}{Score: step.Score, TTL: int(step.TTL / time.Second)})
	}
	for _, network := range policy.Allowlist {
		view.Allowlist = append(view.Allowlist, network.String())
	}
	return view
}

func policyFromView(view SecurityPolicyView) gbsecurity.SecurityPolicy {
	policy := gbsecurity.DefaultPolicy()
	policy.Mode, policy.Window, policy.BanScore = view.Mode, time.Duration(view.Window)*time.Second, view.BanScore
	policy.MaxPacketBytes, policy.MaxUDPPerWindow, policy.MaxTCPConnections = view.MaxPacketBytes, view.MaxUDPPerWindow, view.MaxTCPConnections
	policy.SamplePerSource, policy.NonceTTL = view.SamplePerSource, time.Duration(view.NonceTTL)*time.Second
	policy = policy.WithPermanentAutoBan()
	if view.Allowlist != nil {
		policy.Allowlist = nil
		for _, raw := range view.Allowlist {
			if _, network, err := net.ParseCIDR(raw); err == nil {
				policy.Allowlist = append(policy.Allowlist, *network)
			}
		}
	}
	return policy
}

type SecurityProvider interface {
	Snapshot() SecuritySnapshot
	Events() []gbsecurity.EventAggregate
	Bans() []gbsecurity.FirewallBan
	Policy() gbsecurity.SecurityPolicy
	UpdatePolicy(gbsecurity.SecurityPolicy, string) error
	Unban(string, string) error
	AccessRules(gbsecurity.AccessListType) []gbsecurity.AccessRule
	CreateAccessRule(*gbsecurity.AccessRule, string) error
	UpdateAccessRule(gbsecurity.AccessRule, string) error
	DeleteAccessRule(uint64, string) error
	AgentStatus() gbsecurity.AgentStatus
	Stream() (<-chan SecuritySnapshot, func())
}

type SecurityController struct {
	appcontrollers.Common
	provider SecurityProvider
}

func NewSecurityController(provider SecurityProvider) *SecurityController {
	return &SecurityController{provider: provider}
}
func (c *SecurityController) SetProvider(provider SecurityProvider) { c.provider = provider }

func (c *SecurityController) Snapshot(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	c.Success(ctx, c.provider.Snapshot())
}

func (c *SecurityController) Events(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	severity := gbsecurity.EventSeverity(strings.ToLower(strings.TrimSpace(ctx.Query("severity"))))
	if !severity.Valid() {
		securityBadQuery(ctx, "invalid severity")
		return
	}
	keyword := securityKeyword(ctx)
	source := c.provider.Events()
	items := make([]gbsecurity.EventAggregate, 0, len(source))
	for _, item := range source {
		if !securityKeywordMatch(keyword, item.SourceIP, item.DeviceID, item.Method, item.UserAgent, string(item.Reason), string(item.Action)) {
			continue
		}
		if severity != "" && gbsecurity.EventSeverityFor(item.Action, item.Reason) != severity {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].LastSeenAt.Equal(items[j].LastSeenAt) {
			return items[i].SourceIP < items[j].SourceIP
		}
		return items[i].LastSeenAt.After(items[j].LastSeenAt)
	})
	page, pageSize := securityPageParams(ctx)
	total := len(items)
	c.Success(ctx, gin.H{"items": securityEventViews(securityPage(items, page, pageSize)), "total": total, "page": page, "pageSize": pageSize})
}

func (c *SecurityController) Bans(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	firewallState := strings.ToLower(strings.TrimSpace(ctx.Query("firewallState")))
	if firewallState != "" && firewallState != "applied" && firewallState != "pending" && firewallState != "failed" {
		securityBadQuery(ctx, "invalid firewallState")
		return
	}
	keyword := securityKeyword(ctx)
	items := append([]gbsecurity.FirewallBan(nil), c.provider.Bans()...)
	if ctx.Query("activeOnly") == "true" {
		activeItems := make([]gbsecurity.FirewallBan, 0, len(items))
		for _, item := range items {
			if item.Status == gbsecurity.BanActive || item.Status == gbsecurity.BanAgentFailed {
				activeItems = append(activeItems, item)
			}
		}
		items = activeItems
	}
	if keyword != "" || firewallState != "" {
		filtered := make([]gbsecurity.FirewallBan, 0, len(items))
		for _, item := range items {
			if !securityKeywordMatch(keyword,
				item.Decision.SourceIP,
				item.Decision.DeviceID,
				string(item.Decision.Reason),
				item.Decision.TriggerMethod,
				string(item.Decision.PolicyMode),
			) {
				continue
			}
			if firewallState != "" && banFirewallState(item) != firewallState {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Decision.CreatedAt.Equal(items[j].Decision.CreatedAt) {
			return items[i].Decision.DecisionID > items[j].Decision.DecisionID
		}
		return items[i].Decision.CreatedAt.After(items[j].Decision.CreatedAt)
	})
	page, pageSize := securityPageParams(ctx)
	total := len(items)
	c.Success(ctx, gin.H{"items": securityPage(items, page, pageSize), "total": total, "page": page, "pageSize": pageSize})
}

func (c *SecurityController) Policy(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	c.Success(ctx, securityPolicyView(c.provider.Policy()))
}

func (c *SecurityController) UpdatePolicy(ctx *gin.Context) {
	var view SecurityPolicyView
	if err := ctx.ShouldBindJSON(&view); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	policy := policyFromView(view)
	if err := policy.Validate(); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if c.provider == nil {
		response.SetBusinessResult(ctx, http.StatusServiceUnavailable, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security provider unavailable"})
		return
	}
	if err := c.provider.UpdatePolicy(policy, strconv.FormatUint(uint64(c.GetCurrentUserID(ctx)), 10)); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, securityPolicyView(policy))
}

func (c *SecurityController) Unban(ctx *gin.Context) {
	if c.provider == nil {
		response.SetBusinessResult(ctx, http.StatusServiceUnavailable, false)
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security provider unavailable"})
		return
	}
	if err := c.provider.Unban(ctx.Param("id"), strconv.FormatUint(uint64(c.GetCurrentUserID(ctx)), 10)); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, gin.H{"id": ctx.Param("id"), "status": "unbanned"})
}

func (c *SecurityController) AgentHealth(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	c.Success(ctx, c.provider.AgentStatus())
}

func (c *SecurityController) AccessRules(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	listType := gbsecurity.AccessListType(ctx.Query("listType"))
	if listType != "" && listType != gbsecurity.ListBlacklist && listType != gbsecurity.ListAllowlist {
		securityBadQuery(ctx, "invalid listType")
		return
	}
	matchType := gbsecurity.AccessMatchType(strings.ToLower(strings.TrimSpace(ctx.Query("matchType"))))
	if matchType != "" && matchType != gbsecurity.MatchIP && matchType != gbsecurity.MatchCIDR && matchType != gbsecurity.MatchUserAgent {
		securityBadQuery(ctx, "invalid matchType")
		return
	}
	status := gbsecurity.AccessRuleStatus(strings.ToLower(strings.TrimSpace(ctx.Query("status"))))
	if status != "" && status != gbsecurity.RuleEnabled && status != gbsecurity.RuleDisabled {
		securityBadQuery(ctx, "invalid status")
		return
	}
	keyword := securityKeyword(ctx)
	source := c.provider.AccessRules(listType)
	items := make([]gbsecurity.AccessRule, 0, len(source))
	for _, item := range source {
		if !securityKeywordMatch(keyword, item.MatchValue, item.Note, item.Scope, string(item.MatchType)) {
			continue
		}
		if matchType != "" && item.MatchType != matchType {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, pageSize := securityPageParams(ctx)
	total := len(items)
	c.Success(ctx, gin.H{"items": securityPage(items, page, pageSize), "total": total, "page": page, "pageSize": pageSize})
}

func securityPageParams(ctx *gin.Context) (int, int) {
	page, pageErr := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(ctx.DefaultQuery("pageSize", "20"))
	if pageErr != nil || page <= 0 {
		page = 1
	}
	if pageSizeErr != nil || pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func securityPage[T any](items []T, page, pageSize int) []T {
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func (c *SecurityController) CreateAccessRule(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	var rule gbsecurity.AccessRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if err := c.provider.CreateAccessRule(&rule, strconv.FormatUint(uint64(c.GetCurrentUserID(ctx)), 10)); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, rule)
}

func (c *SecurityController) UpdateAccessRule(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid access rule id"})
		return
	}
	var rule gbsecurity.AccessRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	rule.ID = id
	if err := c.provider.UpdateAccessRule(rule, strconv.FormatUint(uint64(c.GetCurrentUserID(ctx)), 10)); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, rule)
}

func (c *SecurityController) DeleteAccessRule(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid access rule id"})
		return
	}
	if err := c.provider.DeleteAccessRule(id, strconv.FormatUint(uint64(c.GetCurrentUserID(ctx)), 10)); err != nil {
		response.SetBusinessResult(ctx, http.StatusBadRequest, false)
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, gin.H{"id": id, "deleted": true})
}

func (c *SecurityController) Stream(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ch, cancel := c.provider.Stream()
	defer cancel()
	ctx.SSEvent("ready", c.provider.Snapshot())
	ctx.Writer.Flush()
	for {
		select {
		case <-ctx.Request.Context().Done():
			return
		case snapshot, ok := <-ch:
			if !ok {
				return
			}
			ctx.SSEvent("snapshot", snapshot)
			ctx.Writer.Flush()
		case <-time.After(30 * time.Second):
			ctx.SSEvent("ping", gin.H{"t": time.Now().Unix()})
			ctx.Writer.Flush()
		}
	}
}

func securityUnavailable(ctx *gin.Context) {
	response.SetBusinessResult(ctx, http.StatusServiceUnavailable, false)
	ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security runtime unavailable"})
}

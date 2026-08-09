package controllers

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	appcontrollers "uvplatform.cn/uvp-gb28181/app/controllers"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
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
	view := SecurityPolicyView{Mode: policy.Mode, Window: int(policy.Window / time.Second), BanScore: policy.BanScore, MaxPacketBytes: policy.MaxPacketBytes, MaxUDPPerWindow: policy.MaxUDPPerWindow, MaxTCPConnections: policy.MaxTCPConnections, SamplePerSource: policy.SamplePerSource, NonceTTL: int(policy.NonceTTL / time.Second), PermanentAutoBan: true}
	view.BanTTLs = append(view.BanTTLs, struct {
		Score int `json:"score"`
		TTL   int `json:"ttl"`
	}{Score: policy.BanScore, TTL: 0})
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
	policy.BanTTLs = []gbsecurity.TTLStep{{Score: policy.BanScore, TTL: 0}}
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
	items := []gbsecurity.EventAggregate{}
	items = c.provider.Events()
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	c.Success(ctx, gin.H{"items": items, "total": len(items), "limit": limit})
}

func (c *SecurityController) Bans(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	items := c.provider.Bans()
	c.Success(ctx, gin.H{"items": items, "total": len(items)})
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
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	policy := policyFromView(view)
	if err := policy.Validate(); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if c.provider == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security provider unavailable"})
		return
	}
	if err := c.provider.UpdatePolicy(policy, ctx.GetString("userId")); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, securityPolicyView(policy))
}

func (c *SecurityController) Unban(ctx *gin.Context) {
	if c.provider == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security provider unavailable"})
		return
	}
	if err := c.provider.Unban(ctx.Param("id"), ctx.GetString("userId")); err != nil {
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
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid listType"})
		return
	}
	items := c.provider.AccessRules(listType)
	c.Success(ctx, gin.H{"items": items, "total": len(items)})
}

func (c *SecurityController) CreateAccessRule(ctx *gin.Context) {
	if c.provider == nil {
		securityUnavailable(ctx)
		return
	}
	var rule gbsecurity.AccessRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if err := c.provider.CreateAccessRule(&rule, ctx.GetString("userId")); err != nil {
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
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid access rule id"})
		return
	}
	var rule gbsecurity.AccessRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	rule.ID = id
	if err := c.provider.UpdateAccessRule(rule, ctx.GetString("userId")); err != nil {
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
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid access rule id"})
		return
	}
	if err := c.provider.DeleteAccessRule(id, ctx.GetString("userId")); err != nil {
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
	ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security runtime unavailable"})
}

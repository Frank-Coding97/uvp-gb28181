package controllers

import (
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

type SecurityProvider interface {
	Snapshot() SecuritySnapshot
	Events() []gbsecurity.EventAggregate
	Bans() []gbsecurity.FirewallBan
	Policy() gbsecurity.SecurityPolicy
	UpdatePolicy(gbsecurity.SecurityPolicy) error
	Unban(string, string) error
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
		c.Success(ctx, SecuritySnapshot{Mode: gbsecurity.ModeObserve, Events: []gbsecurity.EventAggregate{}, Bans: []gbsecurity.FirewallBan{}, AsOf: time.Now()})
		return
	}
	c.Success(ctx, c.provider.Snapshot())
}

func (c *SecurityController) Events(ctx *gin.Context) {
	items := []gbsecurity.EventAggregate{}
	if c.provider != nil {
		items = c.provider.Events()
	}
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
	items := []gbsecurity.FirewallBan{}
	if c.provider != nil {
		items = c.provider.Bans()
	}
	c.Success(ctx, gin.H{"items": items, "total": len(items)})
}

func (c *SecurityController) Policy(ctx *gin.Context) {
	if c.provider == nil {
		c.Success(ctx, gbsecurity.DefaultPolicy())
		return
	}
	c.Success(ctx, c.provider.Policy())
}

func (c *SecurityController) UpdatePolicy(ctx *gin.Context) {
	var policy gbsecurity.SecurityPolicy
	if err := ctx.ShouldBindJSON(&policy); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if err := policy.Validate(); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	if c.provider == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "message": "security provider unavailable"})
		return
	}
	if err := c.provider.UpdatePolicy(policy); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.Success(ctx, policy)
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
		c.Success(ctx, gbsecurity.AgentStatus{})
		return
	}
	c.Success(ctx, c.provider.AgentStatus())
}

func (c *SecurityController) Stream(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	if c.provider == nil {
		ctx.SSEvent("ready", SecuritySnapshot{Mode: gbsecurity.ModeObserve, AsOf: time.Now()})
		ctx.Writer.Flush()
		return
	}
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

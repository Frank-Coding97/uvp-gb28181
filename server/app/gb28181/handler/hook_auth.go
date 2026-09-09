package handler

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type HookAuthNodeResolver interface {
	GetByUUID(string) (*node.Node, bool)
}

type HookRejectMode uint8

const (
	HookRejectNotification HookRejectMode = iota
	HookRejectAdmission
	HookRejectNoneReader
)

type HookNodeIdentity struct {
	ID              int64
	MediaServerUUID string
}

const hookNodeContextKey = "uvp.gb28181.authenticated-hook-node"

type HookAuthenticator struct {
	mu       sync.RWMutex
	resolver HookAuthNodeResolver
	limiter  *rate.Limiter
}

func NewHookAuthenticator() *HookAuthenticator {
	return &HookAuthenticator{limiter: rate.NewLimiter(8, 16)}
}

func (a *HookAuthenticator) SetResolver(resolver HookAuthNodeResolver) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.resolver = resolver
	a.mu.Unlock()
}

func (a *HookAuthenticator) Middleware(event playauth.HookEvent, rejectMode HookRejectMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil || !event.Valid() {
			rejectHook(c, event, rejectMode, "auth-runtime-unavailable", a)
			return
		}
		nodeID, nodeOK := singleValue(c.Request.URL.Query(), "node")
		capability, capOK := singleValue(c.Request.URL.Query(), "cap")
		if !nodeOK || !capOK {
			rejectHook(c, event, rejectMode, "credentials-invalid", a)
			return
		}

		a.mu.RLock()
		resolver := a.resolver
		a.mu.RUnlock()
		if resolver == nil {
			rejectHook(c, event, rejectMode, "auth-runtime-unavailable", a)
			return
		}
		mediaNode, ok := resolver.GetByUUID(nodeID)
		if !ok || mediaNode == nil || mediaNode.MediaServerUUID != nodeID {
			rejectHook(c, event, rejectMode, "node-unknown", a)
			return
		}
		if !playauth.VerifyHookCapability(mediaNode.APISecret, nodeID, event, capability) {
			rejectHook(c, event, rejectMode, "capability-invalid", a)
			return
		}

		c.Set(hookNodeContextKey, HookNodeIdentity{ID: mediaNode.ID, MediaServerUUID: mediaNode.MediaServerUUID})
		c.Next()
	}
}

func AuthenticatedHookNode(c *gin.Context) (HookNodeIdentity, bool) {
	if c == nil {
		return HookNodeIdentity{}, false
	}
	value, ok := c.Get(hookNodeContextKey)
	if !ok {
		return HookNodeIdentity{}, false
	}
	identity, ok := value.(HookNodeIdentity)
	return identity, ok && identity.MediaServerUUID != ""
}

func hookPayloadNodeMatches(c *gin.Context, event playauth.HookEvent, payloadNodeID string) bool {
	identity, authenticated := AuthenticatedHookNode(c)
	if !authenticated || payloadNodeID == "" || identity.MediaServerUUID == payloadNodeID {
		return true
	}
	hookLog(c).Warn("ZLM Hook 载荷节点不匹配",
		zap.String("event", "gb28181.hook.auth.node_mismatch"),
		zap.String("hook_event", string(event)),
		zap.String("node", identity.MediaServerUUID),
		zap.String("payloadNode", payloadNodeID))
	return false
}

func rejectHook(c *gin.Context, event playauth.HookEvent, mode HookRejectMode, reason string, authenticator *HookAuthenticator) {
	if shouldLogHookRejection(authenticator) {
		hookLog(c).Warn("ZLM Hook 认证已拒绝",
			zap.String("event", "gb28181.hook.auth.rejected"),
			zap.String("hook_event", string(event)),
			zap.String("node", safeHookNodeQuery(c)),
			zap.String("sourceIp", hookSourceIP(c.Request.RemoteAddr)),
			zap.String("reason", reason))
	}
	c.Abort()
	switch mode {
	case HookRejectAdmission:
		response.SetBusinessResult(c, -1, false)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "hook authorization denied"})
	case HookRejectNoneReader:
		response.SetBusinessResult(c, 0, true)
		c.JSON(http.StatusOK, gin.H{"code": 0, "close": false})
	default:
		response.SetBusinessResult(c, 0, true)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	}
}

func shouldLogHookRejection(authenticator *HookAuthenticator) bool {
	return authenticator == nil || authenticator.limiter == nil || authenticator.limiter.Allow()
}

func safeHookNodeQuery(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	value, ok := singleValue(c.Request.URL.Query(), "node")
	if !ok || len(value) > 128 {
		return ""
	}
	return value
}

func hookSourceIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return ""
	}
	return host
}

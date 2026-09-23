package handler

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

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

// HookRetiredObserver 在认证 miss 时回答"这个自称 node_id 的 uuid 是不是平台**主动
// 退休掉**的节点"，并顺带安排一次撤销对端 hook 的重试。
//
// ⛔ 它与 HookAuthNodeResolver 问的是**两个不同的事实**，必须分开：
//   - resolver miss ⇒ `node_unknown`：平台根本不认识它（伪造、串台、别人家的平台写错了地址）；
//   - observer 命中 ⇒ `node_retired`：平台**认识**它，而且是自己把它删掉的，凭据还在，
//     对端的残留本该被撤掉。
//
// 把后者混进前者，会让"我们自己删过、还没撤干净"和"有个陌生人一直在敲门"
// 在日志里长得一模一样，值班的人没法据此判断该不该紧张。
type HookRetiredObserver interface {
	ObserveRetiredHook(uuid, sourceIP string) bool
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
	retired  HookRetiredObserver
	limiter  *rate.Limiter
	// rejections 折叠同源的慢性重复拒绝（详见 hook_rejection_log.go）。
	// 构造后只读，不再加锁。
	rejections *hookRejectionTracker
}

func NewHookAuthenticator() *HookAuthenticator {
	return &HookAuthenticator{
		limiter:    rate.NewLimiter(8, 16),
		rejections: newHookRejectionTracker(),
	}
}

func (a *HookAuthenticator) rejectionTracker() *hookRejectionTracker {
	if a == nil {
		return nil
	}
	return a.rejections
}

func (a *HookAuthenticator) SetResolver(resolver HookAuthNodeResolver) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.resolver = resolver
	a.mu.Unlock()
}

// SetRetiredObserver 装配"已退休节点"观察器（见 HookRetiredObserver）。
func (a *HookAuthenticator) SetRetiredObserver(observer HookRetiredObserver) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.retired = observer
	a.mu.Unlock()
}

func (a *HookAuthenticator) retiredObserver() HookRetiredObserver {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.retired
}

func (a *HookAuthenticator) Middleware(event playauth.HookEvent, rejectMode HookRejectMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		if a == nil || !event.Valid() {
			rejectHook(c, event, rejectMode, "auth_runtime_unavailable", a)
			return
		}
		nodeID, nodeOK := singleValue(c.Request.URL.Query(), "node")
		capability, capOK := singleValue(c.Request.URL.Query(), "cap")
		if !nodeOK || !capOK {
			rejectHook(c, event, rejectMode, "credentials_invalid", a)
			return
		}

		a.mu.RLock()
		resolver := a.resolver
		a.mu.RUnlock()
		if resolver == nil {
			rejectHook(c, event, rejectMode, "auth_runtime_unavailable", a)
			return
		}
		mediaNode, ok := resolver.GetByUUID(nodeID)
		if !ok || mediaNode == nil || mediaNode.MediaServerUUID != nodeID {
			// 查不到这个节点 —— 但要先分辨"不认识"还是"我们自己删过它"：
			// 后者有凭据、能撤销，前者只能靠折叠兜底。详见 HookRetiredObserver。
			if observeRetiredHook(c, nodeID, a) {
				rejectHook(c, event, rejectMode, "node_retired", a)
				return
			}
			rejectHook(c, event, rejectMode, "node_unknown", a)
			return
		}
		if !playauth.VerifyHookCapability(mediaNode.APISecret, nodeID, event, capability) {
			rejectHook(c, event, rejectMode, "capability_invalid", a)
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
	// 这条的答案就是"哪个节点、报的是哪个节点"——两边都用事件目录里定义的规范名
	// `node_id`（`media_server_id` 是同一维度，但 hook 链路统一用 `node_id`）。
	hookLog(c).Warn("ZLM Hook 载荷节点不匹配",
		zap.String("event", "gb28181.hook.auth.node_mismatch"),
		zap.String("hook_event", string(event)),
		zap.String("node_id", identity.MediaServerUUID),
		zap.String("payload_node_id", payloadNodeID))
	return false
}

func rejectHook(c *gin.Context, event playauth.HookEvent, mode HookRejectMode, reasonCode string, authenticator *HookAuthenticator) {
	logHookRejection(c, event, reasonCode, authenticator)
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

// logHookRejection 记录一次"hook 被拒"，并把同源的慢性重复折叠掉。
//
// 直接按次打 WARN 会让一个已被移除、却仍在回调的 ZLM 把控制台刷满（心跳 10s 一条）；
// 折叠规则与理由见 hook_rejection_log.go。
func logHookRejection(c *gin.Context, event playauth.HookEvent, reasonCode string, authenticator *HookAuthenticator) {
	// 被拒绝时**对方还没通过认证**，所以"是谁"只能由 `source_ip` + 它自称的
	// `node_id` 一起回答（`reason_code` 说为什么拒）。三个字段缺一不可：
	// 只有 reason 的拒绝日志等于"有人被拒了，但不知道是谁"。
	nodeID := safeHookNodeQuery(c)
	sourceIP := hookSourceIP(c.Request.RemoteAddr)
	if !hookRejectionFoldable[reasonCode] {
		// 平台自身故障（`auth_runtime_unavailable`）与尚未判过的新原因不折叠。
		if !shouldLogHookRejection(authenticator) {
			return
		}
		logRejectedHook(c, event, reasonCode, nodeID, sourceIP)
		return
	}
	verdict, record := authenticator.rejectionTracker().observe(hookRejectionKey{
		reason:    reasonCode,
		nodeID:    nodeID,
		sourceIP:  sourceIP,
		hookEvent: string(event),
	}, time.Now())

	switch verdict {
	case HookRejectionSuppressed:
		// 窗口内的其余重复不进日志：它们是同一条事实的第 N 次播报，
		// 下一窗口的汇总会用 `suppressed_count` 把它们交代清楚。
	case HookRejectionSummary:
		// 汇总不占令牌桶：它的频率由窗口决定（同一来源每窗口至多一条），
		// 不参与秒级风暴，压掉它只会丢掉"这里在持续被拒"的计数。
		hookLog(c).Warn("ZLM Hook 认证在同一来源上重复被拒",
			zap.String("event", "gb28181.hook.auth.rejected_summary"),
			zap.String("hook_event", string(event)),
			zap.String("node_id", nodeID),
			zap.String("source_ip", sourceIP),
			zap.String("reason_code", reasonCode),
			zap.Int("suppressed_count", record.suppressed),
			zap.Int("window_seconds", int(hookRejectionWindow.Seconds())))
	default:
		if !shouldLogHookRejection(authenticator) {
			return
		}
		logRejectedHook(c, event, reasonCode, nodeID, sourceIP)
	}
}

func logRejectedHook(c *gin.Context, event playauth.HookEvent, reasonCode, nodeID, sourceIP string) {
	hookLog(c).Warn("ZLM Hook 认证已拒绝",
		zap.String("event", "gb28181.hook.auth.rejected"),
		zap.String("hook_event", string(event)),
		zap.String("node_id", nodeID),
		zap.String("source_ip", sourceIP),
		zap.String("reason_code", reasonCode))
}

func shouldLogHookRejection(authenticator *HookAuthenticator) bool {
	return authenticator == nil || authenticator.limiter == nil || authenticator.limiter.Allow()
}

// observeRetiredHook 问一次"这个 uuid 是不是我们主动退休的节点"。
//
// 观察器自己会处理"该不该重试解约"，这里只负责把"谁、从哪来"传过去 ——
// 认证失败的这一刻对方还没通过认证，`source_ip` 是唯一可信的来源标识。
func observeRetiredHook(c *gin.Context, uuid string, authenticator *HookAuthenticator) bool {
	observer := authenticator.retiredObserver()
	if observer == nil || uuid == "" {
		return false
	}
	return observer.ObserveRetiredHook(uuid, hookSourceIP(c.Request.RemoteAddr))
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

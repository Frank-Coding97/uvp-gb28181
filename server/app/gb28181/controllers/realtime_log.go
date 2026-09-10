package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

const maxRealtimeConnections = 20
const maxRealtimeLifetime = 30 * time.Minute

var realtimeConnections atomic.Int32
var realtimeUserConnections = struct {
	sync.Mutex
	counts map[uint]int
}{counts: make(map[uint]int)}

// RealtimeLogController exposes only the redacted business event stream.
type RealtimeLogController struct{}

func NewRealtimeLogController() *RealtimeLogController { return &RealtimeLogController{} }

func (c *RealtimeLogController) Stream(ctx *gin.Context) {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		ctx.JSON(http.StatusForbidden, gin.H{"message": "无权查看实时业务日志"})
		return
	}
	if app.CasbinV2 == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"message": "权限服务不可用"})
		return
	}
	roles, err := app.CasbinV2.GetRolesForUserByID(claims.UserID)
	if err != nil || !containsRole(roles, 1) {
		ctx.JSON(http.StatusForbidden, gin.H{"message": "首版实时业务日志仅系统管理员可用"})
		return
	}
	if app.RealtimeLogHub == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"message": "实时业务日志未启用"})
		return
	}
	if app.TokenService == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"message": "认证服务不可用"})
		return
	}
	if realtimeConnections.Add(1) > maxRealtimeConnections {
		realtimeConnections.Add(-1)
		ctx.JSON(http.StatusTooManyRequests, gin.H{"message": "实时日志连接数已达上限"})
		return
	}
	defer realtimeConnections.Add(-1)
	realtimeUserConnections.Lock()
	if realtimeUserConnections.counts[claims.UserID] >= 3 {
		realtimeUserConnections.Unlock()
		ctx.JSON(http.StatusTooManyRequests, gin.H{"message": "当前用户实时日志连接数已达上限"})
		return
	}
	realtimeUserConnections.counts[claims.UserID]++
	realtimeUserConnections.Unlock()
	defer func() {
		realtimeUserConnections.Lock()
		realtimeUserConnections.counts[claims.UserID]--
		if realtimeUserConnections.counts[claims.UserID] <= 0 {
			delete(realtimeUserConnections.counts, claims.UserID)
		}
		realtimeUserConnections.Unlock()
	}()
	filter := logging.RealtimeFilter{Level: strings.TrimSpace(ctx.Query("level")), Module: strings.TrimSpace(ctx.Query("module")), Event: strings.TrimSpace(ctx.Query("event")), DeviceID: strings.TrimSpace(ctx.Query("deviceId")), ChannelID: strings.TrimSpace(ctx.Query("channelId")), NodeID: strings.TrimSpace(ctx.Query("nodeId")), StreamID: strings.TrimSpace(ctx.Query("streamId")), CallID: strings.TrimSpace(ctx.Query("callId")), RequestID: strings.TrimSpace(ctx.Query("requestId")), OperationID: strings.TrimSpace(ctx.Query("operationId")), CorrelationID: strings.TrimSpace(ctx.Query("correlationId"))}
	var since uint64
	if raw := ctx.Query("since"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"message": "since 参数非法"})
			return
		}
		since = n
	}
	snapshotLimit := 100
	if raw := ctx.Query("snapshot"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 200 {
			ctx.JSON(http.StatusBadRequest, gin.H{"message": "snapshot 参数必须在 0 到 200 之间"})
			return
		}
		snapshotLimit = n
	}
	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("X-Accel-Buffering", "no")
	ctx.Status(http.StatusOK)
	ctx.Writer.Flush()
	sub, snapshot, gap := app.RealtimeLogHub.Subscribe(ctx.Request.Context(), filter, since)
	defer app.RealtimeLogHub.Unsubscribe(sub.ID)
	startedAt := time.Now()
	app.Log(ctx.Request.Context()).Named("realtime_log").Info("实时业务日志订阅已建立", zap.String("event", "realtime_log.subscription_started"), zap.Uint("user_id", claims.UserID), zap.String("subscription_id", sub.ID))
	defer func() {
		app.Log(ctx.Request.Context()).Named("realtime_log").Info("实时业务日志订阅已结束", zap.String("event", "realtime_log.subscription_ended"), zap.Uint("user_id", claims.UserID), zap.String("subscription_id", sub.ID), zap.Uint64("dropped", sub.Dropped()), zap.Duration("duration", time.Since(startedAt)))
	}()
	write := func(event string, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(ctx.Writer, "event: %s\ndata: %s\n\n", event, b)
		if err == nil {
			ctx.Writer.Flush()
		}
		return err
	}
	_ = write("ready", map[string]any{"subscriptionId": sub.ID, "instanceId": app.RealtimeLogHub.InstanceID(), "latestSequence": app.RealtimeLogHub.LatestSequence()})
	if gap {
		_ = write("gap", map[string]any{"reason": "history_unavailable", "since": since, "latestSequence": app.RealtimeLogHub.LatestSequence()})
	}
	if len(snapshot) > snapshotLimit {
		snapshot = snapshot[len(snapshot)-snapshotLimit:]
	}
	for _, event := range snapshot {
		if err := write("message", event); err != nil {
			return
		}
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	lifetime := time.NewTimer(maxRealtimeLifetime)
	defer lifetime.Stop()
	for {
		select {
		case <-ctx.Request.Context().Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			if err := write("message", event); err != nil {
				return
			}
			if dropped := sub.TakeDropped(); dropped > 0 {
				_ = write("dropped", map[string]any{"count": dropped})
			}
		case now := <-ping.C:
			if err := write("ping", map[string]any{"at": now}); err != nil {
				return
			}
		case <-lifetime.C:
			_ = write("auth_expired", map[string]any{"reason": "max_connection_lifetime"})
			return
		}
	}
}

func containsRole(roles []uint, expected uint) bool {
	for _, role := range roles {
		if role == expected {
			return true
		}
	}
	return false
}

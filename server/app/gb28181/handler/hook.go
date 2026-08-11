package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// PlayStopper 由点播 service 实现:停掉一路流(BYE + closeRtpServer)
// 接口化便于 hook 端点接入,且让 handler 包不依赖 play 包(避免循环导入)
type PlayStopper interface {
	Stop(ctx context.Context, streamID string) error
}

type GenerationPlayStopper interface {
	CurrentLiveRef(streamID string) (stream.LiveRef, bool)
	StopIfCurrent(ctx context.Context, ref stream.LiveRef) (bool, error)
	StopOnNoneReader(ctx context.Context, captured stream.LiveRef) (bool, error)
}

// NoneReaderPolicy 查询流对应通道的无人观看断流策略。
type NoneReaderPolicy interface {
	ShouldCloseOnNoneReader(ctx context.Context, streamID string) (bool, error)
}

// SourceLeaseChecker marks streams that still have an active non-browser
// consumer. ZLM must keep such a source alive even when its reader count is 0.
type SourceLeaseChecker interface {
	HasLease(streamID string) bool
}

// KeepaliveCollector 由 heartbeat.Collector 实现:接收 ZLM on_server_keepalive 上报
// 接口化避免 handler 包反向依赖 heartbeat 包
type KeepaliveCollector interface {
	Receive(payload []byte) error
}

// NodeUUIDResolver 把 mediaServerUUID 反查为 nodeID(由 node.Registry 实现)
type NodeUUIDResolver interface {
	IDForUUID(uuid string) (int64, bool)
}

// StreamLocationBinder 流位置表 Bind 端(由 stream.LocationMap 实现)
type StreamLocationBinder interface {
	Bind(streamID string, nodeID int64)
}

type RecordMP4Indexer interface {
	IndexRecordMP4(context.Context, int64, recording.RecordMP4Event) (bool, error)
}

type StreamObserver interface {
	ObserveStream(context.Context, string, bool) error
}

type PlaybackMediaSink interface {
	OnPlaybackStreamEnded(context.Context, string, string) error
}

type TalkPublishRequest struct {
	NodeID       int64
	App          string
	SourceStream string
	PublishToken string
	PublishID    string
}

type TalkPublishAuthorizer interface {
	AuthorizeTalkPublish(context.Context, TalkPublishRequest) (bool, error)
}

type TalkStreamObserver interface {
	ObserveTalkStream(context.Context, int64, string, string, bool) error
}

type PlayAuthorizer interface {
	Verify(string, playauth.Binding) (playauth.Claims, error)
}

type PlaybackMediaContextResolver interface {
	ResolvePlaybackMediaContext(app, stream, mediaServerID string) (playauth.Binding, error)
}

type AutoOnDemandNodeResolver interface {
	ResolveAutoOnDemandNode(mediaServerID string) (*node.Node, bool)
}

type AutoOnDemandTargetValidator interface {
	ValidateAutoOnDemandTarget(context.Context, string, string) error
}

type AutoOnDemandDispatcher interface {
	Available() bool
	Submit(play.Request) error
}

type AutoOnDemandSettingsProvider func() gbconfig.FixedAddressPlaybackSettings

// HookController 接收 ZLMediaKit 的 Hook 回调
// ZLM 以 POST JSON 调用,响应需返回 {"code":0,"msg":"success"}
type HookController struct {
	notifier       *stream.Notifier     // 流就绪事件分发(由点播 service 订阅,T6 创新3)
	stopper        PlayStopper          // 无人观看/超时时调用,可为 nil(降级:仅返回 close=true,不发 BYE)
	policy         NoneReaderPolicy     // 通道级无人观看断流策略,可为 nil(兼容旧行为)
	leaseChecker   SourceLeaseChecker   // 级联 source lease,可为 nil
	collector      KeepaliveCollector   // on_server_keepalive 转发目标,可为 nil(降级:仅 200 OK)
	resolver       NodeUUIDResolver     // M2 多节点 UUID 反查,可为 nil(降级:单节点不 Bind)
	binder         StreamLocationBinder // M2 LocationMap 反向 Bind(防 service.Start 漏 Bind)
	recordMP4      RecordMP4Indexer
	recordResolver NodeUUIDResolver
	observer       StreamObserver
	playbackMedia  PlaybackMediaSink
	talkResolver   NodeUUIDResolver
	talkAuthorizer TalkPublishAuthorizer
	talkObserver   TalkStreamObserver
	talkMu         sync.RWMutex
	playAuthorizer PlayAuthorizer
	playResolver   PlaybackMediaContextResolver
	playAuthMu     sync.RWMutex
	autoMu         sync.RWMutex
	autoResolver   AutoOnDemandNodeResolver
	autoValidator  AutoOnDemandTargetValidator
	autoDispatcher AutoOnDemandDispatcher
	autoSettings   AutoOnDemandSettingsProvider
	autoLimiter    *rate.Limiter
}

func NewHookController(notifier *stream.Notifier) *HookController {
	return &HookController{
		notifier:     notifier,
		autoSettings: gbconfig.CurrentFixedAddressPlaybackSettings,
		autoLimiter:  rate.NewLimiter(32, 32),
	}
}

// SetPlayStopper 注入点播停止器(bootstrap 装配 play service 后调用)
func (h *HookController) SetPlayStopper(s PlayStopper) {
	h.stopper = s
}

// SetNoneReaderPolicy 注入通道级无人观看断流策略。
func (h *HookController) SetNoneReaderPolicy(p NoneReaderPolicy) {
	h.policy = p
}

// SetSourceLeaseChecker injects the cascade lease registry without coupling
// the hook package to cascade/media.
func (h *HookController) SetSourceLeaseChecker(checker SourceLeaseChecker) {
	h.leaseChecker = checker
}

// SetKeepaliveCollector 注入心跳收集器(bootstrap M2.1 装配 heartbeat.Collector 后调用)
func (h *HookController) SetKeepaliveCollector(c KeepaliveCollector) {
	h.collector = c
}

// SetMultiNode 注入多节点路由能力(M2.4 bootstrap 多节点装配后调用)
func (h *HookController) SetMultiNode(resolver NodeUUIDResolver, binder StreamLocationBinder) {
	h.resolver = resolver
	h.binder = binder
}

func (h *HookController) SetRecordMP4Indexer(resolver NodeUUIDResolver, indexer RecordMP4Indexer) {
	h.recordResolver = resolver
	h.recordMP4 = indexer
}

func (h *HookController) SetStreamObserver(observer StreamObserver) {
	h.observer = observer
}

func (h *HookController) SetPlaybackMediaSink(sink PlaybackMediaSink) {
	h.playbackMedia = sink
}

func (h *HookController) SetTalk(resolver NodeUUIDResolver, authorizer TalkPublishAuthorizer, observer TalkStreamObserver) {
	h.talkMu.Lock()
	defer h.talkMu.Unlock()
	h.talkResolver = resolver
	h.talkAuthorizer = authorizer
	h.talkObserver = observer
}

func (h *HookController) SetPlayAuthorizer(authorizer PlayAuthorizer) {
	h.playAuthMu.Lock()
	defer h.playAuthMu.Unlock()
	h.playAuthorizer = authorizer
}

func (h *HookController) SetPlaybackMediaContextResolver(resolver PlaybackMediaContextResolver) {
	h.playAuthMu.Lock()
	defer h.playAuthMu.Unlock()
	h.playResolver = resolver
}

func (h *HookController) SetAutoOnDemandRuntime(
	resolver AutoOnDemandNodeResolver,
	validator AutoOnDemandTargetValidator,
	dispatcher AutoOnDemandDispatcher,
) {
	h.autoMu.Lock()
	defer h.autoMu.Unlock()
	h.autoResolver = resolver
	h.autoValidator = validator
	h.autoDispatcher = dispatcher
}

func (h *HookController) SetAutoOnDemandSettingsProvider(provider AutoOnDemandSettingsProvider) {
	h.autoMu.Lock()
	defer h.autoMu.Unlock()
	if provider == nil {
		h.autoSettings = gbconfig.CurrentFixedAddressPlaybackSettings
		return
	}
	h.autoSettings = provider
}

// hookOK ZLM 期望的标准成功响应
func hookOK(c *gin.Context) {
	c.JSON(200, gin.H{"code": 0, "msg": "success"})
}

// onStreamChangedBody on_stream_changed 回调载荷(只取我们需要的字段)
type onStreamChangedBody struct {
	App           string `json:"app"`
	Stream        string `json:"stream"`
	Regist        bool   `json:"regist"`
	Schema        string `json:"schema"`
	MediaServerID string `json:"mediaServerId"` // M2: ZLM 在 general.mediaServerId 配置的节点 UUID
}

// OnStreamChanged 流注册/注销事件(regist=true 流就绪,false 流消失)
//
// M2 行为:regist=true 时若 payload 含 mediaServerId 且能反查节点,则 Bind LocationMap(兜底)
func (h *HookController) OnStreamChanged(c *gin.Context) {
	var body onStreamChangedBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_changed",
		zap.String("app", body.App),
		zap.String("stream", body.Stream),
		zap.String("schema", body.Schema),
		zap.String("mediaServerId", body.MediaServerID),
		zap.Bool("regist", body.Regist))

	// 流就绪 → 通知正在 WaitReady 的点播 service
	if body.Regist && h.notifier != nil && body.Stream != "" {
		h.notifier.Publish(body.Stream)

		// M2: 兜底反向 Bind(防 service.Start 漏写 / ZLM 主动推流场景)
		if h.resolver != nil && h.binder != nil && body.MediaServerID != "" {
			if nodeID, ok := h.resolver.IDForUUID(body.MediaServerID); ok {
				h.binder.Bind(body.Stream, nodeID)
			}
		}
	}
	if body.App != "talk" && h.observer != nil && body.Stream != "" {
		go func(streamID string, registered bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.observer.ObserveStream(ctx, streamID, registered); err != nil {
				app.ZapLog.Warn("录像流状态联动失败", zap.String("stream", streamID), zap.Bool("regist", registered), zap.Error(err))
			}
		}(body.Stream, body.Regist)
	}
	if !body.Regist && h.playbackMedia != nil && body.Stream != "" {
		h.notifyPlaybackEnded(body.Stream, "media-offline")
	}
	talkResolver, _, talkObserver := h.talkDependencies()
	if body.App == "talk" && talkObserver != nil && talkResolver != nil && body.Stream != "" && body.MediaServerID != "" {
		if nodeID, ok := talkResolver.IDForUUID(body.MediaServerID); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := talkObserver.ObserveTalkStream(ctx, nodeID, body.App, body.Stream, body.Regist); err != nil {
					app.ZapLog.Warn("对讲流状态联动失败", zap.String("stream", body.Stream), zap.Bool("regist", body.Regist), zap.Error(err))
				}
			}()
		}
	}
	hookOK(c)
}

// onStreamNoneReaderBody on_stream_none_reader 回调载荷
type onStreamNoneReaderBody struct {
	App    string `json:"app"`
	Stream string `json:"stream"`
	Schema string `json:"schema"`
}

// OnStreamNoneReader 无人观看 → ZLM 询问是否关流
// 返回 close=true 让 ZLM 立即关流;同时异步向设备发 BYE 释放上行
func (h *HookController) OnStreamNoneReader(c *gin.Context) {
	var body onStreamNoneReaderBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_stream_none_reader",
		zap.String("app", body.App), zap.String("stream", body.Stream))

	closeStream := true
	policyFailed := false
	if h.leaseChecker != nil && body.Stream != "" && h.leaseChecker.HasLease(body.Stream) {
		closeStream = false
	} else if h.policy != nil && body.Stream != "" {
		var err error
		closeStream, err = h.policy.ShouldCloseOnNoneReader(c.Request.Context(), body.Stream)
		if err != nil {
			app.ZapLog.Warn("查询无人观看断流策略失败,沿用默认关闭策略",
				zap.String("stream", body.Stream), zap.Error(err))
			closeStream = true
			policyFailed = true
		}
	}

	_, _, fixedErr := play.ParseFixedStreamID(body.Stream)
	isFixedLive := body.App == "rtp" && fixedErr == nil
	if closeStream && isFixedLive {
		// 固定 stream ID 会跨代复用，不能让 ZLM 按裸 stream 名立即关闭。
		// 捕获当前代并由 service 复查 readerCount 后执行条件清理。
		closeStream = false
		if !policyFailed {
			if stopper, ok := h.stopper.(GenerationPlayStopper); ok {
				if captured, exists := stopper.CurrentLiveRef(body.Stream); exists {
					go func(ref stream.LiveRef) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if _, err := stopper.StopOnNoneReader(ctx, ref); err != nil {
							app.ZapLog.Warn("固定流无人观看条件断流失败", zap.String("stream", ref.StreamID), zap.Error(err))
						}
					}(captured)
				}
			}
		}
	} else if closeStream && h.stopper != nil && body.Stream != "" {
		// 动态流名不会跨代复用，保留历史快速关闭行为。
		go func(streamID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.stopper.Stop(ctx, streamID); err != nil {
				app.ZapLog.Warn("无人观看自动断流失败",
					zap.String("stream", streamID), zap.Error(err))
			} else {
				app.ZapLog.Info("无人观看自动断流", zap.String("stream", streamID))
			}
		}(body.Stream)
	}

	c.JSON(200, gin.H{"code": 0, "close": closeStream})
}

// onRtpServerTimeoutBody on_rtp_server_timeout 回调载荷
type onRtpServerTimeoutBody struct {
	StreamID      string   `json:"stream_id"`
	App           string   `json:"app"`
	SSRC          hookSSRC `json:"ssrc"`
	MediaServerID string   `json:"mediaServerId"`
}

type hookSSRC string

func (s *hookSSRC) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), "\"")
	if raw == "" || raw == "null" {
		*s = ""
		return nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value > 9_999_999_999 {
		return fmt.Errorf("invalid ssrc")
	}
	*s = hookSSRC(fmt.Sprintf("%010d", value))
	return nil
}

// OnRtpServerTimeout RTP 收流超时 → 设备实际没推流,清理会话
func (h *HookController) OnRtpServerTimeout(c *gin.Context) {
	var body onRtpServerTimeoutBody
	_ = c.ShouldBindJSON(&body)
	app.ZapLog.Info("ZLM Hook on_rtp_server_timeout",
		zap.String("stream_id", body.StreamID), zap.String("ssrc", string(body.SSRC)),
		zap.String("mediaServerId", body.MediaServerID))

	_, _, fixedErr := play.ParseFixedStreamID(body.StreamID)
	isFixedLive := body.App == "rtp" && fixedErr == nil
	if isFixedLive {
		stopper, stopperOK := h.stopper.(GenerationPlayStopper)
		nodeID, nodeOK := int64(0), false
		if h.resolver != nil && body.MediaServerID != "" {
			nodeID, nodeOK = h.resolver.IDForUUID(body.MediaServerID)
		}
		if stopperOK && nodeOK && body.SSRC != "" {
			if current, ok := stopper.CurrentLiveRef(body.StreamID); ok &&
				current.NodeID == nodeID && current.SSRC == string(body.SSRC) {
				go func(ref stream.LiveRef) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, err := stopper.StopIfCurrent(ctx, ref); err != nil {
						app.ZapLog.Warn("RTP 超时条件清理会话失败", zap.String("stream", ref.StreamID), zap.Error(err))
					}
				}(current)
			}
		}
	} else if h.stopper != nil && body.StreamID != "" {
		go func(streamID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.stopper.Stop(ctx, streamID); err != nil {
				app.ZapLog.Warn("RTP 超时清理会话失败",
					zap.String("stream", streamID), zap.Error(err))
			}
		}(body.StreamID)
	}
	if h.playbackMedia != nil && body.StreamID != "" {
		h.notifyPlaybackEnded(body.StreamID, "rtp-timeout")
	}
	hookOK(c)
}

func (h *HookController) notifyPlaybackEnded(streamID, reason string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.playbackMedia.OnPlaybackStreamEnded(ctx, streamID, reason); err != nil && !errors.Is(err, context.Canceled) {
			app.ZapLog.Debug("回放媒体终态未命中活动会话", zap.String("stream", streamID), zap.String("reason", reason), zap.Error(err))
		}
	}()
}

type onPublishBody struct {
	App           string `json:"app"`
	Stream        string `json:"stream"`
	Params        string `json:"params"`
	ID            string `json:"id"`
	MediaServerID string `json:"mediaServerId"`
}

// OnPublish 普通推流保持放行；talk app 必须通过一次性会话授权。
func (h *HookController) OnPublish(c *gin.Context) {
	var body onPublishBody
	_ = c.ShouldBindJSON(&body)
	if body.App != "talk" {
		hookOK(c)
		return
	}
	talkResolver, talkAuthorizer, _ := h.talkDependencies()
	if talkResolver == nil || talkAuthorizer == nil || body.MediaServerID == "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "talk publish authorization unavailable"})
		return
	}
	nodeID, ok := talkResolver.IDForUUID(body.MediaServerID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "unknown media server"})
		return
	}
	params, err := url.ParseQuery(strings.TrimPrefix(body.Params, "?"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "invalid talk publish parameters"})
		return
	}
	allowed, err := talkAuthorizer.AuthorizeTalkPublish(c.Request.Context(), TalkPublishRequest{
		NodeID: nodeID, App: body.App, SourceStream: body.Stream,
		PublishToken: params.Get("token"), PublishID: body.ID,
	})
	if err != nil || !allowed {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "talk publish denied"})
		return
	}
	hookOK(c)
}

func (h *HookController) talkDependencies() (NodeUUIDResolver, TalkPublishAuthorizer, TalkStreamObserver) {
	h.talkMu.RLock()
	defer h.talkMu.RUnlock()
	return h.talkResolver, h.talkAuthorizer, h.talkObserver
}

type onPlayBody struct {
	App           string `json:"app"`
	Stream        string `json:"stream"`
	Schema        string `json:"schema"`
	Params        string `json:"params"`
	MediaServerID string `json:"mediaServerId"`
	IP            string `json:"ip"`
}

type onStreamNotFoundBody struct {
	MediaServerID string `json:"mediaServerId"`
	VHost         string `json:"vhost"`
	App           string `json:"app"`
	Schema        string `json:"schema"`
	Stream        string `json:"stream"`
	Params        string `json:"params"`
}

func (h *HookController) OnStreamNotFound(c *gin.Context) {
	resolver, validator, dispatcher, settingsProvider := h.autoOnDemandDependencies()
	if resolver == nil || validator == nil || dispatcher == nil || settingsProvider == nil || !dispatcher.Available() {
		h.denyAutoOnDemand(c, "runtime-unavailable")
		return
	}
	if h.autoLimiter == nil || !h.autoLimiter.Allow() {
		h.denyAutoOnDemand(c, "global-rate-limited")
		return
	}
	peerIP, ok := parsePeerIP(c.Request.RemoteAddr)
	if !ok {
		h.denyAutoOnDemand(c, "source-invalid")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var body onStreamNotFoundBody
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&body); err != nil {
		h.denyAutoOnDemand(c, "payload-invalid")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		h.denyAutoOnDemand(c, "payload-invalid")
		return
	}
	deviceID, channelID, err := play.ParseFixedStreamID(body.Stream)
	if body.MediaServerID == "" || body.VHost != "__defaultVhost__" || body.App != "rtp" ||
		!validAutoOnDemandSchema(body.Schema) || err != nil {
		h.denyAutoOnDemand(c, "media-scope-invalid")
		return
	}
	settings := settingsProvider()
	if !settings.FixedAddressEnabled || !settings.AutoOnDemandEnabled {
		h.denyAutoOnDemand(c, "feature-disabled")
		return
	}
	mediaNode, ok := resolver.ResolveAutoOnDemandNode(body.MediaServerID)
	if !ok || mediaNode == nil || mediaNode.ID == 0 || !mediaNode.IsActive() || mediaNode.IsNearCapacity() ||
		mediaNode.MediaServerUUID != body.MediaServerID {
		h.denyAutoOnDemand(c, "node-unavailable")
		return
	}
	if !sourceIPMatchesNode(peerIP, mediaNode.Host) {
		h.denyAutoOnDemand(c, "source-mismatch")
		return
	}
	capability, ok := singleValue(c.Request.URL.Query(), "cap")
	if !ok || !playauth.VerifyCallbackCapability(mediaNode.APISecret, body.MediaServerID, capability) {
		h.denyAutoOnDemand(c, "callback-auth-invalid")
		return
	}
	params, err := url.ParseQuery(strings.TrimPrefix(body.Params, "?"))
	if err != nil {
		h.denyAutoOnDemand(c, "params-invalid")
		return
	}
	playToken, ok := singleValue(params, playauth.QueryParameter)
	if !ok || !h.verifyPlayToken(playToken, playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: body.App,
		Stream: body.Stream, MediaServerID: body.MediaServerID,
	}) {
		h.denyAutoOnDemand(c, "play-auth-invalid")
		return
	}
	validateCtx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	err = validator.ValidateAutoOnDemandTarget(validateCtx, deviceID, channelID)
	cancel()
	if err != nil {
		h.denyAutoOnDemand(c, "target-invalid")
		return
	}
	if err := dispatcher.Submit(play.Request{
		DeviceID: deviceID, ChannelID: channelID,
		Trigger: "on_stream_not_found", RequiredNode: mediaNode.ID,
	}); err != nil {
		h.denyAutoOnDemand(c, autoOnDemandAdmissionReason(err))
		return
	}
	if app.ZapLog != nil {
		app.ZapLog.Info("自动点播 Hook 已接收",
			zap.String("reason", "accepted"),
			zap.String("deviceId", deviceID),
			zap.String("channelId", channelID),
			zap.Int64("nodeId", mediaNode.ID))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "close": false})
}

// OnPlay authorizes every real-time rtp pull when playback authorization is
// enabled. Non-live applications retain their existing behavior.
func (h *HookController) OnPlay(c *gin.Context) {
	var body onPlayBody
	if err := c.ShouldBindJSON(&body); err != nil {
		hookDenied(c, "invalid playback request")
		return
	}
	if body.App != "rtp" {
		hookOK(c)
		return
	}
	settings := gbconfig.CurrentPlayAuthSettings()
	if !settings.Enabled {
		hookOK(c)
		return
	}
	if body.Stream == "" || body.MediaServerID == "" {
		hookDenied(c, "playback authorization denied")
		return
	}

	h.playAuthMu.RLock()
	authorizer := h.playAuthorizer
	resolver := h.playResolver
	h.playAuthMu.RUnlock()
	if authorizer == nil || resolver == nil {
		hookDenied(c, "playback authorization unavailable")
		return
	}
	params, err := url.ParseQuery(strings.TrimPrefix(body.Params, "?"))
	if err != nil {
		hookDenied(c, "invalid playback authorization")
		return
	}
	binding, err := resolver.ResolvePlaybackMediaContext(body.App, body.Stream, body.MediaServerID)
	if err != nil {
		hookDenied(c, "playback authorization denied")
		return
	}
	binding.BindClientIP = settings.BindClientIP
	binding.ClientIP = body.IP
	playToken, ok := singleValue(params, playauth.QueryParameter)
	if !ok {
		hookDenied(c, "invalid playback authorization")
		return
	}
	_, err = authorizer.Verify(playToken, binding)
	if err != nil {
		hookDenied(c, "playback authorization denied")
		return
	}
	hookOK(c)
}

func hookDenied(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{"code": -1, "msg": message})
}

func (h *HookController) autoOnDemandDependencies() (
	AutoOnDemandNodeResolver,
	AutoOnDemandTargetValidator,
	AutoOnDemandDispatcher,
	AutoOnDemandSettingsProvider,
) {
	h.autoMu.RLock()
	defer h.autoMu.RUnlock()
	return h.autoResolver, h.autoValidator, h.autoDispatcher, h.autoSettings
}

func (h *HookController) verifyPlayToken(token string, binding playauth.Binding) bool {
	h.playAuthMu.RLock()
	authorizer := h.playAuthorizer
	h.playAuthMu.RUnlock()
	if authorizer == nil {
		return false
	}
	_, err := authorizer.Verify(token, binding)
	return err == nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func singleValue(values url.Values, key string) (string, bool) {
	items, ok := values[key]
	returnValue := ""
	if ok && len(items) == 1 {
		returnValue = items[0]
	}
	return returnValue, ok && len(items) == 1 && returnValue != ""
}

func parsePeerIP(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func sourceIPMatchesNode(peerIP netip.Addr, nodeHost string) bool {
	nodeIP, err := netip.ParseAddr(strings.TrimSpace(nodeHost))
	return err == nil && nodeIP.Unmap() == peerIP
}

func validAutoOnDemandSchema(schema string) bool {
	switch strings.ToLower(strings.TrimSpace(schema)) {
	case "fmp4", "http", "https", "ws", "wss", "rtsp", "rtsps", "rtmp", "rtmps", "webrtc":
		return true
	default:
		return false
	}
}

func (h *HookController) denyAutoOnDemand(c *gin.Context, reason string) {
	if app.ZapLog != nil {
		app.ZapLog.Debug("自动点播 Hook 已拒绝", zap.String("reason", reason))
	}
	hookDenied(c, "automatic playback unavailable")
}

func autoOnDemandAdmissionReason(err error) string {
	switch {
	case errors.Is(err, play.ErrAutoStartStopped):
		return "dispatcher-stopped"
	case errors.Is(err, play.ErrAutoStartQueueFull):
		return "queue-full"
	case errors.Is(err, play.ErrAutoStartNodeRateLimited):
		return "node-rate-limited"
	case errors.Is(err, play.ErrAutoStartInvalidRequest):
		return "admission-invalid"
	default:
		return "admission-failed"
	}
}

// OnServerStarted ZLM 启动事件
func (h *HookController) OnServerStarted(c *gin.Context) {
	app.ZapLog.Info("ZLM Hook on_server_started")
	hookOK(c)
}

type onRecordMP4Body struct {
	MediaServerID string  `json:"mediaServerId" binding:"required"`
	VHost         string  `json:"vhost" binding:"required"`
	App           string  `json:"app" binding:"required"`
	Stream        string  `json:"stream" binding:"required"`
	StartTime     int64   `json:"start_time" binding:"required"`
	FileSize      uint64  `json:"file_size"`
	TimeLen       float64 `json:"time_len"`
	FilePath      string  `json:"file_path" binding:"required"`
	FileName      string  `json:"file_name" binding:"required"`
	Folder        string  `json:"folder"`
	URL           string  `json:"url"`
}

func (h *HookController) OnRecordMP4(c *gin.Context) {
	var body onRecordMP4Body
	if err := c.ShouldBindJSON(&body); err != nil || body.StartTime <= 0 || body.TimeLen < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "invalid on_record_mp4 payload"})
		return
	}
	if h.recordResolver == nil || h.recordMP4 == nil {
		hookOK(c)
		return
	}
	nodeID, ok := h.recordResolver.IDForUUID(body.MediaServerID)
	if !ok {
		app.ZapLog.Warn("忽略未知 ZLM 节点的录像文件", zap.String("mediaServerId", body.MediaServerID))
		hookOK(c)
		return
	}
	indexed, err := h.recordMP4.IndexRecordMP4(c.Request.Context(), nodeID, recording.RecordMP4Event{
		VHost: body.VHost, App: body.App, Stream: body.Stream,
		FileName: body.FileName, FilePath: body.FilePath, Folder: body.Folder, URL: body.URL,
		StartTime: time.Unix(body.StartTime, 0), TimeLen: body.TimeLen, FileSize: body.FileSize,
	})
	if err != nil {
		app.ZapLog.Error("写入 ZLM 录像文件索引失败", zap.Error(err), zap.Int64("nodeId", nodeID))
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "persist recording file failed"})
		return
	}
	if !indexed {
		app.ZapLog.Warn("忽略无法归属的 ZLM MP4 文件", zap.Int64("nodeId", nodeID))
	}
	hookOK(c)
}

// OnServerKeepalive ZLM 节点心跳(每 hook.alive_interval 秒一次,默认 30s)
//
// payload 含 mediaServerId + data{MediaSource, Session, NetThreadLoad[], WorkThreadLoad[], ...}
// 转交给 heartbeat.Collector 解析、写入 node.Registry 内存表。
//
// 失败处理(JSON 坏 / 未知 uuid / collector 未装配):log warn,仍返回 hookOK
// — ZLM 收到非 0 会重试甚至打 onException,业务侧别让心跳路径打扰它。
func (h *HookController) OnServerKeepalive(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		app.ZapLog.Warn("ZLM Hook on_server_keepalive 读 body 失败", zap.Error(err))
		hookOK(c)
		return
	}
	if h.collector == nil {
		app.ZapLog.Debug("ZLM Hook on_server_keepalive 收到但 Collector 未装配,忽略")
		hookOK(c)
		return
	}
	if err := h.collector.Receive(body); err != nil {
		app.ZapLog.Warn("ZLM Hook on_server_keepalive 处理失败",
			zap.Error(err), zap.Int("bodyLen", len(body)))
	}
	hookOK(c)
}

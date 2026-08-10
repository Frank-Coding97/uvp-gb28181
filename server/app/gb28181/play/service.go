package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// ZLM ZLM 客户端能力(便于测试 mock)
type ZLM interface {
	OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int, onlyTrack int) (*zlm.OpenRtpServerResult, error)
	CloseRtpServer(ctx context.Context, streamID string) error
	IsMediaOnline(ctx context.Context, app, stream string) (bool, error)
}

// NodePicker 节点选择能力(由 scheduler.Manager 实现,便于测试)
type NodePicker interface {
	Pick(ctx context.Context, inv PickContext) (*node.Node, error)
}

// PickContext 给 scheduler 的上下文(避免直接依赖 scheduler.InviteContext 反向引用)
type PickContext struct {
	DeviceID  string
	ChannelID string
	StreamID  string
}

// NodeLookup 按 ID 取节点(由 node.Registry 实现)
type NodeLookup interface {
	Get(id int64) (*node.Node, bool)
	// ListActive 返回所有活跃节点(用于流复用检测时,LocationMap 无 binding 的兜底探测)
	ListActive() []*node.Node
}

// LocationStore 流位置表(由 stream.LocationMap 实现)
type LocationStore interface {
	Bind(streamID string, nodeID int64)
	Lookup(streamID string) (int64, bool)
	Unbind(streamID string)
}

// Inviter 平台主叫能力(便于测试 mock)
type Inviter interface {
	Invite(ctx context.Context, m *uac.SessionManager, s *uac.Session, sdpBody string) error
	Bye(ctx context.Context, m *uac.SessionManager, streamID string) error
}

// ChannelRepo 通道查询(便于测试 mock)
type ChannelRepo interface {
	FindChannel(ctx context.Context, deviceID, channelID string) (*gbmodels.GbChannel, error)
	FindChannelByStream(ctx context.Context, streamID string) (*gbmodels.GbChannel, error)
	UpdateStream(ctx context.Context, deviceID, channelID, streamID string) error
	ClearStream(ctx context.Context, streamID string) error
	SetCurrent(ctx context.Context, deviceID, channelID, streamID, ssrc string) error
	ClearIfCurrent(ctx context.Context, streamID, ssrc string) (bool, error)
}

// DeviceRepo 设备查询(便于测试 mock)
type DeviceRepo interface {
	FindByDeviceID(ctx context.Context, deviceID string) (*gbmodels.GbDevice, error)
}

// Result 点播结果
type Result struct {
	StreamID        string       `json:"streamId"` // ZLM stream id(也是会话主键)
	SSRC            string       `json:"ssrc"`     // 媒体流 SSRC
	App             string       `json:"app"`      // ZLM app(固定 rtp)
	Reused          bool         `json:"reused"`
	Status          string       `json:"status"`
	Node            *ResultNode  `json:"node"`
	URLs            PlaybackURLs `json:"urls"`
	URLWarnings     []string     `json:"urlWarnings"`
	WSFlvURL        string       `json:"wsflvUrl"`   // ws-flv 播放地址(前端 avplayer 用)
	HLSURL          string       `json:"hlsUrl"`     // HLS 备用
	HTTPFlvURL      string       `json:"httpFlvUrl"` // http-flv 备用
	DefaultProtocol string       `json:"defaultProtocol"`
	Protocol        string       `json:"protocol"`
	URL             string       `json:"url"`
	ZLMWebRTC       bool         `json:"zlmWebrtc"`
	ExpireAt        int64        `json:"expireAt"` // 预计无人观看断流时刻(秒,UTC)
}

type ResultNode struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// 常量
const (
	zlmApp           = "rtp"
	defaultReadyWait = 8 * time.Second
	defaultPollEvery = 200 * time.Millisecond
)

// 错误
var (
	ErrDeviceNotFound  = errors.New("设备不存在")
	ErrDeviceOffline   = errors.New("设备离线")
	ErrChannelNotFound = errors.New("通道不存在")
	ErrStreamNotReady  = errors.New("流就绪等待超时")
	ErrPlayTimeout     = errors.New("点播总超时")
)

// Service 点播 service:串联 ZLM 收流 + UAC INVITE + 流就绪等待
//
// 两种模式:
//  1. 单节点(deprecated):s.zlm 单 client,M1/兼容路径
//  2. 多节点:s.picker.Pick → node → s.locationMap.Bind,Stop 时 Lookup 找回 node
//
// 模式自动:NewWithScheduler 装载多节点;New 走单节点。
type Service struct {
	cfg      gbconfig.Config
	zlm      ZLM // 单节点 client,deprecated 路径用
	inviter  Inviter
	sessions *uac.SessionManager
	notifier *stream.Notifier
	devices  DeviceRepo
	channels ChannelRepo

	// 多节点 — 都为 nil 表示走 deprecated 单节点路径
	picker      NodePicker
	registry    NodeLookup
	locationMap LocationStore

	readyWait time.Duration // 流就绪等待上限,默认 8s
	pollEvery time.Duration // 轮询间隔,默认 200ms

	snapshotSvc SnapshotService // 通道快照(播放触发),可为 nil
	urlResolver *URLResolver
}

// SnapshotService 通道快照能力(播放成功后 fire-and-forget 抓帧)
//
// 由 snapshot.Service 实现;为 nil 时 Start 静默跳过,主链路零影响。
type SnapshotService interface {
	FireAfterPlay(ctx context.Context, nodeID, streamID, deviceID, channelID string)
}

// Option 装配可选能力
type Option func(*Service)

// WithSnapshotService 注入通道快照服务
func WithSnapshotService(svc SnapshotService) Option {
	return func(s *Service) { s.snapshotSvc = svc }
}

func WithURLResolver(resolver *URLResolver) Option {
	return func(s *Service) { s.urlResolver = resolver }
}

// New 创建 service(deprecated 单节点路径,M1/test fixture 兼容)
//
// Deprecated: M2 起新代码用 NewWithScheduler,这里保留兼容旧 service_test 不退化。
func New(cfg gbconfig.Config, z ZLM, inv Inviter, sm *uac.SessionManager, n *stream.Notifier,
	devices DeviceRepo, channels ChannelRepo) *Service {
	return &Service{
		cfg: cfg, zlm: z, inviter: inv, sessions: sm, notifier: n,
		devices: devices, channels: channels,
		readyWait: defaultReadyWait, pollEvery: defaultPollEvery,
	}
}

// NewWithScheduler 创建多节点版 service
//
// picker / registry / locationMap 必须都非 nil;否则退化到单节点 New。
// opts 允许附加可选能力(如通道快照 WithSnapshotService)。
func NewWithScheduler(cfg gbconfig.Config, picker NodePicker, registry NodeLookup, locationMap LocationStore,
	inv Inviter, sm *uac.SessionManager, n *stream.Notifier,
	devices DeviceRepo, channels ChannelRepo, opts ...Option) *Service {
	s := &Service{
		cfg: cfg, inviter: inv, sessions: sm, notifier: n,
		devices: devices, channels: channels,
		picker: picker, registry: registry, locationMap: locationMap,
		readyWait: defaultReadyWait, pollEvery: defaultPollEvery,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// useMultiNode 是否走多节点路径
func (s *Service) useMultiNode() bool {
	return s.picker != nil && s.registry != nil && s.locationMap != nil
}

// clientForStream 返回操作 streamID 对应节点的 ZLM client。
// 多节点路径:LocationMap.Lookup → registry.Get → NewClientForNode
// 单节点路径:s.zlm
func (s *Service) clientForStream(streamID string) (ZLM, error) {
	if !s.useMultiNode() {
		return s.zlm, nil
	}
	nodeID, ok := s.locationMap.Lookup(streamID)
	if !ok {
		return nil, fmt.Errorf("stream %s not bound to any node", streamID)
	}
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, fmt.Errorf("stream %s bound to node %d but node not found", streamID, nodeID)
	}
	return zlm.NewClientForNode(n), nil
}

// SetReadyTimings 给测试调小等待
func (s *Service) SetReadyTimings(wait, poll time.Duration) { s.readyWait, s.pollEvery = wait, poll }

// tryReuseStream 尝试复用通道现有流。
//
// 返回值:
//
//	(nil, nil)  → 无法复用(流确实不在),调用方应清理残留后重新 INVITE
//	(res, nil)  → 复用成功,直接返回给客户端
//	(nil, err)  → 探测异常(不做清理,避免误杀正在播放的流)
//
// 探测策略:
//  1. LocationMap 有 binding → 用绑定节点探测
//  2. LocationMap 无 binding(多节点内存丢失等) → 遍历所有活跃节点探测并兜底 Bind
//  3. 任一节点确认流在线 → 复用
//  4. 所有节点都返回"不在线"→ 才判定流不存在
func (s *Service) tryReuseStream(ctx context.Context, ch *gbmodels.GbChannel) (*Result, error) {
	streamID := ch.StreamID
	if streamID == "" {
		return nil, nil
	}

	// 单节点路径:直接用 s.zlm 探测
	if !s.useMultiNode() {
		online, err := s.zlm.IsMediaOnline(ctx, zlmApp, streamID)
		if err != nil {
			app.ZapLog.Warn("流复用探测失败(单节点)",
				zap.String("streamId", streamID), zap.Error(err))
			return nil, nil // 探测失败保守视为流不在,但记 warn
		}
		if !online {
			return nil, nil
		}
		return s.buildReuseResult(ctx, streamID, nil), nil
	}

	// 多节点路径:优先看 LocationMap
	if nodeID, ok := s.locationMap.Lookup(streamID); ok {
		if n, ok := s.registry.Get(nodeID); ok {
			client := zlm.NewClientForNode(n)
			online, err := client.IsMediaOnline(ctx, zlmApp, streamID)
			if err == nil && online {
				return s.buildReuseResult(ctx, streamID, n), nil
			}
			app.ZapLog.Info("流复用绑定节点探测未在线",
				zap.String("streamId", streamID),
				zap.Int64("nodeId", nodeID),
				zap.Bool("online", online),
				zap.Error(err))
			// 绑定节点上不在,不代表流真消失(比如 hook 尚未处理完毕);
			// 但绝大多数情况绑定节点就是流的唯一节点,直接判为不在。
			return nil, nil
		}
		app.ZapLog.Warn("流复用绑定节点不存在于 registry",
			zap.String("streamId", streamID), zap.Int64("nodeId", nodeID))
	}

	// LocationMap 无 binding(多节点内存丢失/hook 兜底 Bind 尚未到达)
	// 遍历所有活跃节点探测,任一命中即复用并兜底 Bind
	activeNodes := s.registry.ListActive()
	app.ZapLog.Info("流复用兜底探测(LocationMap 无 binding)",
		zap.String("streamId", streamID),
		zap.Int("activeNodes", len(activeNodes)))
	for _, n := range activeNodes {
		client := zlm.NewClientForNode(n)
		online, err := client.IsMediaOnline(ctx, zlmApp, streamID)
		if err != nil {
			app.ZapLog.Debug("流复用兜底探测单节点失败",
				zap.String("streamId", streamID),
				zap.Int64("nodeId", n.ID),
				zap.Error(err))
			continue
		}
		if online {
			// 找到了,兜底 Bind 回 LocationMap 恢复元数据
			s.locationMap.Bind(streamID, n.ID)
			app.ZapLog.Info("流复用兜底探测命中,恢复 LocationMap",
				zap.String("streamId", streamID),
				zap.Int64("nodeId", n.ID))
			return s.buildReuseResult(ctx, streamID, n), nil
		}
	}
	return nil, nil
}

// buildReuseResult 构造复用返回值(从 session 取 SSRC,取不到用 streamID 兜底)
func (s *Service) buildReuseResult(ctx context.Context, streamID string, mediaNode *node.Node) *Result {
	ssrc := streamID
	if sess := s.sessions.Get(streamID); sess != nil {
		ssrc = sess.SSRC
	}
	if mediaNode != nil {
		return s.buildNodeResult(ctx, streamID, ssrc, mediaNode, true)
	}
	result := s.buildResultFor(streamID, ssrc, s.cfg.ZLM.EffectivePlaybackHost())
	result.Reused = true
	return result
}

// Start 发起点播
// 0) 检查通道是否已在播放，在线则直接返回现有地址
// 1) 校验设备/通道  2) Pick 节点 + openRtpServer + Bind  3) 构造 SDP+SSRC
// 4) UAC INVITE  5) WaitReady  6) 返地址
// 任一中断都会回滚已开的 RTP 端口 + Unbind LocationMap
func (s *Service) Start(ctx context.Context, deviceID, channelID string) (*Result, error) {
	// 1. 校验设备 + 通道
	dev, err := s.devices.FindByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("查设备失败: %w", err)
	}
	if dev == nil {
		return nil, ErrDeviceNotFound
	}
	if dev.Status != gbmodels.DeviceStatusOnline {
		return nil, ErrDeviceOffline
	}
	ch, err := s.channels.FindChannel(ctx, deviceID, channelID)
	if err != nil {
		return nil, fmt.Errorf("查通道失败: %w", err)
	}
	if ch == nil {
		return nil, ErrChannelNotFound
	}

	// 2. 检查通道是否已在播放 —— 尝试流复用
	//
	// 策略要点(避免误杀正在播放的流):
	//   - IsMediaOnline 返回 true → 复用现有流,直接返回地址
	//   - IsMediaOnline 明确返回 false 且 clientForStream 成功 → 流确实不在,清理残留
	//   - clientForStream 失败(多节点 LocationMap 丢失 binding)或 IsMediaOnline 报错 →
	//     不主动清理,尝试从 registry 里遍历所有节点探测 (兜底恢复 LocationMap)
	//   - 所有节点都探测失败 → 才走清理路径(此时说明流真的不在了)
	if ch.StreamID != "" {
		if reused, err := s.tryReuseStream(ctx, ch); err != nil {
			return nil, err
		} else if reused != nil {
			app.ZapLog.Info("点播复用现有流",
				zap.String("deviceId", deviceID),
				zap.String("channelId", channelID),
				zap.String("streamId", ch.StreamID))
			return reused, nil
		}
		// 复用失败(流确实不在),清理残留后走完整 INVITE 流程
		app.ZapLog.Info("点播复用失败,清理残留后重新 INVITE",
			zap.String("deviceId", deviceID),
			zap.String("channelId", channelID),
			zap.String("staleStreamId", ch.StreamID))
		_ = s.Stop(context.Background(), ch.StreamID)
	}

	// 3. 生成 SSRC + StreamID(stream_id = ssrc,简化映射)
	ssrc := sdp.GenRealtimeSSRC(s.cfg.SIP.Domain)
	streamID := ssrc

	// 4. 多节点路径:Pick + Bind;单节点路径:直接走 s.zlm
	var client ZLM
	var recvHost string
	var rtpFallback int
	var pickedNodeID int64 // 通道快照要按 nodeID 拿 ZLM 端口配置 —— 单节点路径为 0
	var pickedNode *node.Node

	if s.useMultiNode() {
		selectedNode, err := s.picker.Pick(ctx, PickContext{
			DeviceID: deviceID, ChannelID: channelID, StreamID: streamID,
		})
		if err != nil {
			return nil, fmt.Errorf("无可用 ZLM 节点: %w", err)
		}
		client = zlm.NewClientForNode(selectedNode)
		recvHost = selectedNode.EffectiveReceiveHost()
		rtpFallback = selectedNode.RTPPortStart // 兜底端口
		pickedNodeID = selectedNode.ID
		pickedNode = selectedNode
		s.locationMap.Bind(streamID, selectedNode.ID)
	} else {
		// deprecated 单节点路径
		client = s.zlm
		recvHost = s.cfg.ZLM.EffectiveReceiveHost()
		rtpFallback = s.cfg.ZLM.RTPPort
	}

	// 5. openRtpServer:port=0 让 ZLM 自选临时端口
	onlyTrack := 2 // ZLM: 2=仅视频,关闭音频
	if ch.AudioEnabled {
		onlyTrack = 0 // ZLM: 0=音视频
	}
	rtpRes, err := client.OpenRtpServer(ctx, streamID, 0, 0, onlyTrack)
	if err != nil {
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		return nil, fmt.Errorf("申请 ZLM 收流端口失败: %w", err)
	}
	recvPort := rtpRes.Port
	if recvPort == 0 {
		recvPort = rtpFallback
	}

	// 6. 构造 SDP + 发 INVITE(任何失败要回滚 RTP 端口 + Unbind)
	body := sdp.BuildPlaySDP(sdp.PlayParams{
		ServerID: s.cfg.SIP.ServerID,
		RecvIP:   recvHost,
		RecvPort: recvPort,
		SSRC:     ssrc,
		Extended: gbconfig.SDPExtensionEnabled(),
	})

	sess := &uac.Session{
		DeviceID:  deviceID,
		ChannelID: channelID,
		SSRC:      ssrc,
		StreamID:  streamID,
		Dest:      fmt.Sprintf("%s:%d", dev.IP, dev.Port),
		Transport: dev.Transport,
	}

	playCtx, playCancel := context.WithTimeout(ctx, gbconfig.CurrentPlaybackSettings().PlayTimeout())
	defer playCancel()
	inviteCtx, inviteCancel := context.WithTimeout(playCtx, gbconfig.SIPCommandTimeout())
	defer inviteCancel()
	if err := s.inviter.Invite(inviteCtx, s.sessions, sess, body); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if errors.Is(playCtx.Err(), context.DeadlineExceeded) {
			_ = s.inviter.Bye(cleanupCtx, s.sessions, streamID)
		}
		_ = client.CloseRtpServer(cleanupCtx, streamID)
		cleanupCancel()
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		if errors.Is(playCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrPlayTimeout, err)
		}
		return nil, fmt.Errorf("发 INVITE 失败: %w", err)
	}

	// 7. WaitReady:hook + 轮询双源(ADR-002 创新 3)
	readyCtx, readyCancel := context.WithTimeout(playCtx, s.readyWait)
	defer readyCancel()
	poll := func(ctx context.Context) (bool, error) {
		return client.IsMediaOnline(ctx, zlmApp, streamID)
	}
	if err := stream.WaitReady(readyCtx, s.notifier, streamID, poll, s.pollEvery); err != nil {
		// 流没就绪:发 BYE + 关 RTP 端口 + Unbind
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.inviter.Bye(byeCtx, s.sessions, streamID)
		_ = client.CloseRtpServer(byeCtx, streamID)
		byeCancel()
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		if errors.Is(playCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", ErrPlayTimeout, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrStreamNotReady, err)
	}
	if err := s.channels.UpdateStream(ctx, deviceID, channelID, streamID); err != nil {
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.inviter.Bye(byeCtx, s.sessions, streamID)
		_ = client.CloseRtpServer(byeCtx, streamID)
		byeCancel()
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		return nil, fmt.Errorf("记录通道播放流失败: %w", err)
	}

	// 8. 通道快照(fire-and-forget,不阻塞返回,不影响主链路)
	if s.snapshotSvc != nil {
		nodeIDStr := ""
		if pickedNodeID != 0 {
			nodeIDStr = fmt.Sprintf("%d", pickedNodeID)
		}
		s.snapshotSvc.FireAfterPlay(context.Background(), nodeIDStr, streamID, deviceID, channelID)
	}

	// 9. 生成播放地址(多节点用 picked node 的 host;单节点用 cfg.ZLM.Host)
	var result *Result
	if pickedNode != nil {
		result = s.buildNodeResult(ctx, streamID, ssrc, pickedNode, false)
	} else {
		result = s.buildResultFor(streamID, ssrc, s.cfg.ZLM.EffectivePlaybackHost())
	}
	return result, nil
}

// Stop 停播:发 BYE + 关 RTP 端口 + Unbind LocationMap
func (s *Service) Stop(ctx context.Context, streamID string) error {
	byeErr := s.inviter.Bye(ctx, s.sessions, streamID)

	client, clientErr := s.clientForStream(streamID)
	var closeErr error
	if clientErr != nil {
		// 多节点路径找不到 client(LocationMap 没记录,可能已被清理)
		// 单节点不会进这里,只 log;失败不阻塞 BYE
		closeErr = clientErr
	} else {
		closeErr = client.CloseRtpServer(ctx, streamID)
	}

	// Unbind 总要做(即便 Close 失败,避免 streamID 永远占位)
	if s.useMultiNode() {
		s.locationMap.Unbind(streamID)
	}
	clearErr := s.channels.ClearStream(ctx, streamID)

	if byeErr != nil {
		return byeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return clearErr
}

// ShouldCloseOnNoneReader 返回通道无人观看时是否应关闭上行流。
// 找不到通道时沿用历史默认行为,避免残留流长期占用设备和 RTP 资源。
func (s *Service) ShouldCloseOnNoneReader(ctx context.Context, streamID string) (bool, error) {
	ch, err := s.channels.FindChannelByStream(ctx, streamID)
	if err != nil {
		return true, err
	}
	if ch == nil {
		return true, nil
	}
	return ch.OnDemandLive && !ch.CloudRecordingEnabled, nil
}

// buildResultFor 构造播放地址,host 由 Start 传(多节点路径取选中 node host,单节点取 cfg.ZLM.Host)
//
// 简化:ws-flv 走 ZLM 默认 http 端口(80)即可;HTTPPort 是 API 端口不是 web 服务端口,
// 多节点场景假设 web 端口也是 80(M3 可加 Node.WebPort 字段)。
func (s *Service) buildResultFor(streamID, ssrc, host string) *Result {
	port := s.cfg.ZLM.HTTPPort
	base := fmt.Sprintf("%s:%d/%s/%s", host, port, zlmApp, streamID)
	urls := PlaybackURLs{
		WSFLV: stringPtr("ws://" + base + ".live.flv"), HTTPFLV: stringPtr("http://" + base + ".live.flv"),
		HLS: stringPtr("http://" + base + "/hls.m3u8"),
	}
	result := &Result{
		StreamID:   streamID,
		SSRC:       ssrc,
		App:        zlmApp,
		Status:     "online",
		URLs:       urls,
		WSFlvURL:   "ws://" + base + ".live.flv",
		HTTPFlvURL: "http://" + base + ".live.flv",
		HLSURL:     "http://" + base + "/hls.m3u8",
		ExpireAt:   time.Now().Add(time.Duration(s.cfg.Media.StreamNoneReaderTimeout) * time.Second).Unix(),
	}
	ApplyPlaybackSelection(result, gbconfig.CurrentDefaultPlaybackProtocol(), false)
	return result
}

func (s *Service) buildNodeResult(ctx context.Context, streamID, ssrc string, mediaNode *node.Node, reused bool) *Result {
	urls, warnings := s.urlResolver.Resolve(ctx, mediaNode, zlmApp, streamID)
	result := &Result{
		StreamID:    streamID,
		SSRC:        ssrc,
		App:         zlmApp,
		Reused:      reused,
		Status:      "online",
		Node:        &ResultNode{ID: mediaNode.ID, Name: mediaNode.Name, Host: mediaNode.Host},
		URLs:        urls,
		URLWarnings: warnings,
		ExpireAt:    time.Now().Add(time.Duration(s.cfg.Media.StreamNoneReaderTimeout) * time.Second).Unix(),
	}
	if urls.WSFLV != nil {
		result.WSFlvURL = *urls.WSFLV
	}
	if urls.HTTPFLV != nil {
		result.HTTPFlvURL = *urls.HTTPFLV
	}
	if urls.HLS != nil {
		result.HLSURL = *urls.HLS
	}
	ApplyPlaybackSelection(result, gbconfig.CurrentDefaultPlaybackProtocol(), false)
	return result
}

func ApplyPlaybackSelection(result *Result, preferred string, secure bool) {
	if result == nil {
		return
	}
	if result.DefaultProtocol == "" {
		result.DefaultProtocol = preferred
	}
	selected := SelectPlaybackSource(result.URLs, result.DefaultProtocol, secure)
	result.Protocol, result.URL, result.ZLMWebRTC = selected.Protocol, selected.URL, selected.ZLMWebRTC
}

// buildResult 旧版,deprecated 单节点路径用
//
// Deprecated: 走 buildResultFor。仅保留用于 service_test 旧用例(若有)。
func (s *Service) buildResult(streamID, ssrc string) *Result {
	return s.buildResultFor(streamID, ssrc, s.cfg.ZLM.EffectivePlaybackHost())
}

// sessions 暴露给 hook 端点(on_stream_none_reader / on_rtp_server_timeout 用)
var _ = sync.Mutex{}

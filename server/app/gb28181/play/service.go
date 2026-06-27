package play

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ZLM ZLM 客户端能力(便于测试 mock)
type ZLM interface {
	OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int) (*zlm.OpenRtpServerResult, error)
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
}

// DeviceRepo 设备查询(便于测试 mock)
type DeviceRepo interface {
	FindByDeviceID(ctx context.Context, deviceID string) (*gbmodels.GbDevice, error)
}

// Result 点播结果
type Result struct {
	StreamID  string `json:"streamId"`  // ZLM stream id(也是会话主键)
	SSRC      string `json:"ssrc"`      // 媒体流 SSRC
	App       string `json:"app"`       // ZLM app(固定 rtp)
	WSFlvURL  string `json:"wsflvUrl"`  // ws-flv 播放地址(前端 avplayer 用)
	HLSURL    string `json:"hlsUrl"`    // HLS 备用
	HTTPFlvURL string `json:"httpFlvUrl"` // http-flv 备用
	ExpireAt  int64  `json:"expireAt"`  // 预计无人观看断流时刻(秒,UTC)
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
)

// Service 点播 service:串联 ZLM 收流 + UAC INVITE + 流就绪等待
//
// 两种模式:
//  1. 单节点(deprecated):s.zlm 单 client,M1/兼容路径
//  2. 多节点:s.picker.Pick → node → s.locationMap.Bind,Stop 时 Lookup 找回 node
//
// 模式自动:NewWithScheduler 装载多节点;New 走单节点。
type Service struct {
	cfg       gbconfig.Config
	zlm       ZLM // 单节点 client,deprecated 路径用
	inviter   Inviter
	sessions  *uac.SessionManager
	notifier  *stream.Notifier
	devices   DeviceRepo
	channels  ChannelRepo

	// 多节点 — 都为 nil 表示走 deprecated 单节点路径
	picker      NodePicker
	registry    NodeLookup
	locationMap LocationStore

	readyWait time.Duration // 流就绪等待上限,默认 8s
	pollEvery time.Duration // 轮询间隔,默认 200ms
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
func NewWithScheduler(cfg gbconfig.Config, picker NodePicker, registry NodeLookup, locationMap LocationStore,
	inv Inviter, sm *uac.SessionManager, n *stream.Notifier,
	devices DeviceRepo, channels ChannelRepo) *Service {
	return &Service{
		cfg: cfg, inviter: inv, sessions: sm, notifier: n,
		devices: devices, channels: channels,
		picker: picker, registry: registry, locationMap: locationMap,
		readyWait: defaultReadyWait, pollEvery: defaultPollEvery,
	}
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

// Start 发起点播
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

	// 2. 生成 SSRC + StreamID(stream_id = ssrc,简化映射)
	ssrc := sdp.GenRealtimeSSRC(s.cfg.SIP.Domain)
	streamID := ssrc

	// 3. 多节点路径:Pick + Bind;单节点路径:直接走 s.zlm
	var client ZLM
	var recvHost string
	var rtpFallback int

	if s.useMultiNode() {
		pickedNode, err := s.picker.Pick(ctx, PickContext{
			DeviceID: deviceID, ChannelID: channelID, StreamID: streamID,
		})
		if err != nil {
			return nil, fmt.Errorf("无可用 ZLM 节点: %w", err)
		}
		client = zlm.NewClientForNode(pickedNode)
		recvHost = pickedNode.Host
		rtpFallback = pickedNode.RTPPortStart // 兜底端口
		s.locationMap.Bind(streamID, pickedNode.ID)
	} else {
		// deprecated 单节点路径
		client = s.zlm
		recvHost = s.cfg.ZLM.Host
		rtpFallback = s.cfg.ZLM.RTPPort
	}

	// 4. openRtpServer:port=0 让 ZLM 自选临时端口
	rtpRes, err := client.OpenRtpServer(ctx, streamID, 0, 0)
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

	// 5. 构造 SDP + 发 INVITE(任何失败要回滚 RTP 端口 + Unbind)
	body := sdp.BuildPlaySDP(sdp.PlayParams{
		ServerID: s.cfg.SIP.ServerID,
		RecvIP:   recvHost,
		RecvPort: recvPort,
		SSRC:     ssrc,
	})

	sess := &uac.Session{
		DeviceID:  deviceID,
		ChannelID: channelID,
		SSRC:      ssrc,
		StreamID:  streamID,
		Dest:      fmt.Sprintf("%s:%d", dev.IP, dev.Port),
	}

	inviteCtx, inviteCancel := context.WithTimeout(ctx, 5*time.Second)
	defer inviteCancel()
	if err := s.inviter.Invite(inviteCtx, s.sessions, sess, body); err != nil {
		_ = client.CloseRtpServer(context.Background(), streamID)
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		return nil, fmt.Errorf("发 INVITE 失败: %w", err)
	}

	// 6. WaitReady:hook + 轮询双源(ADR-002 创新 3)
	readyCtx, readyCancel := context.WithTimeout(ctx, s.readyWait)
	defer readyCancel()
	poll := func(ctx context.Context) (bool, error) {
		return client.IsMediaOnline(ctx, zlmApp, streamID)
	}
	if err := stream.WaitReady(readyCtx, s.notifier, streamID, poll, s.pollEvery); err != nil {
		// 流没就绪:发 BYE + 关 RTP 端口 + Unbind
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer byeCancel()
		_ = s.inviter.Bye(byeCtx, s.sessions, streamID)
		_ = client.CloseRtpServer(context.Background(), streamID)
		if s.useMultiNode() {
			s.locationMap.Unbind(streamID)
		}
		return nil, fmt.Errorf("%w: %v", ErrStreamNotReady, err)
	}

	// 7. 生成播放地址(多节点用 picked node 的 host;单节点用 cfg.ZLM.Host)
	result := s.buildResultFor(streamID, ssrc, recvHost)
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

	if byeErr != nil {
		return byeErr
	}
	return closeErr
}

// buildResultFor 构造播放地址,host 由 Start 传(多节点路径取选中 node host,单节点取 cfg.ZLM.Host)
//
// 简化:ws-flv 走 ZLM 默认 http 端口(80)即可;HTTPPort 是 API 端口不是 web 服务端口,
// 多节点场景假设 web 端口也是 80(M3 可加 Node.WebPort 字段)。
func (s *Service) buildResultFor(streamID, ssrc, host string) *Result {
	port := s.cfg.ZLM.HTTPPort
	base := fmt.Sprintf("%s:%d/%s/%s", host, port, zlmApp, streamID)
	return &Result{
		StreamID:   streamID,
		SSRC:       ssrc,
		App:        zlmApp,
		WSFlvURL:   "ws://" + base + ".live.flv",
		HTTPFlvURL: "http://" + base + ".live.flv",
		HLSURL:     "http://" + base + "/hls.m3u8",
		ExpireAt:   time.Now().Add(time.Duration(s.cfg.Media.StreamNoneReaderTimeout) * time.Second).Unix(),
	}
}

// buildResult 旧版,deprecated 单节点路径用
//
// Deprecated: 走 buildResultFor。仅保留用于 service_test 旧用例(若有)。
func (s *Service) buildResult(streamID, ssrc string) *Result {
	return s.buildResultFor(streamID, ssrc, s.cfg.ZLM.Host)
}

// sessions 暴露给 hook 端点(on_stream_none_reader / on_rtp_server_timeout 用)
var _ = sync.Mutex{}

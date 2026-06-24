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
)

// ZLM ZLM 客户端能力(便于测试 mock)
type ZLM interface {
	OpenRtpServer(ctx context.Context, streamID string, port int, tcpMode int) (*zlm.OpenRtpServerResult, error)
	CloseRtpServer(ctx context.Context, streamID string) error
	IsMediaOnline(ctx context.Context, app, stream string) (bool, error)
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
type Service struct {
	cfg       gbconfig.Config
	zlm       ZLM
	inviter   Inviter
	sessions  *uac.SessionManager
	notifier  *stream.Notifier
	devices   DeviceRepo
	channels  ChannelRepo

	readyWait time.Duration // 流就绪等待上限,默认 8s
	pollEvery time.Duration // 轮询间隔,默认 200ms
}

// New 创建 service
func New(cfg gbconfig.Config, z ZLM, inv Inviter, sm *uac.SessionManager, n *stream.Notifier,
	devices DeviceRepo, channels ChannelRepo) *Service {
	return &Service{
		cfg: cfg, zlm: z, inviter: inv, sessions: sm, notifier: n,
		devices: devices, channels: channels,
		readyWait: defaultReadyWait, pollEvery: defaultPollEvery,
	}
}

// SetReadyTimings 给测试调小等待
func (s *Service) SetReadyTimings(wait, poll time.Duration) { s.readyWait, s.pollEvery = wait, poll }

// Start 发起点播
// 1) 校验设备/通道  2) openRtpServer  3) 构造 SDP+SSRC  4) UAC INVITE  5) WaitReady  6) 返地址
// 任一中断都会回滚已开的 RTP 端口
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

	// 3. openRtpServer(失败直接返回,无资源需回滚)
	rtpRes, err := s.zlm.OpenRtpServer(ctx, streamID, s.cfg.ZLM.RTPPort, 0)
	if err != nil {
		return nil, fmt.Errorf("申请 ZLM 收流端口失败: %w", err)
	}
	recvPort := rtpRes.Port
	if recvPort == 0 {
		recvPort = s.cfg.ZLM.RTPPort
	}

	// 4. 构造 SDP + 发 INVITE(任何失败要回滚 RTP 端口)
	body := sdp.BuildPlaySDP(sdp.PlayParams{
		ServerID: s.cfg.SIP.ServerID,
		RecvIP:   s.cfg.ZLM.Host,
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
		_ = s.zlm.CloseRtpServer(context.Background(), streamID)
		return nil, fmt.Errorf("发 INVITE 失败: %w", err)
	}

	// 5. WaitReady:hook + 轮询双源(ADR-002 创新 3)
	readyCtx, readyCancel := context.WithTimeout(ctx, s.readyWait)
	defer readyCancel()
	poll := func(ctx context.Context) (bool, error) {
		return s.zlm.IsMediaOnline(ctx, zlmApp, streamID)
	}
	if err := stream.WaitReady(readyCtx, s.notifier, streamID, poll, s.pollEvery); err != nil {
		// 流没就绪:发 BYE + 关 RTP 端口
		byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer byeCancel()
		_ = s.inviter.Bye(byeCtx, s.sessions, streamID)
		_ = s.zlm.CloseRtpServer(context.Background(), streamID)
		return nil, fmt.Errorf("%w: %v", ErrStreamNotReady, err)
	}

	// 6. 生成播放地址(zlm http 端口默认 80;ws-flv 用同端口)
	result := s.buildResult(streamID, ssrc)
	return result, nil
}

// Stop 停播:发 BYE + 关 RTP 端口
func (s *Service) Stop(ctx context.Context, streamID string) error {
	byeErr := s.inviter.Bye(ctx, s.sessions, streamID)
	closeErr := s.zlm.CloseRtpServer(ctx, streamID)
	if byeErr != nil {
		return byeErr
	}
	return closeErr
}

// buildResult 构造播放地址(暂用 ZLM 默认 http 端口 80;若与 HTTPPort 不同后续可加配置)
func (s *Service) buildResult(streamID, ssrc string) *Result {
	host := s.cfg.ZLM.Host
	// ZLM 默认 http port=80,api port 在 18080;ws-flv 走 80(HTTP)
	// 简化:直接复用 HTTPPort 暴露的 http 服务(zlm 默认开 80,但也支持 api 端口同时服务静态/流)
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

// sessions 暴露给 hook 端点(on_stream_none_reader / on_rtp_server_timeout 用)
var _ = sync.Mutex{}

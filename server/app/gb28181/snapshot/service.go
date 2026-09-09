package snapshot

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

const (
	snapshotEventPanic         = "gb28181.snapshot.panic"
	snapshotEventCaptureFailed = "gb28181.snapshot.capture_failed"
	snapshotEventCaptured      = "gb28181.snapshot.captured"
)

// ZLMClient 抓帧客户端接口(只包含 Service 需要的方法,方便单测 mock)
type ZLMClient interface {
	GetSnap(ctx context.Context, streamURL string, timeoutSec, expireSec int) ([]byte, error)
}

// GetClientFunc 按 nodeID 拿到 ZLM 客户端(多节点场景下 play.Service 已把
// streamID 绑定到具体 nodeID,快照必须调回同一节点)。
// 单节点场景传空串,实现可返回默认节点。
type GetClientFunc func(nodeID string) (ZLMClient, error)

// BuildStreamURLFunc 构造 ZLM 内部可拉流的 URL(供 getSnap 的 FFmpeg 拉流用)。
//
// 由 bootstrap 装配时注入:内部一般走 rtsp(比 http-flv 更稳),端口从 ServerConfigCache
// 按 nodeID 拉 ZLM /index/api/getServerConfig 拿到 rtsp.port 后构造。
type BuildStreamURLFunc func(ctx context.Context, nodeID, streamID, playToken string) (string, error)

// Config 装配参数
type Config struct {
	UploadRoot     string             // upload 根目录(如 ./resource/public/uploads)
	URLPrefix      string             // 前端可访问 URL 前缀(如 /uploads)
	DelayBefore    time.Duration      // 2s,给 ZLM 收流稳画面用
	ZLMTimeout     int                // ZLM 抓帧超时秒(推荐 5)
	ZLMExpire      int                // ZLM 缓存快照秒(默认 1,避免点播复用历史帧)
	GetClient      GetClientFunc      // 按 nodeID 拿 ZLM client
	BuildStreamURL BuildStreamURLFunc // 按 nodeID + streamID 拼 ZLM 内部拉流 URL
	Repo           Repo
	Logger         *zap.Logger
}

// Service 通道快照服务:播放触发,fire-and-forget 抓帧落盘 + 更新通道行
type Service struct {
	cfg Config
	log *zap.Logger
}

func detachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func (s *Service) logger(ctx context.Context) *zap.Logger {
	return logging.FromContext(ctx, s.log)
}

// New 构造 Service。cfg 各字段为 0/空时给合理默认。
func New(cfg Config) *Service {
	if cfg.DelayBefore == 0 {
		cfg.DelayBefore = 2 * time.Second
	}
	if cfg.ZLMTimeout == 0 {
		cfg.ZLMTimeout = 5
	}
	if cfg.ZLMExpire == 0 {
		cfg.ZLMExpire = 1
	}
	if cfg.URLPrefix == "" {
		cfg.URLPrefix = "/uploads"
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{
		cfg: cfg,
		log: log,
	}
}

// FireAfterPlay 由 play.Service 在新流 WaitReady 之后 fire-and-forget 调。
// 内部起 goroutine 异步抓帧,不阻塞调用方;普通抓帧失败记录 warn,真实 panic
// 记录 error 后由 defer 吞掉,不影响播放主链路。
//
// nodeID:   多节点场景传 pickedNode.ID.string(),单节点传空串
// streamID: ZLM 内部 stream 标识(通常 = ssrc)
// s 为 nil 时安全跳过(方便 play.Service 未注入 snapshot 时零改动)。
func (s *Service) FireAfterPlay(ctx context.Context, nodeID, streamID, deviceID, channelID, playToken string) {
	if s == nil {
		return
	}
	captureCtx := detachedContext(ctx)
	logger := s.logger(captureCtx)
	app.BackgroundWork.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("通道快照 goroutine panic",
					zap.String("event", snapshotEventPanic),
					zap.String("panic_type", logging.TypeName(r)),
					zap.String("stack", string(debug.Stack())),
					zap.String("device", deviceID), zap.String("channel", channelID))
			}
		}()
		if err := s.doCaptureContext(captureCtx, nodeID, streamID, deviceID, channelID, playToken); err != nil {
			logger.Warn("通道快照抓取失败",
				zap.String("event", snapshotEventCaptureFailed), logging.Error(err),
				zap.String("device", deviceID), zap.String("channel", channelID))
		}
	})
}

// doCapture 保留旧的同步测试/内部调用语义,不携带请求上下文。
func (s *Service) doCapture(nodeID, streamID, deviceID, channelID, playToken string) error {
	return s.doCaptureContext(context.Background(), nodeID, streamID, deviceID, channelID, playToken)
}

// doCaptureContext 在独立的、有界上下文中执行抓拍。父上下文只用于传递
// 不可变日志 scope,请求取消不会中断播放成功后的快照补偿。
func (s *Service) doCaptureContext(parentCtx context.Context, nodeID, streamID, deviceID, channelID, playToken string) error {
	// 延迟 2s,给 ZLM 收流稳画面
	time.Sleep(s.cfg.DelayBefore)

	if s.cfg.GetClient == nil {
		return fmt.Errorf("snapshot: GetClient 未配置")
	}
	if s.cfg.BuildStreamURL == nil {
		return fmt.Errorf("snapshot: BuildStreamURL 未配置")
	}
	client, err := s.cfg.GetClient(nodeID)
	if err != nil {
		return fmt.Errorf("拿 ZLM 客户端失败: %w", err)
	}
	if client == nil {
		return fmt.Errorf("ZLM 客户端为 nil")
	}

	// ZLM 抓帧本身超时 cfg.ZLMTimeout 秒,外层再加 2 秒兜底
	ctx, cancel := context.WithTimeout(detachedContext(parentCtx),
		time.Duration(s.cfg.ZLMTimeout+2)*time.Second)
	defer cancel()

	streamURL, err := s.cfg.BuildStreamURL(ctx, nodeID, streamID, playToken)
	if err != nil {
		return fmt.Errorf("构造流 URL 失败: %w", err)
	}

	bytes, err := client.GetSnap(ctx, streamURL, s.cfg.ZLMTimeout, s.cfg.ZLMExpire)
	if err != nil {
		return fmt.Errorf("ZLM getSnap 失败: %w", err)
	}
	if len(bytes) == 0 {
		return fmt.Errorf("ZLM 返回空图片")
	}

	now := time.Now()
	absPath, relURL := snapshotPath(s.cfg.UploadRoot, s.cfg.URLPrefix, deviceID, channelID, now)
	if err := writeSnapshotFile(absPath, bytes); err != nil {
		return fmt.Errorf("落盘失败: %w", err)
	}

	if s.cfg.Repo != nil {
		if err := s.cfg.Repo.UpdateSnapshot(detachedContext(ctx), deviceID, channelID, relURL, now); err != nil {
			// 文件已经落盘;DB 更新失败只 warn,下次抓拍会重试(覆盖式)
			return fmt.Errorf("UPDATE gb_channel 失败: %w", err)
		}
	}

	s.logger(ctx).Info("通道快照成功",
		zap.String("event", snapshotEventCaptured),
		zap.String("device", deviceID),
		zap.String("channel", channelID),
		zap.Int("bytes", len(bytes)),
		zap.String("url", relURL))
	return nil
}

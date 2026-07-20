package snapshot

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// ZLMClient 抓帧客户端接口(只包含 Service 需要的方法,方便单测 mock)
type ZLMClient interface {
	GetSnap(ctx context.Context, streamID string, timeoutSec, expireSec int) ([]byte, error)
}

// GetClientFunc 按 nodeID 拿到 ZLM 客户端(多节点场景下 play.Service 已把
// streamID 绑定到具体 nodeID,快照必须调回同一节点)
type GetClientFunc func(nodeID string) (ZLMClient, error)

// Config 装配参数
type Config struct {
	UploadRoot  string        // upload 根目录(如 ./resource/public/uploads)
	URLPrefix   string        // 前端可访问 URL 前缀(如 /uploads)
	DedupTTL    time.Duration // 30s
	DelayBefore time.Duration // 2s,给 ZLM 收流稳画面用
	ZLMTimeout  int           // ZLM 抓帧超时秒(推荐 5)
	ZLMExpire   int           // ZLM 缓存快照秒(推荐 30,跟 DedupTTL 对齐)
	GetClient   GetClientFunc
	Repo        Repo
	Logger      *zap.Logger
}

// Service 通道快照服务:播放触发,fire-and-forget 抓帧落盘 + 更新通道行
type Service struct {
	cfg   Config
	dedup *dedupCache
	log   *zap.Logger
}

// New 构造 Service。cfg 各字段为 0/空时给合理默认。
func New(cfg Config) *Service {
	if cfg.DedupTTL == 0 {
		cfg.DedupTTL = 30 * time.Second
	}
	if cfg.DelayBefore == 0 {
		cfg.DelayBefore = 2 * time.Second
	}
	if cfg.ZLMTimeout == 0 {
		cfg.ZLMTimeout = 5
	}
	if cfg.ZLMExpire == 0 {
		cfg.ZLMExpire = 30
	}
	if cfg.URLPrefix == "" {
		cfg.URLPrefix = "/uploads"
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{
		cfg:   cfg,
		dedup: newDedup(cfg.DedupTTL),
		log:   log,
	}
}

// FireAfterPlay 由 play.Service 在 WaitReady 之后 fire-and-forget 调。
// 内部起 goroutine 异步抓帧,不阻塞调用方;任何失败都是 warn 级不 panic。
//
// s 为 nil 时安全跳过(方便 play.Service 未注入 snapshot 时零改动)。
func (s *Service) FireAfterPlay(ctx context.Context, nodeID, streamID, deviceID, channelID string) {
	if s == nil {
		return
	}
	key := deviceID + ":" + channelID
	if !s.dedup.CheckAndMark(key) {
		s.log.Debug("通道快照 30s 内已抓过,复用",
			zap.String("device", deviceID), zap.String("channel", channelID))
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Warn("通道快照 goroutine panic",
					zap.Any("panic", r),
					zap.String("device", deviceID), zap.String("channel", channelID))
			}
		}()
		if err := s.doCapture(nodeID, streamID, deviceID, channelID); err != nil {
			s.log.Warn("通道快照抓取失败",
				zap.Error(err),
				zap.String("device", deviceID), zap.String("channel", channelID))
		}
	}()
}

// doCapture 独立于 ctx 跑(播放请求 ctx 会 cancel),内部走独立 context.Background
func (s *Service) doCapture(nodeID, streamID, deviceID, channelID string) error {
	// 延迟 2s,给 ZLM 收流稳画面
	time.Sleep(s.cfg.DelayBefore)

	if s.cfg.GetClient == nil {
		return fmt.Errorf("snapshot: GetClient 未配置")
	}
	client, err := s.cfg.GetClient(nodeID)
	if err != nil {
		return fmt.Errorf("拿 ZLM 客户端失败: %w", err)
	}
	if client == nil {
		return fmt.Errorf("ZLM 客户端为 nil,node=%s", nodeID)
	}

	// ZLM 抓帧本身超时 cfg.ZLMTimeout 秒,外层再加 2 秒兜底
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(s.cfg.ZLMTimeout+2)*time.Second)
	defer cancel()

	bytes, err := client.GetSnap(ctx, streamID, s.cfg.ZLMTimeout, s.cfg.ZLMExpire)
	if err != nil {
		return fmt.Errorf("ZLM getSnap 失败: %w", err)
	}
	if len(bytes) == 0 {
		return fmt.Errorf("ZLM 返回空 JPEG")
	}

	now := time.Now()
	absPath, relURL := snapshotPath(s.cfg.UploadRoot, s.cfg.URLPrefix, deviceID, channelID, now)
	if err := writeSnapshotFile(absPath, bytes); err != nil {
		return fmt.Errorf("落盘失败: %w", err)
	}

	if s.cfg.Repo != nil {
		if err := s.cfg.Repo.UpdateSnapshot(context.Background(), deviceID, channelID, relURL, now); err != nil {
			// 文件已经落盘;DB 更新失败只 warn,下次抓拍会重试(覆盖式)
			return fmt.Errorf("UPDATE gb_channel 失败: %w", err)
		}
	}

	s.log.Info("通道快照成功",
		zap.String("device", deviceID),
		zap.String("channel", channelID),
		zap.Int("bytes", len(bytes)),
		zap.String("url", relURL))
	return nil
}

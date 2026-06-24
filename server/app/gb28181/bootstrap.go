package gb28181

import (
	"context"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// sipServer 持有全局 SIP 服务实例,供优雅关闭引用
var sipServer *gbsip.Server

// offlineScanner 离线扫描器
var offlineScanner *device.OfflineScanner

// zlmClient 全局 ZLM 客户端
var zlmClient *gbzlm.Client

// playSvc 全局点播 service(routes 用 GetPlayService 取)
var playSvc *play.Service

// playSessions 全局点播会话管理(让 hook 端点 on_stream_none_reader/on_rtp_server_timeout 也能查到)
var playSessions = uac.NewSessionManager()

// PlayService 返回点播 service(可能为 nil,gb28181 未启用 / UAC 初始化失败时)
func PlayService() *play.Service { return playSvc }

// Start 启动 GB28181 SIP 服务 + 离线扫描器(在 HTTP 服务阻塞等待信号之前调用)
// 若 gb28181.enabled=false 则跳过
func Start() {
	cfg := gbconfig.Load()
	if !cfg.Enabled {
		app.ZapLog.Info("GB28181 未启用,跳过 SIP 服务启动")
		return
	}
	srv, err := gbsip.NewServer(cfg)
	if err != nil {
		app.ZapLog.Error("GB28181 SIP 服务创建失败", zap.Error(err))
		return
	}
	if err := srv.Start(); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务启动失败", zap.Error(err))
		return
	}
	sipServer = srv

	// 启动离线扫描器(基于 keepalive_time 事实派生)
	offlineScanner = device.NewOfflineScanner(
		cfg.Device.OfflineScanInterval,
		cfg.Device.KeepaliveTimeoutCount,
		cfg.Device.KeepaliveGraceSeconds,
	)
	offlineScanner.Start()
	app.ZapLog.Info("GB28181 离线扫描器已启动", zap.Int("intervalSeconds", cfg.Device.OfflineScanInterval))

	// 向 ZLMediaKit 动态下发 Hook 配置(控制面,替代 config.ini 写死)
	zlmClient = gbzlm.NewClient(cfg.ZLM)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := zlmClient.ApplyConfig(ctx, cfg.Media); err != nil {
			app.ZapLog.Warn("GB28181 ZLM 配置下发失败(ZLM 可能暂不可达,不影响启动)", zap.Error(err))
		} else {
			app.ZapLog.Info("GB28181 ZLM Hook 配置已下发", zap.String("hookHost", cfg.Media.HookHost))
		}
	}()

	// 装配点播 service(依赖 SIP UAC + ZLM 客户端 + 流就绪 Notifier)
	if u := srv.UAC(); u != nil {
		playSvc = play.New(cfg, zlmClient, u, playSessions, gbroutes.StreamNotifier(),
			play.NewDeviceRepo(), play.NewChannelRepo())
		gbroutes.SetPlayService(playSvc)
		app.ZapLog.Info("GB28181 点播 service 已装配")
	} else {
		app.ZapLog.Warn("GB28181 UAC 不可用,点播 service 跳过装配")
	}
}

// Stop 优雅关闭 GB28181 SIP 服务 + 离线扫描器(纳入主进程退出流程)
func Stop() {
	if offlineScanner != nil {
		offlineScanner.Stop()
	}
	if sipServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sipServer.Shutdown(ctx); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务关闭异常", zap.Error(err))
	}
}

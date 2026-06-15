package gb28181

import (
	"context"
	"time"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// sipServer 持有全局 SIP 服务实例,供优雅关闭引用
var sipServer *gbsip.Server

// Start 启动 GB28181 SIP 服务(在 HTTP 服务阻塞等待信号之前调用)
// 若 gb28181.enabled=false 则跳过
func Start() {
	cfg := gbconfig.Load()
	if !cfg.Enabled {
		app.ZapLog.Info("GB28181 未启用,跳过 SIP 服务启动")
		return
	}
	srv, err := gbsip.NewServer(cfg.SIP)
	if err != nil {
		app.ZapLog.Error("GB28181 SIP 服务创建失败", zap.Error(err))
		return
	}
	if err := srv.Start(); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务启动失败", zap.Error(err))
		return
	}
	sipServer = srv
}

// Stop 优雅关闭 GB28181 SIP 服务(纳入主进程退出流程)
func Stop() {
	if sipServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sipServer.Shutdown(ctx); err != nil {
		app.ZapLog.Error("GB28181 SIP 服务关闭异常", zap.Error(err))
	}
}

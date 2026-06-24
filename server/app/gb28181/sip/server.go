package sip

import (
	"context"
	"fmt"
	"sync"

	"github.com/emiago/sipgo"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// Server 封装 GB28181 SIP 服务(双栈 UDP+TCP)
type Server struct {
	cfg     gbconfig.Config
	ua      *sipgo.UserAgent
	srv     *sipgo.Server
	regH    *handler.RegisterHandler // 暴露给测试/扩展注入 CatalogTrigger
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// NewServer 创建 SIP 服务
func NewServer(cfg gbconfig.Config) (*Server, error) {
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("UVP-GB28181"))
	if err != nil {
		return nil, fmt.Errorf("创建 SIP UA 失败: %w", err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, fmt.Errorf("创建 SIP server 失败: %w", err)
	}
	s := &Server{cfg: cfg, ua: ua, srv: srv}
	s.registerHandlers()
	return s, nil
}

// registerHandlers 注册 SIP 方法处理器
func (s *Server) registerHandlers() {
	regHandler := handler.NewRegisterHandler(s.cfg)

	// UAC:用于注册成功后向设备发 MESSAGE(Catalog 查询等)
	// 创建失败仅警告:注册仍可工作,只是没有 Catalog 自动触发
	if u, err := uac.New(s.ua, s.cfg.SIP.ServerID, s.cfg.SIP.Domain, s.cfg.SIP.IP, s.cfg.SIP.Port); err != nil {
		app.ZapLog.Warn("GB28181 UAC 初始化失败,跳过注册→Catalog 自动触发", zap.Error(err))
	} else {
		regHandler.SetCatalogTrigger(handler.NewUACCatalogTrigger(u))
	}

	s.regH = regHandler
	s.srv.OnRegister(regHandler.Handle)
	msgHandler := handler.NewMessageHandler(s.cfg)
	s.srv.OnMessage(msgHandler.Handle)
}

// SetCatalogTrigger 替换默认 Catalog 触发器(主要给测试用,生产路径走 registerHandlers 自动注入)
func (s *Server) SetCatalogTrigger(t handler.CatalogTrigger) {
	if s.regH != nil {
		s.regH.SetCatalogTrigger(t)
	}
}

// Start 启动双栈监听(配置里声明的每个 transport 各起一个 goroutine)
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true

	addr := fmt.Sprintf("%s:%d", s.cfg.SIP.IP, s.cfg.SIP.Port)
	for _, tran := range s.cfg.SIP.Transport {
		t := tran
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			app.ZapLog.Info("GB28181 SIP 监听启动", zap.String("transport", t), zap.String("addr", addr))
			if err := s.srv.ListenAndServe(ctx, t, addr); err != nil && ctx.Err() == nil {
				app.ZapLog.Error("GB28181 SIP 监听失败", zap.String("transport", t), zap.Error(err))
			}
		}()
	}
	return nil
}

// Shutdown 优雅关闭
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.started {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.srv != nil {
		_ = s.srv.Close()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		app.ZapLog.Info("GB28181 SIP 服务已优雅关闭")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

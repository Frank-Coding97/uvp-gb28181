package sip

import (
	"context"
	"fmt"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// Server 封装 GB28181 SIP 服务(双栈 UDP+TCP)
type Server struct {
	cfg     gbconfig.SIPConfig
	ua      *sipgo.UserAgent
	srv     *sipgo.Server
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// NewServer 创建 SIP 服务
func NewServer(cfg gbconfig.SIPConfig) (*Server, error) {
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
// T3 阶段先注册最简占位(回 200),REGISTER/MESSAGE 业务逻辑在 T4/T5 接入
func (s *Server) registerHandlers() {
	s.srv.OnRegister(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	s.srv.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
}

// Start 启动双栈监听(配置里声明的每个 transport 各起一个 goroutine)
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true

	addr := fmt.Sprintf("%s:%d", s.cfg.IP, s.cfg.Port)
	for _, tran := range s.cfg.Transport {
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

package sip

import (
	"context"
	"fmt"
	"sync"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// Server 封装 GB28181 SIP 服务(双栈 UDP+TCP)
type Server struct {
	cfg       gbconfig.Config
	ua        *sipgo.UserAgent
	srv       *sipgo.Server
	regH      *handler.RegisterHandler // 暴露给测试/扩展注入 CatalogTrigger
	msgH      *handler.MessageHandler
	notifyH   *handler.NotifyHandler
	uac       *uac.UAC // 供 play service 等业务模块复用
	recorder  metrics.Recorder
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
	trace     gbtrace.Runtime
	traceOnce sync.Once
}

type TraceFactory func(gbconfig.TraceConfig) gbtrace.Runtime

type ServerOption func(*serverOptions)

type serverOptions struct {
	traceFactory TraceFactory
}

func WithTraceFactory(factory TraceFactory) ServerOption {
	return func(options *serverOptions) {
		options.traceFactory = factory
	}
}

// UAC 返回 SIP 服务内置的 UAC(可能为 nil,初始化失败时)
func (s *Server) UAC() *uac.UAC { return s.uac }

// SetRecorder 注入 metrics Recorder,会同时下发到 register/message handler 和 uac
// 必须在 NewServer 之后、Start 之前调用
func (s *Server) SetRecorder(r metrics.Recorder) {
	s.recorder = r
	if s.regH != nil {
		s.regH.SetRecorder(r)
	}
	if s.msgH != nil {
		s.msgH.SetRecorder(r)
	}
	if s.uac != nil {
		s.uac.SetRecorder(r)
	}
}

// NewServer 创建 SIP 服务
func NewServer(cfg gbconfig.Config, options ...ServerOption) (*Server, error) {
	opts := serverOptions{traceFactory: gbtrace.NewRuntime}
	for _, option := range options {
		option(&opts)
	}

	var traceRuntime gbtrace.Runtime
	uaOptions := []sipgo.UserAgentOption{sipgo.WithUserAgent("UVP-GB28181")}
	if cfg.Trace.Enabled && opts.traceFactory != nil {
		traceRuntime = opts.traceFactory(cfg.Trace)
		if traceRuntime != nil {
			uaOptions = append(uaOptions, sipgo.WithUserAgentTransportLayerOptions(
				siplib.WithTransportLayerReadFilter(traceRuntime.ReadFilter),
				siplib.WithTransportLayerWriteObserver(traceRuntime.WriteObserver),
			))
		}
	}

	ua, err := sipgo.NewUA(uaOptions...)
	if err != nil {
		if traceRuntime != nil {
			_ = traceRuntime.Shutdown(context.Background())
		}
		return nil, fmt.Errorf("创建 SIP UA 失败: %w", err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, fmt.Errorf("创建 SIP server 失败: %w", err)
	}
	s := &Server{cfg: cfg, ua: ua, srv: srv, trace: traceRuntime}
	s.registerHandlers()
	return s, nil
}

// registerHandlers 注册 SIP 方法处理器
func (s *Server) registerHandlers() {
	regHandler := handler.NewRegisterHandler(s.cfg)
	msgHandler := handler.NewMessageHandler(s.cfg)

	// UAC:用于注册成功后向设备发 MESSAGE(Catalog 查询等),也供 play service 发 INVITE/BYE
	// 创建失败仅警告:注册仍可工作,只是没有 Catalog 自动触发,点播也不可用
	if u, err := uac.New(s.ua, s.cfg.SIP.ServerID, s.cfg.SIP.Domain, s.cfg.SIP.IP, s.cfg.SIP.Port); err != nil {
		app.ZapLog.Warn("GB28181 UAC 初始化失败,跳过注册→Catalog 自动触发", zap.Error(err))
	} else {
		s.uac = u
		catalogTrigger := handler.NewUACCatalogTrigger(u)
		regHandler.SetCatalogTrigger(catalogTrigger)
		msgHandler.SetCatalogTrigger(catalogTrigger)
		regHandler.SetDeviceInfoTrigger(handler.NewUACDeviceInfoTrigger(u))
	}

	s.regH = regHandler
	s.srv.OnRegister(regHandler.Handle)
	s.msgH = msgHandler
	s.srv.OnMessage(msgHandler.Handle)
	s.notifyH = handler.NewNotifyHandler(nil)
	s.srv.OnNotify(s.notifyH.Handle)
}

// SetCatalogTrigger 替换默认 Catalog 触发器(主要给测试用),同时覆盖注册与心跳恢复路径。
func (s *Server) SetCatalogTrigger(t handler.CatalogTrigger) {
	if s.regH != nil {
		s.regH.SetCatalogTrigger(t)
	}
	if s.msgH != nil {
		s.msgH.SetCatalogTrigger(t)
	}
}

// SetSubscriptionNotifier connects the durable subscription service to inbound NOTIFY requests.
// It must be called before Start.
func (s *Server) SetSubscriptionNotifier(notifier handler.SubscriptionNotifier) {
	if s.notifyH != nil {
		s.notifyH.SetNotifier(notifier)
	}
}

func (s *Server) SetAlarmMessageProcessor(processor handler.AlarmMessageProcessor) {
	if s.msgH != nil {
		s.msgH.SetAlarmProcessor(processor)
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
		return s.shutdownTrace(ctx)
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
		return s.shutdownTrace(ctx)
	case <-ctx.Done():
		_ = s.shutdownTrace(ctx)
		return ctx.Err()
	}
}

func (s *Server) shutdownTrace(ctx context.Context) error {
	var err error
	s.traceOnce.Do(func() {
		if s.trace != nil {
			err = s.trace.Shutdown(ctx)
		}
	})
	return err
}

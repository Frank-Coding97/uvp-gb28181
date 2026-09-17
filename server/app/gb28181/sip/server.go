package sip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/asyncgroup"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

// Server 封装 GB28181 SIP 服务(双栈 UDP+TCP)
type Server struct {
	cascadeDialog      func(*siplib.Request, siplib.ServerTransaction) bool
	cascadeMessageMu   sync.RWMutex
	cascadeMessage     func(*siplib.Request, siplib.ServerTransaction) bool
	logger             *slog.Logger
	cfg                gbconfig.Config
	ua                 *sipgo.UserAgent
	srv                *sipgo.Server
	regH               *handler.RegisterHandler // 暴露给测试/扩展注入 CatalogTrigger
	msgH               *handler.MessageHandler
	notifyH            *handler.NotifyHandler
	uac                *uac.UAC // 供 play service 等业务模块复用
	recorder           metrics.Recorder
	onError            func(error)
	cancel             context.CancelFunc
	listenerWork       *asyncgroup.Group
	businessWork       *requestBusinessGate
	deviceInfoWork     *asyncgroup.Group
	quiesce            lifecyclePhase
	shutdown           lifecyclePhase
	started            bool
	trace              gbtrace.Runtime
	traceOnce          sync.Once
	traceErr           error
	security           handler.RegisterSecurity
	broadcastDialogs   *sipgo.DialogServerCache
	broadcastProcessor handler.BroadcastInviteProcessor
	broadcastSessions  sync.Map
	// deviceLinkSink 接收「可靠传输通道断开」通知。见 DeviceLinkSink 的注释:
	// 为什么这件事必须由设备域而不是 sip 层来完成。
	deviceLinkSink DeviceLinkSink
	// linkLossMuted 在关停开始时置位,用于**停止**把断开上抛成设备离线。
	// 平台自己 Close 时每条 TCP 都会被关掉,若照常判定,一次重启会写出成百上千条
	// 「链路断开」事件,还可能撞上已经先关掉的数据库。设备在这段时间确实失联,
	// 但那是重启的既定后果,由重启后的注册/心跳重新收敛,不必在这里留痕。
	linkLossMuted atomic.Bool
}

// requestBusinessGate tracks the synchronous middleware and business handler
// portion of an admitted SIP request. It deliberately keeps already admitted
// requests runnable after Close; only the admission decision is closed.
type requestBusinessGate struct {
	mu     sync.Mutex
	active int
	closed bool
	done   chan struct{}
}

func (g *requestBusinessGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	if g.done == nil {
		g.done = make(chan struct{})
	}
	g.active++
	return true
}

func (g *requestBusinessGate) leave() {
	g.mu.Lock()
	g.active--
	if g.closed && g.active == 0 {
		close(g.done)
	}
	g.mu.Unlock()
}

func (g *requestBusinessGate) close() {
	g.mu.Lock()
	if !g.closed {
		g.closed = true
		if g.done == nil {
			g.done = make(chan struct{})
		}
		if g.active == 0 {
			close(g.done)
		}
	}
	g.mu.Unlock()
}

func (g *requestBusinessGate) wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g.mu.Lock()
	done := g.done
	if done == nil {
		done = make(chan struct{})
		g.done = done
		if g.closed && g.active == 0 {
			close(done)
		}
	}
	g.mu.Unlock()

	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

type requestBusinessLease struct {
	gate *requestBusinessGate
	once sync.Once
}

func (l *requestBusinessLease) Release() {
	if l == nil || l.gate == nil {
		return
	}
	l.once.Do(l.gate.leave)
}

type sipRequestLifecycle struct{ business *requestBusinessGate }

func (l *sipRequestLifecycle) ReserveBusiness(_ *siplib.Request, _ siplib.ServerTransaction) (siplib.RequestLease, bool) {
	if l == nil || l.business == nil {
		return nil, true
	}
	if !l.business.enter() {
		return nil, false
	}
	return &requestBusinessLease{gate: l.business}, true
}

func (l *sipRequestLifecycle) BeginBusiness(_ *siplib.Request, tx siplib.ServerTransaction) bool {
	if l == nil || l.business == nil {
		return true
	}
	if tx != nil {
		if provider, ok := tx.(interface{ RequestLease() siplib.RequestLease }); ok && provider.RequestLease() != nil {
			return true
		}
	}
	return l.business.enter()
}

func (l *sipRequestLifecycle) EndBusiness() {
	if l != nil && l.business != nil {
		l.business.leave()
	}
}

type lifecyclePhase struct {
	mu      sync.Mutex
	started bool
	done    chan struct{}
	err     error
}

func (p *lifecyclePhase) run(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if p.started {
		done := p.done
		p.mu.Unlock()
		return waitLifecyclePhase(ctx, done, p)
	}
	p.started = true
	p.done = make(chan struct{})
	p.mu.Unlock()

	err := fn(ctx)
	p.mu.Lock()
	p.err = err
	close(p.done)
	p.mu.Unlock()
	return err
}

func waitLifecyclePhase(ctx context.Context, done <-chan struct{}, p *lifecyclePhase) error {
	select {
	case <-done:
		p.mu.Lock()
		err := p.err
		p.mu.Unlock()
		return err
	default:
	}
	select {
	case <-done:
		p.mu.Lock()
		err := p.err
		p.mu.Unlock()
		return err
	case <-ctx.Done():
		select {
		case <-done:
			p.mu.Lock()
			err := p.err
			p.mu.Unlock()
			return err
		default:
			return ctx.Err()
		}
	}
}

type TraceFactory func(gbconfig.TraceConfig) gbtrace.Runtime

type ServerOption func(*serverOptions)

type serverOptions struct {
	traceFactory      TraceFactory
	securityAdmission *gbsecurity.Admission
	registerSecurity  handler.RegisterSecurity
	deviceLinkSink    DeviceLinkSink
}

func WithTraceFactory(factory TraceFactory) ServerOption {
	return func(options *serverOptions) {
		options.traceFactory = factory
	}
}

// WithSecurityAdmission installs the pre-parser security gate. The gate is
// composed with Trace rather than replacing it, because sipgo accepts one
// TransportReadFilter per transport.
func WithSecurityAdmission(admission *gbsecurity.Admission) ServerOption {
	return func(options *serverOptions) { options.securityAdmission = admission }
}

// WithSecurityRuntime installs both the transport admission gate and REGISTER
// authentication protection from the same runtime generation.
func WithSecurityRuntime(runtime *gbsecurity.Runtime) ServerOption {
	return func(options *serverOptions) {
		if runtime == nil {
			return
		}
		options.securityAdmission = runtime.Admission()
		options.registerSecurity = runtime
	}
}

// SetErrorHandler registers a callback for asynchronous listener failures.
// It must be set before Start.
func (s *Server) SetErrorHandler(fn func(error)) { s.onError = fn }

// UAC 返回 SIP 服务内置的 UAC(可能为 nil,初始化失败时)
func (s *Server) UAC() *uac.UAC { return s.uac }

// NewCascadeClient creates another transaction client on the server-owned UA.
// The UA and its listeners remain owned by Server and are closed by Shutdown.
func (s *Server) NewCascadeClient() (*sipgo.Client, error) {
	if s == nil || s.ua == nil {
		return nil, fmt.Errorf("GB28181 shared SIP UA is unavailable")
	}
	return sipgo.NewClient(s.ua, sipgo.WithClientLogger(s.logger))
}

// TraceRuntime returns the optional trace runtime for controller bootstrap wiring.
func (s *Server) TraceRuntime() gbtrace.Runtime { return s.trace }

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

	root := app.ZapLog
	if root == nil {
		root = zap.NewNop()
	}
	logger := slog.New(logging.NewSlogHandler(root.Named("sipgo")))
	var traceRuntime gbtrace.Runtime
	uaOptions := []sipgo.UserAgentOption{sipgo.WithUserAgent("UVP-GB28181"), sipgo.WithUserAgentTransactionLayerOptions(siplib.WithTransactionLayerLogger(logger))}
	if cfg.Trace.Enabled && opts.traceFactory != nil {
		traceRuntime = opts.traceFactory(cfg.Trace)
		if traceRuntime != nil {
			// 采集拿到的 LocalAddr 是 [::]:port 通配符,注入平台实际 AdvertiseIP:Port
			// 让 Source/Destination 显示真实端点
			if setter, ok := traceRuntime.(interface{ SetPlatformAddr(string) }); ok && !cfg.SIP.DynamicAdvertise && cfg.SIP.AdvertiseIP != "" && cfg.SIP.Port > 0 {
				setter.SetPlatformAddr(fmt.Sprintf("%s:%d", cfg.SIP.AdvertiseIP, cfg.SIP.Port))
			}
		}
	}
	var readFilter siplib.TransportReadFilter
	var closeObserver siplib.TransportConnectionCloseObserver
	if traceRuntime != nil {
		readFilter = traceRuntime.ReadFilter
		closeObserver = traceRuntime.ConnectionClosed
	}
	if opts.securityAdmission != nil {
		opts.securityAdmission.SetTraceFilter(readFilter)
		readFilter = opts.securityAdmission.Filter
		previousClose := closeObserver
		closeObserver = func(info siplib.TransportReadProps) {
			opts.securityAdmission.ConnectionClosed(info)
			if previousClose != nil {
				previousClose(info)
			}
		}
	}

	// GB/T 28181-2022 §9.1.1 f):「若 TCP 通道断开,则认为 SIP 代理异常掉线」。
	// 包在链路最外层(最后执行) —— trace 与 security 是「记录 / 风控」,它们先跑完;
	// 离线判定会改设备状态,放最后保证前两者无论设备域是否装配都照常收到回调。
	//
	// ⛔ 这里用 serverRef 而不是直接捕获 s:closeObserver 在 NewServer 里构造时
	// Server 实例还没 new 出来(它在下面才创建)。也别改成"在闭包里查全局单例"。
	var serverRef *Server
	if opts.deviceLinkSink != nil {
		previousClose := closeObserver
		closeObserver = func(info siplib.TransportReadProps) {
			if previousClose != nil {
				previousClose(info)
			}
			if serverRef != nil {
				serverRef.notifyLinkLoss(info)
			}
		}
	}
	{
		transportOptions := []siplib.TransportLayerOption{siplib.WithTransportLayerLogger(logger)}
		if readFilter != nil {
			transportOptions = append(transportOptions, siplib.WithTransportLayerReadFilter(readFilter))
		}
		if traceRuntime != nil {
			transportOptions = append(transportOptions, siplib.WithTransportLayerWriteObserver(traceRuntime.WriteObserver))
		}
		if closeObserver != nil {
			transportOptions = append(transportOptions, siplib.WithTransportLayerConnectionCloseObserver(closeObserver))
		}
		uaOptions = append(uaOptions, sipgo.WithUserAgentTransportLayerOptions(transportOptions...))
	}

	ua, err := sipgo.NewUA(uaOptions...)
	if err != nil {
		if traceRuntime != nil {
			_ = traceRuntime.Shutdown(context.Background())
		}
		return nil, fmt.Errorf("创建 SIP UA 失败: %w", err)
	}
	businessWork := &requestBusinessGate{}
	srv, err := sipgo.NewServer(ua,
		sipgo.WithServerLogger(logger),
		sipgo.WithServerRequestLifecycle(&sipRequestLifecycle{business: businessWork}),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 SIP server 失败: %w", err)
	}
	s := &Server{
		logger:         logger,
		cfg:            cfg,
		ua:             ua,
		srv:            srv,
		trace:          traceRuntime,
		security:       opts.registerSecurity,
		businessWork:   businessWork,
		deviceInfoWork: &asyncgroup.Group{},
		listenerWork:   &asyncgroup.Group{},
		deviceLinkSink: opts.deviceLinkSink,
	}
	// 让上面那个连接断开观察者拿到 Server —— 它是在 NewServer 前段构造的闭包,
	// 那时还没有 Server 实例可取。
	serverRef = s
	s.registerHandlers()
	return s, nil
}

// registerHandlers 注册 SIP 方法处理器
func (s *Server) registerHandlers() {
	regHandler := handler.NewRegisterHandler(s.cfg)
	regHandler.SetSecurity(s.security)
	regHandler.SetDiagnosticSink(gbtrace.DiagnosisSinkFromRuntime(s.trace))
	msgHandler := handler.NewMessageHandler(s.cfg)

	// UAC:用于注册成功后向设备发 MESSAGE(Catalog 查询等),也供 play service 发 INVITE/BYE
	// 留 ERROR（C03.④ 复核）：原文写的是"创建失败仅警告，注册仍可工作"，但后果并不轻 ——
	// 没有 UAC 就没有 **Catalog 自动触发 / DeviceInfo 查询，点播也不可用**。这是启动期
	// 一次性的、不会自愈的整块能力缺失，属于 C03.③ 第三组"SIP 子系统装配失败"
	// （同组的 `gb28181.sip.start_failed` / `gb28181.sip.config_load_failed` 都是 ERROR）。
	// ⚠️ 紧邻的 Broadcast client 失败**不升**：`handleBroadcastInvite` 在
	// `broadcastDialogs == nil` 时会回 503 "Broadcast Service Unavailable" —— 有替代信号。
	if u, err := uac.New(s.ua, s.cfg.SIP.ServerID, s.cfg.SIP.Domain, s.cfg.SIP.AdvertiseIP, s.cfg.SIP.Port, s.cfg.SIP.DynamicAdvertise, sipgo.WithClientLogger(s.logger)); err != nil {
		app.Log(context.Background()).Named("gb28181.sip").Error(
			"GB28181 UAC 初始化失败,跳过注册→Catalog 自动触发",
			zap.String("event", "gb28181.sip.uac_init_failed"), logging.Error(err))
	} else {
		s.uac = u
		catalogTrigger := handler.NewUACCatalogTrigger(u)
		regHandler.SetCatalogTrigger(catalogTrigger)
		msgHandler.SetCatalogTrigger(catalogTrigger)
		regHandler.SetDeviceInfoTrigger(handler.NewUACDeviceInfoTrigger(u, s.deviceInfoWork))
		contactHost := resolveBroadcastContactHost(s.cfg.SIP.AdvertiseIP, s.cfg.SIP.ListenIP)
		if contactHost != "" {
			if client, clientErr := sipgo.NewClient(s.ua, sipgo.WithClientLogger(s.logger)); clientErr == nil {
				s.broadcastDialogs = sipgo.NewDialogServerCache(client, siplib.ContactHeader{Address: siplib.Uri{User: s.cfg.SIP.ServerID, Host: contactHost, Port: s.cfg.SIP.Port}})
			} else {
				// 留 WARN（C03.④ 复核过）：广播是**子能力**，不是核心链路；缺失时有替代
				// 信号 —— `handleBroadcastInvite` 会回 503 "Broadcast Service Unavailable"，
				// 上游明确知道它不可用。别按"和上面 UAC 那条长得一样"把它升成 ERROR。
				app.Log(context.Background()).Named("gb28181.sip").Warn(
					"GB28181 Broadcast UAS client 初始化失败",
					zap.String("event", "gb28181.sip.broadcast_client_init_failed"), logging.Error(clientErr))
			}
		} else {
			app.Log(context.Background()).Named("gb28181.sip").Warn(
				"GB28181 Broadcast UAS 缺少可用 Contact 地址,语音广播将不可用(设备 INVITE 会被回 503)",
				zap.String("event", "gb28181.sip.broadcast_contact_host_unavailable"),
				zap.String("advertise_ip", strings.TrimSpace(s.cfg.SIP.AdvertiseIP)),
				zap.String("listen_ip", strings.TrimSpace(s.cfg.SIP.ListenIP)))
		}
	}

	s.regH = regHandler
	s.srv.OnRegister(regHandler.Handle)
	s.msgH = msgHandler
	s.srv.OnMessage(func(req *siplib.Request, tx siplib.ServerTransaction) {
		s.cascadeMessageMu.RLock()
		hook := s.cascadeMessage
		s.cascadeMessageMu.RUnlock()
		if hook != nil && hook(req, tx) {
			return
		}
		msgHandler.Handle(req, tx)
	})
	s.notifyH = handler.NewNotifyHandler(nil)
	s.srv.OnNotify(s.notifyH.Handle)
	s.srv.OnInvite(s.dispatchCascadeDialog(s.handleBroadcastInvite))
	s.srv.OnAck(s.dispatchCascadeDialog(s.handleBroadcastAck))
	s.srv.OnBye(s.dispatchCascadeDialog(s.handleBye))
}

func (s *Server) handleBroadcastInvite(req *siplib.Request, tx siplib.ServerTransaction) {
	if req == nil || tx == nil {
		return
	}
	if s.broadcastDialogs == nil || s.broadcastProcessor == nil {
		_ = tx.Respond(siplib.NewResponseFromRequest(req, siplib.StatusServiceUnavailable, "Broadcast Service Unavailable", nil))
		return
	}
	dialog, err := s.broadcastDialogs.ReadInvite(req, tx)
	if err != nil {
		_ = tx.Respond(siplib.NewResponseFromRequest(req, siplib.StatusBadRequest, "Invalid Dialog", nil))
		return
	}
	callID := requestCallID(req)
	peerID := ""
	if from := req.From(); from != nil {
		peerID = from.Address.User
	}
	targetID := broadcastSubjectTarget(req)
	cseq := uint(0)
	if req.CSeq() != nil {
		cseq = uint(req.CSeq().SeqNo)
	}
	prepared, prepareErr := s.broadcastProcessor.PrepareBroadcastInvite(context.Background(), handler.BroadcastInviteRequest{
		PeerID: peerID, TargetID: targetID, CallID: callID, CSeq: cseq, SDP: string(req.Body()),
	})
	if prepareErr != nil {
		status := siplib.StatusNotAcceptableHere
		if statusProvider, ok := prepareErr.(interface{ SIPStatus() int }); ok {
			status = statusProvider.SIPStatus()
		}
		_ = dialog.Respond(status, prepareErr.Error(), nil)
		_ = dialog.Close()
		return
	}
	// 设备（UAC）收到 2xx 必须回 ACK，然后才能用 BYE 取消 —— 而 BYE 的理由通常是
	// "应答里没有可用的媒体描述"。所以**没有 SDP 的 200 OK 是协议级错误**：
	// 设备拿不到平台收流地址，只能立刻 ACK+BYE，表现为广播"偶发建立失败"。
	// 这里兜住上游任何"空 answer + nil error"的走漏，让它变成一次明确失败的 INVITE。
	if strings.TrimSpace(prepared.AnswerSDP) == "" || strings.TrimSpace(prepared.SessionID) == "" {
		app.Log(context.Background()).Named("gb28181.sip").Warn(
			"Broadcast INVITE 应答缺少 SDP，拒绝以 200 OK 应答",
			zap.String("event", "gb28181.sip.broadcast_answer_missing"),
			zap.String("call_id", callID), zap.String("session_id", prepared.SessionID))
		_ = dialog.Respond(siplib.StatusInternalServerError, "Broadcast Answer Unavailable", nil)
		_ = dialog.Close()
		return
	}
	s.broadcastSessions.Store(callID, struct {
		sessionID string
		dialog    *sipgo.DialogServerSession
	}{prepared.SessionID, dialog})
	if err := dialog.RespondSDP([]byte(prepared.AnswerSDP)); err != nil {
		s.broadcastSessions.Delete(callID)
		_ = dialog.Close()
		_ = s.broadcastProcessor.OnBroadcastBye(context.Background(), callID)
	}
}

func (s *Server) handleBroadcastAck(req *siplib.Request, tx siplib.ServerTransaction) {
	if req == nil || s.broadcastDialogs == nil {
		return
	}
	callID := requestCallID(req)
	ctx := context.Background()
	logger := app.Log(ctx).Named("gb28181.sip")
	if _, ok := s.broadcastSessions.Load(callID); !ok {
		return
	}
	if err := s.broadcastDialogs.ReadAck(req, tx); err != nil {
		logger.Warn("处理设备 Broadcast ACK 失败",
			zap.String("event", "gb28181.sip.broadcast_ack_failed"), zap.String("call_id", callID), logging.Error(err))
		return
	}
	if s.broadcastProcessor != nil {
		if err := s.broadcastProcessor.OnBroadcastAck(context.Background(), callID); err != nil {
			logger.Warn("激活 Broadcast 会话失败",
				zap.String("event", "gb28181.sip.broadcast_activation_failed"), zap.String("call_id", callID),
				logging.Error(err))
		}
	}
}

func (s *Server) handleBye(req *siplib.Request, tx siplib.ServerTransaction) {
	callID := requestCallID(req)
	ctx := context.Background()
	logger := app.Log(ctx).Named("gb28181.sip")
	if _, ok := s.broadcastSessions.Load(callID); ok && s.broadcastDialogs != nil {
		if err := s.broadcastDialogs.ReadBye(req, tx); err != nil {
			logger.Warn("处理设备 Broadcast BYE 失败",
				zap.String("event", "gb28181.sip.broadcast_bye_failed"), zap.String("call_id", callID), logging.Error(err))
			return
		}
		s.broadcastSessions.Delete(callID)
		if s.broadcastProcessor != nil {
			if err := s.broadcastProcessor.OnBroadcastBye(context.Background(), callID); err != nil {
				logger.Warn("设备 Broadcast BYE 清理失败",
					zap.String("event", "gb28181.sip.broadcast_bye_cleanup_failed"), zap.String("call_id", callID),
					logging.Error(err))
			}
		}
		return
	}
	if strings.HasPrefix(callID, "talk-") && s.uac != nil {
		if _, err := s.uac.HandleTalkBye(req, tx); err != nil {
			logger.Warn("处理设备 TALK BYE 失败",
				zap.String("event", "gb28181.sip.talk_bye_failed"), zap.String("call_id", callID), logging.Error(err))
		}
		return
	}
	if req != nil && tx != nil {
		_ = tx.Respond(siplib.NewResponseFromRequest(req, siplib.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
	}
}

func requestCallID(req *siplib.Request) string {
	if req != nil && req.CallID() != nil {
		return string(*req.CallID())
	}
	return ""
}

func broadcastSubjectTarget(req *siplib.Request) string {
	if req == nil {
		return ""
	}
	if subject := req.GetHeader("Subject"); subject != nil {
		value := strings.TrimSpace(subject.Value())
		if index := strings.IndexAny(value, ":,"); index > 0 {
			return strings.TrimSpace(value[:index])
		}
		return value
	}
	return ""
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

// SetSubscriptionWaker connects online recovery events to durable subscriptions.
func (s *Server) SetSubscriptionWaker(w handler.SubscriptionWaker) {
	if s.regH != nil {
		s.regH.SetSubscriptionWaker(w)
	}
	if s.msgH != nil {
		s.msgH.SetSubscriptionWaker(w)
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

func (s *Server) SetPTZMessageProcessor(processor handler.PTZMessageProcessor) {
	if s.msgH != nil {
		s.msgH.SetPTZProcessor(processor)
	}
}

func (s *Server) SetUpgradeProcessor(processor handler.UpgradeMessageProcessor) {
	if s.msgH != nil {
		s.msgH.SetUpgradeProcessor(processor)
	}
}

func (s *Server) SetUpgradeMessageProcessor(processor handler.UpgradeMessageProcessor) {
	s.SetUpgradeProcessor(processor)
}

func (s *Server) SetBroadcastMessageProcessor(processor handler.BroadcastMessageProcessor) {
	if s.msgH != nil {
		s.msgH.SetBroadcastProcessor(processor)
	}
}

func (s *Server) SetBroadcastInviteProcessor(processor handler.BroadcastInviteProcessor) {
	s.broadcastProcessor = processor
}

// resolveBroadcastContactHost 决定 Broadcast UAS(等设备主动 INVITE)写进 Contact 头的本机地址。
//
// ⛔ 通配监听时**不能**把 ListenIP 直接当 Contact Host:`0.0.0.0` / `::` 写进 Contact 是无意义
// 地址,原先的做法是"整块跳过创建"——而跳过是**静默**的,于是设备发来的 Broadcast INVITE 会被
// `handleBroadcastInvite` 一律回 503 "Broadcast Service Unavailable",现象是前端一直停在
// 「正在建立广播」,而 SIP 报文里能看到设备 INVITE 明明白白到达。
//
// LAN + 通配监听本身是**允许**的部署(`setup.allowDynamicAdvertise`):此时内核路由与实时网卡
// 扫描才是权威,持久化的 advertise_ip 可能是过期值(同 `setup.ActiveSIPAddresses` 的口径)。
// 所以 advertise_ip 为空时退回实时枚举本机非回环 IPv4 —— 只在"本来必然失败"的路径上兜底,
// 静态配置的既有行为不变。
func resolveBroadcastContactHost(advertiseIP, listenIP string) string {
	if host := concreteSIPHost(advertiseIP); host != "" {
		return host
	}
	if host := concreteSIPHost(listenIP); host != "" {
		return host
	}
	return firstLocalIPv4()
}

func concreteSIPHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return ""
	}
	return host
}

func firstLocalIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterfaceName(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

// isVirtualInterfaceName 与 `setup.isVirtualInterface`(setup/network.go) **同口径**:
// VPN / 容器 / 虚拟机网卡上的地址设备路由不到,不能被选成 Contact Host。
// 两处规则要一起改,别只改一边。
func isVirtualInterfaceName(name string) bool {
	lowered := strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{
		"br-", "bridge", "cali", "cni", "docker", "flannel", "kube",
		"podman", "tap", "tailscale", "tun", "utun", "vboxnet", "veth",
		"virbr", "vmnet", "wg",
	} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) ByeBroadcast(ctx context.Context, callID string) error {
	value, ok := s.broadcastSessions.Load(strings.TrimSpace(callID))
	if !ok {
		return nil
	}
	entry := value.(struct {
		sessionID string
		dialog    *sipgo.DialogServerSession
	})
	err := entry.dialog.Bye(ctx)
	if err == nil {
		s.broadcastSessions.Delete(callID)
		_ = entry.dialog.Close()
	}
	return err
}

func (s *Server) SetRecordInfoSink(sink handler.RecordInfoSink) {
	if s.msgH != nil {
		s.msgH.SetRecordInfoSink(sink)
	}
}

func (s *Server) SetSnapshotSink(sink handler.SnapshotSink) {
	if s.msgH != nil {
		s.msgH.SetSnapshotSink(sink)
	}
}

func (s *Server) SetPlaybackEndSink(sink handler.PlaybackEndSink) {
	if s.msgH != nil {
		s.msgH.SetPlaybackEndSink(sink)
	}
}

// Start 启动双栈监听(配置里声明的每个 transport 各起一个 goroutine)
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.started = true

	addr := fmt.Sprintf("%s:%d", s.cfg.SIP.ListenIP, s.cfg.SIP.Port)
	for _, tran := range s.cfg.SIP.Transport {
		t := tran
		if s.listenerWork == nil {
			s.listenerWork = &asyncgroup.Group{}
		}
		s.listenerWork.Go(func() {
			logger := app.Log(ctx).Named("gb28181.sip")
			logger.Info("GB28181 SIP 监听启动",
				zap.String("event", "gb28181.sip.listener_started"),
				zap.String("transport", t), zap.String("address", addr))
			if err := s.srv.ListenAndServe(ctx, t, addr); err != nil && ctx.Err() == nil {
				logger.Error("GB28181 SIP 监听失败",
					zap.String("event", "gb28181.sip.listener_failed"),
					zap.String("transport", t), zap.String("address", addr), logging.Error(err))
				if s.onError != nil {
					s.onError(err)
				}
			}
		})
	}
	return nil
}

// QuiesceRequests 停止新业务事务入场,并等待已接纳的 middleware/handler 返回。
// UAC 出站传输仍保持可用,供根生命周期继续完成依赖清理。
func (s *Server) QuiesceRequests(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.quiesce.run(ctx, func(ctx context.Context) error {
		if s.srv != nil {
			s.srv.QuiesceRequests()
		}
		var err error
		if s.businessWork != nil {
			s.businessWork.close()
			err = errors.Join(err, s.businessWork.wait(ctx))
		}
		// DeviceInfo is deliberately launched after a successful 200 response;
		// close its owner only after the synchronous handler set is drained.
		if s.deviceInfoWork != nil {
			err = errors.Join(err, s.deviceInfoWork.StopContext(ctx))
		}
		if s.regH != nil {
			err = errors.Join(err, s.regH.Close(ctx))
		}
		return err
	})
}

// Shutdown 优雅关闭。调用方应先完成其他服务依赖的 drain,再进入本方法。
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.shutdown.run(ctx, func(ctx context.Context) error {
		// 先静音链路断开判定:下面 srv.CloseContext 会把所有 TCP 一起关掉,
		// 那是平台自己断的,不是设备掉线(见 linkLossMuted 的注释)。
		s.linkLossMuted.Store(true)
		var err error
		err = errors.Join(err, s.QuiesceRequests(ctx))
		if s.cancel != nil {
			s.cancel()
		}
		if s.listenerWork != nil {
			err = errors.Join(err, s.listenerWork.StopContext(ctx))
		}
		if s.srv != nil {
			err = errors.Join(err, s.srv.CloseContext(ctx))
		}
		// Trace must be the final producer closed by the SIP owner.
		err = errors.Join(err, s.shutdownTrace(ctx))
		logger := app.Log(ctx).Named("gb28181.sip")
		if err == nil {
			logger.Info("GB28181 SIP 服务已优雅关闭",
				zap.String("event", "gb28181.sip.shutdown_complete"))
		} else {
			logger.Warn("GB28181 SIP 服务关闭未完成",
				zap.String("event", "gb28181.sip.shutdown_incomplete"), logging.Error(err))
		}
		return err
	})
}

func (s *Server) shutdownTrace(ctx context.Context) error {
	s.traceOnce.Do(func() {
		if s.trace != nil {
			s.traceErr = s.trace.Shutdown(ctx)
		}
	})
	return s.traceErr
}

func (s *Server) SetCascadeMessageHandler(hook func(*siplib.Request, siplib.ServerTransaction) bool) {
	s.cascadeMessageMu.Lock()
	defer s.cascadeMessageMu.Unlock()
	s.cascadeMessage = hook
}

// SetCascadeDialogHandler installs the video UAS after its media dependencies are ready.
func (s *Server) SetCascadeDialogHandler(fn func(*siplib.Request, siplib.ServerTransaction) bool) {
	s.cascadeMessageMu.Lock()
	s.cascadeDialog = fn
	s.cascadeMessageMu.Unlock()
}
func (s *Server) dispatchCascadeDialog(fallback func(*siplib.Request, siplib.ServerTransaction)) func(*siplib.Request, siplib.ServerTransaction) {
	return func(req *siplib.Request, tx siplib.ServerTransaction) {
		s.cascadeMessageMu.RLock()
		hook := s.cascadeDialog
		s.cascadeMessageMu.RUnlock()
		if hook != nil && hook(req, tx) {
			return
		}
		fallback(req, tx)
	}
}

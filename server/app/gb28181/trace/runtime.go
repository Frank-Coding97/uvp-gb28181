package trace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/trace/diagnosis"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// Runtime is the lifecycle boundary between the SIP transport and trace pipeline.
type Runtime interface {
	ReadFilter(sip.TransportReadProps, []byte) ([]byte, error)
	WriteObserver(sip.TransportWriteProps, []byte)
	ConnectionClosed(sip.TransportReadProps)
	Health() HealthSnapshot
	Shutdown(context.Context) error
}

// Module owns transport hooks, framing, the bounded queue, and the batch writer lifecycle.
type Module struct {
	closed            atomic.Bool
	framer            *FrameAssembler
	onFrame           func(Frame)
	collector         *Collector
	store             Store
	cipher            PayloadCipher
	health            *healthTracker
	diagnosis         *diagnosis.Service
	diagnosisFallback diagnosis.HealthSnapshot
	streamHub         *StreamHub
	// platformAddr 是 SIP 服务实际的 AdvertiseIP:Port,用于替换采集拿到的
	// wildcard socket 地址([::]:5062 / 0.0.0.0:5062),这样 Source/Destination
	// 显示给用户的是真实的平台端点而不是绑定通配符。
	platformAddr   string
	batchSize      int
	flushInterval  time.Duration
	retryMin       time.Duration
	retryMax       time.Duration
	now            func() time.Time
	cancel         context.CancelFunc
	done           chan struct{}
	retryDone      chan struct{}
	prunerDone     chan struct{}
	retryQueue     chan retryBatch
	pendingRetries atomic.Int64
	shutdownOnce   sync.Once
	cleanupOnce    sync.Once
	cleanupDone    chan struct{}
	shutdownErr    error
	storeCloseOnce sync.Once
	storeCloseErr  error
	lifecycleCtx   context.Context
	probeMu        sync.Mutex
	probeWG        sync.WaitGroup
	probeStopped   bool
	probeDone      chan struct{}
	probeWaitOnce  sync.Once
}

// StreamHub 暴露给 controller 挂 SSE 端点使用。启动即创建,非采集必需。
func (m *Module) StreamHub() *StreamHub {
	return m.streamHub
}

// SetPlatformAddr 由 sip server 启动时调用,注入平台实际 AdvertiseIP:Port,
// 用来把采集拿到的 wildcard 本地地址([::]:5062)替换成用户可读的真实端点。
func (m *Module) SetPlatformAddr(addr string) {
	m.platformAddr = addr
}

// resolveAddr 把 wildcard 通配符地址换成 platformAddr,其他地址原样返回
func (m *Module) resolveAddr(addr, peerAddr string) string {
	if !strings.HasPrefix(addr, "[::]:") && !strings.HasPrefix(addr, "0.0.0.0:") {
		return addr
	}
	if m.platformAddr != "" {
		return m.platformAddr
	}
	port := ""
	if _, parsedPort, err := net.SplitHostPort(addr); err == nil {
		port = parsedPort
	}
	if peerAddr != "" {
		if connection, err := net.DialTimeout("udp", peerAddr, 100*time.Millisecond); err == nil {
			localHost, _, splitErr := net.SplitHostPort(connection.LocalAddr().String())
			_ = connection.Close()
			if splitErr == nil && net.ParseIP(localHost) != nil {
				return net.JoinHostPort(localHost, port)
			}
		}
	}
	if port != "" {
		return "本机 SIP:" + port
	}
	return "本机 SIP"
}

func NewRuntime(cfg gbconfig.TraceConfig) Runtime {
	return NewRuntimeWithDB(cfg, activeTraceDB())
}

// activeTraceDB 返回报文诊断用的关系型连接。
//
// ⛔⛔⛔ 这里**必须直接用 app.DB()，不能自己按 usedbtype 分发**（2026-10-09 修）
//
// 原来的实现是「switch usedbtype → mysql/postgresql/sqlserver」+ 一层
// 「兜底遍历 app.GormDbMysql/PostgreSql/Sqlserver」的循环。
// 绿色安装包跑的是 SQLite（`gormv2.usedbtype: sqlite`），
// 而 switch 里**没有 sqlite 分支** ⇒ 落到兜底循环 ⇒ 三个传统全局连接全是 nil
// ⇒ 返回 nil ⇒ traceRelationalStore/diagnosisService 双双降级
// ⇒ health 报`SIP trace store is unavailable` + `diagnosis repository unavailable`，
// **日志里一句原因都没有**（装配处根本不打印）。
//
// ⭐ `app.DB()` 本身已是方言感知的统一入口（记忆里那条「控制器层已统一改用
// app.DB()」就是为这个），trace 模块当时漏了。同一类修复本项目已做过 5 个控制器，
// 这次是第6 个 —— 说明**新增调用方必须默认走 app.DB()，别再抄那段switch**。
func activeTraceDB() *gorm.DB {
	// ⛔ app.DB() 内部**直接解引用 app.ConfigYml**，ConfigYml 为 nil 会 panic
	//   （测试环境与极早期的启动阶段就是这种情况 ——实测
	//   TestHealthStatesCoverDisabledDegradedAndReady 直接 SIGSEGV）。
	//   ⇒ 这里必须先挡一道，**不能**把nil 透传给 app.DB()。
	if app.ConfigYml == nil {
		return nil
	}
	// app.DB() 内部按 usedbtype 分发并对未知/空值有兜底；
	// SQLite 是绿色包的默认形态，必须覆盖到。
	return app.DB()
}

func NewRuntimeWithDB(cfg gbconfig.TraceConfig, db *gorm.DB) Runtime {
	payloadCipher, cipherErr := tracePayloadCipher(cfg)
	store := traceRelationalStore(cfg, db, cipherErr)
	diagnosisService, diagnosisFallback := traceDiagnosisService(cfg, db)
	module := NewModuleWithDiagnosis(cfg, store, payloadCipher, diagnosisService)
	module.diagnosisFallback = diagnosisFallback
	module.startDiagnosisHealthProbe(db, diagnosisService)
	if cipherErr != nil {
		module.health.degraded("trace encryption key is unavailable")
	} else {
		module.health.degraded(ErrTraceStoreUnavailable.Error())
		module.startRelationalHealthProbe(store)
	}
	return module
}

// tracePayloadCipher 从环境加载加密密钥,失败时返回 failingCipher
func tracePayloadCipher(cfg gbconfig.TraceConfig) (PayloadCipher, error) {
	loadedCipher, err := LoadCipherFromEnv(cfg.EncryptionKeyEnv, "v1")
	if err != nil {
		return failingCipher{err: ErrInvalidEncryptionKey}, err
	}
	return loadedCipher, nil
}

// traceRelationalStore 装配关系型存储(带重连);密钥不可用或 db 为空时降级 unavailableStore
func traceRelationalStore(cfg gbconfig.TraceConfig, db *gorm.DB, cipherErr error) Store {
	if cipherErr != nil || db == nil {
		// ⛔⛔ 降级时**必须打出原因**（2026-10-09 补）。
		// 原实现静默返回 unavailableStore，日志里一句都没有 ——
		// 现场只看到 health 报 `SIP trace store is unavailable`，
		// 而「密钥缺失」与「db 是 nil」这两个原因要翻代码 + 查配置才能区分，
		// 这次就为它挖了半小时。降级不是正常状态，别静默。
		usedbtype := ""
		if app.ConfigYml != nil {
			usedbtype = app.ConfigYml.GetString("gormv2.usedbtype")
		}
		logger := app.Log(context.Background()).Named("gb28181.trace")
		logger.Error("SIP 报文诊断存储不可用：报文不会被记录（不影响 SIP 通话本身）",
			zap.String("event", "gb28181.trace.store_unavailable"),
			zap.String("usedbtype", usedbtype),
			zap.Bool("db_nil", db == nil),
			zap.String("key_env", cfg.EncryptionKeyEnv),
			zap.Error(cipherErr),
		)
		return unavailableStore{}
	}
	return NewReconnectingStore(func(ctx context.Context) (Store, error) {
		if pingErr := db.WithContext(ctx).Exec("SELECT 1").Error; pingErr != nil {
			return nil, fmt.Errorf("connect relational SIP trace store: %w", pingErr)
		}
		if !db.WithContext(ctx).Migrator().HasTable(&gbmodels.GbSipTraceMessage{}) {
			return nil, fmt.Errorf("relational SIP trace table is unavailable")
		}
		return NewRelationalStoreWithRetention(db, cfg.RetentionDays)
	}, 250*time.Millisecond, 30*time.Second)
}

// traceDiagnosisService 装配诊断服务;失败时返回降级健康快照
func traceDiagnosisService(cfg gbconfig.TraceConfig, db *gorm.DB) (*diagnosis.Service, diagnosis.HealthSnapshot) {
	if db == nil {
		return nil, diagnosis.HealthSnapshot{State: diagnosis.HealthDegraded, LastError: diagnosis.ErrRepositoryUnavailable.Error()}
	}
	repository, repositoryErr := diagnosis.NewGormRepository(db)
	if repositoryErr == nil {
		service, _ := diagnosis.NewService(repository, diagnosis.ServiceConfig{
			QueueCapacity: cfg.QueueCapacity, BatchSize: cfg.BatchSize,
			FlushInterval:  time.Duration(cfg.FlushIntervalMS) * time.Millisecond,
			Retention:      time.Duration(cfg.RetentionDays) * 24 * time.Hour,
			PruneBatchSize: DefaultTracePruneBatchSize,
		})
		return service, diagnosis.DisabledHealth()
	}
	return nil, diagnosis.HealthSnapshot{State: diagnosis.HealthDegraded, LastError: repositoryErr.Error()}
}

// startDiagnosisHealthProbe 启动异步诊断表健康探测
func (m *Module) startDiagnosisHealthProbe(db *gorm.DB, service *diagnosis.Service) {
	if db == nil || service == nil {
		return
	}
	if !m.admitProbe() {
		return
	}
	go func() {
		defer m.probeWG.Done()
		ctx, cancel := context.WithTimeout(m.lifecycleCtx, 10*time.Second)
		defer cancel()
		if pingErr := db.WithContext(ctx).Exec("SELECT 1").Error; pingErr != nil {
			service.MarkDegraded(fmt.Errorf("connect diagnosis store: %w", pingErr))
			return
		}
		if !db.WithContext(ctx).Migrator().HasTable(&gbmodels.GbSipTraceSessionDiagnosis{}) {
			service.MarkDegraded(errors.New("diagnosis table is unavailable"))
		}
	}()
}

// startRelationalHealthProbe 启动异步关系型存储健康探测(不阻塞 bootstrap,
// 避免第一次 UI 查询才 dial 的冷启动)
func (m *Module) startRelationalHealthProbe(store Store) {
	rs, ok := store.(*ReconnectingStore)
	if !ok {
		return
	}
	if !m.admitProbe() {
		return
	}
	go func() {
		defer m.probeWG.Done()
		ctx, cancel := context.WithTimeout(m.lifecycleCtx, 10*time.Second)
		defer cancel()
		if _, err := rs.ensure(ctx); err != nil {
			m.health.degraded(err.Error())
		} else {
			m.health.ready(time.Now())
		}
	}()
}

// startRelationalHealthProbe keeps the package-local call shape used by older
// tests while routing ownership through Module's lifecycle.
func startRelationalHealthProbe(store Store, module *Module) {
	if module != nil {
		module.startRelationalHealthProbe(store)
	}
}

func NewModule(cfg gbconfig.TraceConfig, store Store, payloadCipher PayloadCipher) *Module {
	return NewModuleWithDiagnosis(cfg, store, payloadCipher, nil)
}

func NewModuleWithDiagnosis(cfg gbconfig.TraceConfig, store Store, payloadCipher PayloadCipher, diagnosisService *diagnosis.Service) *Module {
	if store == nil {
		store = unavailableStore{}
	}
	if payloadCipher == nil {
		payloadCipher = failingCipher{err: ErrInvalidEncryptionKey}
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	flushInterval := time.Duration(cfg.FlushIntervalMS) * time.Millisecond
	if flushInterval <= 0 {
		flushInterval = 500 * time.Millisecond
	}
	module := &Module{
		framer:            NewFrameAssembler(DefaultMaxFrameBytes),
		store:             store,
		cipher:            payloadCipher,
		health:            newHealthTracker(HealthReady, ""),
		diagnosis:         diagnosisService,
		diagnosisFallback: diagnosis.DisabledHealth(),
		streamHub:         NewStreamHub(),
		batchSize:         batchSize,
		flushInterval:     flushInterval,
		retryMin:          10 * time.Millisecond,
		retryMax:          time.Second,
		now:               time.Now,
		done:              make(chan struct{}),
		retryDone:         make(chan struct{}),
		prunerDone:        make(chan struct{}),
		retryQueue:        make(chan retryBatch, 32),
		cleanupDone:       make(chan struct{}),
		probeDone:         make(chan struct{}),
	}
	module.collector = NewCollector(cfg.QueueCapacity, func() {
		module.health.degraded("trace queue is full; events were dropped")
	})
	ctx, cancel := context.WithCancel(context.Background())
	module.lifecycleCtx = ctx
	module.cancel = cancel
	go module.runWriter(ctx)
	go module.runRetryWriter(ctx)
	// Database retention belongs to the platform scheduler, including while capture is disabled.
	close(module.prunerDone)
	return module
}

func (m *Module) ReadFilter(props sip.TransportReadProps, data []byte) ([]byte, error) {
	if m.closed.Load() || m.framer == nil {
		return data, nil
	}
	m.framer.SweepIdle(m.nowUTC())
	frames := m.framer.Push(props, data)
	for _, frame := range frames {
		m.emitFrame(frame)
	}
	return data, nil
}

func (m *Module) WriteObserver(props sip.TransportWriteProps, data []byte) {
	if m.closed.Load() || m.collector == nil {
		return
	}
	m.collector.Submit(Event{
		OccurredAt: m.nowUTC(),
		Direction:  DirectionOutbound,
		Transport:  props.Transport,
		LocalAddr:  m.resolveAddr(addrString(props.LocalAddr), addrString(props.RemoteAddr)),
		RemoteAddr: addrString(props.RemoteAddr),
		Raw:        data,
	})
}

func (m *Module) ConnectionClosed(props sip.TransportReadProps) {
	if m.framer != nil {
		m.framer.Forget(props)
	}
}

func (m *Module) emitFrame(frame Frame) {
	if m.onFrame != nil {
		func() {
			defer func() { _ = recover() }()
			m.onFrame(frame)
		}()
	}
	if m.collector != nil {
		m.collector.Submit(Event{
			OccurredAt: m.nowUTC(),
			Direction:  DirectionInbound,
			Transport:  frame.Props.Transport,
			LocalAddr:  m.resolveAddr(addrString(frame.Props.LocalAddr), addrString(frame.Props.RemoteAddr)),
			RemoteAddr: addrString(frame.Props.RemoteAddr),
			Raw:        frame.Data,
			Malformed:  frame.Malformed,
			ParseError: frame.Error,
		})
	}
}

func (m *Module) Health() HealthSnapshot {
	if m.health == nil || m.collector == nil {
		return DisabledHealth()
	}
	snapshot := m.health.snapshot()
	snapshot.QueueDepth = m.collector.Depth()
	snapshot.QueueCapacity = m.collector.Capacity()
	snapshot.Dropped = m.collector.Dropped()
	if m.diagnosis != nil {
		snapshot.Diagnosis = m.diagnosis.Health()
	} else {
		snapshot.Diagnosis = m.diagnosisFallback
	}
	return snapshot
}

func (m *Module) DiagnosticSink() diagnosis.DiagnosticSink {
	if m == nil || m.diagnosis == nil {
		return diagnosis.NoopSink{}
	}
	return m.diagnosis
}

func DiagnosisSinkFromRuntime(runtime Runtime) diagnosis.DiagnosticSink {
	provider, ok := runtime.(interface {
		DiagnosticSink() diagnosis.DiagnosticSink
	})
	if !ok {
		return diagnosis.NoopSink{}
	}
	return provider.DiagnosticSink()
}

func (m *Module) Shutdown(ctx context.Context) error {
	if m.done == nil {
		m.closed.Store(true)
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.shutdownOnce.Do(func() {
		m.closed.Store(true)
		m.stopProbeAdmission()
		m.collector.Close()
	})
	m.cleanupOnce.Do(func() { go m.finishShutdown() })
	select {
	case <-m.cleanupDone:
		return m.shutdownErr
	default:
	}
	select {
	case <-m.cleanupDone:
		return m.shutdownErr
	case <-ctx.Done():
		select {
		case <-m.cleanupDone:
			return m.shutdownErr
		default:
		}
		m.cancel()
		return ctx.Err()
	}
}

func (m *Module) finishShutdown() {
	<-m.done
	<-m.retryDone
	m.cancel()
	<-m.prunerDone
	m.waitProbes()
	diagnosisErr := m.stopDiagnosis(context.Background())
	m.shutdownErr = errors.Join(diagnosisErr, m.closeStore())
	close(m.cleanupDone)
}

func (m *Module) admitProbe() bool {
	m.probeMu.Lock()
	defer m.probeMu.Unlock()
	if m.probeStopped {
		return false
	}
	m.probeWG.Add(1)
	return true
}

func (m *Module) stopProbeAdmission() {
	m.probeMu.Lock()
	m.probeStopped = true
	m.probeMu.Unlock()
}

func (m *Module) waitProbes() {
	m.probeWaitOnce.Do(func() {
		go func() {
			m.probeWG.Wait()
			close(m.probeDone)
		}()
	})
	<-m.probeDone
}

func (m *Module) stopDiagnosis(ctx context.Context) error {
	if m.diagnosis == nil {
		return nil
	}
	return m.diagnosis.Stop(ctx)
}

func (m *Module) closeStore() error {
	m.storeCloseOnce.Do(func() {
		if closer, ok := m.store.(interface{ Close() error }); ok {
			m.storeCloseErr = closer.Close()
		}
	})
	return m.storeCloseErr
}

func addrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func (m *Module) nowUTC() time.Time {
	if m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}

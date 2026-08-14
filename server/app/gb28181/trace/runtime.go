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
	"gorm.io/gorm"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/trace/diagnosis"
	"uvplatform.cn/uvp-gb28181/app/global/app"
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
	storeCloseOnce sync.Once
	storeCloseErr  error
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
func (m *Module) resolveAddr(addr string) string {
	if m.platformAddr == "" {
		return addr
	}
	if addr == "" {
		return addr
	}
	// [::]:port / 0.0.0.0:port 都是 dual-stack socket 的通配符表示
	if strings.HasPrefix(addr, "[::]:") || strings.HasPrefix(addr, "0.0.0.0:") {
		return m.platformAddr
	}
	return addr
}

func NewRuntime(cfg gbconfig.TraceConfig) Runtime {
	return NewRuntimeWithDB(cfg, activeTraceDB())
}

func activeTraceDB() *gorm.DB {
	if app.ConfigYml != nil {
		switch strings.ToLower(app.ConfigYml.GetString("gormv2.usedbtype")) {
		case "postgresql":
			return app.GormDbPostgreSql
		case "sqlserver":
			return app.GormDbSqlserver
		case "mysql":
			return app.GormDbMysql
		}
	}
	for _, db := range []*gorm.DB{app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver} {
		if db != nil {
			return db
		}
	}
	return nil
}

func NewRuntimeWithDB(cfg gbconfig.TraceConfig, db *gorm.DB) Runtime {
	loadedCipher, err := LoadCipherFromEnv(cfg.EncryptionKeyEnv, "v1")
	var payloadCipher PayloadCipher = loadedCipher
	if err != nil {
		payloadCipher = failingCipher{err: ErrInvalidEncryptionKey}
	}
	var store Store = unavailableStore{}
	if err == nil && db != nil {
		store = NewReconnectingStore(func(ctx context.Context) (Store, error) {
			if pingErr := db.WithContext(ctx).Exec("SELECT 1").Error; pingErr != nil {
				return nil, fmt.Errorf("connect relational SIP trace store: %w", pingErr)
			}
			if !db.Migrator().HasTable(&gbmodels.GbSipTraceMessage{}) {
				return nil, fmt.Errorf("relational SIP trace table is unavailable")
			}
			return NewRelationalStoreWithRetention(db, cfg.RetentionDays)
		}, 250*time.Millisecond, 30*time.Second)
	}
	var diagnosisService *diagnosis.Service
	diagnosisFallback := diagnosis.DisabledHealth()
	if db != nil {
		repository, repositoryErr := diagnosis.NewGormRepository(db)
		if repositoryErr == nil {
			diagnosisService, repositoryErr = diagnosis.NewService(repository, diagnosis.ServiceConfig{
				QueueCapacity: cfg.QueueCapacity, BatchSize: cfg.BatchSize,
				FlushInterval:  time.Duration(cfg.FlushIntervalMS) * time.Millisecond,
				Retention:      time.Duration(cfg.RetentionDays) * 24 * time.Hour,
				PruneBatchSize: DefaultTracePruneBatchSize,
			})
		}
		if repositoryErr != nil {
			diagnosisFallback = diagnosis.HealthSnapshot{State: diagnosis.HealthDegraded, LastError: repositoryErr.Error()}
		}
	} else {
		diagnosisFallback = diagnosis.HealthSnapshot{State: diagnosis.HealthDegraded, LastError: diagnosis.ErrRepositoryUnavailable.Error()}
	}
	module := NewModuleWithDiagnosis(cfg, store, payloadCipher, diagnosisService)
	module.diagnosisFallback = diagnosisFallback
	if diagnosisService != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if pingErr := db.WithContext(ctx).Exec("SELECT 1").Error; pingErr != nil {
				diagnosisService.MarkDegraded(fmt.Errorf("connect diagnosis store: %w", pingErr))
				return
			}
			if !db.Migrator().HasTable(&gbmodels.GbSipTraceSessionDiagnosis{}) {
				diagnosisService.MarkDegraded(errors.New("diagnosis table is unavailable"))
			}
		}()
	}
	if err != nil {
		module.health.degraded("trace encryption key is unavailable")
	} else {
		module.health.degraded(ErrTraceStoreUnavailable.Error())
		// 启动时主动检查关系型存储(异步,不阻塞 bootstrap),
		// 避免第一次 UI 查询才 dial 的冷启动问题,同时把真实错误(schema 缺失/权限等)透出到 health.lastError
		if rs, ok := store.(*ReconnectingStore); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := rs.ensure(ctx); err != nil {
					module.health.degraded(err.Error())
				} else {
					module.health.ready(time.Now())
				}
			}()
		}
	}
	return module
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
	}
	module.collector = NewCollector(cfg.QueueCapacity, func() {
		module.health.degraded("trace queue is full; events were dropped")
	})
	ctx, cancel := context.WithCancel(context.Background())
	module.cancel = cancel
	go module.runWriter(ctx)
	go module.runRetryWriter(ctx)
	if prunable, ok := store.(PrunableStore); ok {
		var scheduledPruner PrunableStore = prunable
		if diagnosisService != nil {
			scheduledPruner = combinedPrunableStore{trace: prunable, diagnosis: diagnosisService}
		}
		pruner := NewTracePruner(scheduledPruner, cfg.RetentionDays, DefaultTracePruneBatchSize, time.Now)
		go func() {
			defer close(module.prunerDone)
			pruner.Run(ctx, time.Hour, func(err error) { module.health.degraded(err.Error()) })
		}()
	} else {
		close(module.prunerDone)
	}
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
		LocalAddr:  m.resolveAddr(addrString(props.LocalAddr)),
		RemoteAddr: m.resolveAddr(addrString(props.RemoteAddr)),
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
			LocalAddr:  m.resolveAddr(addrString(frame.Props.LocalAddr)),
			RemoteAddr: m.resolveAddr(addrString(frame.Props.RemoteAddr)),
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
	m.shutdownOnce.Do(func() {
		m.closed.Store(true)
		m.collector.Close()
	})
	select {
	case <-m.done:
		select {
		case <-m.retryDone:
			m.cancel()
			<-m.prunerDone
			if err := m.stopDiagnosis(ctx); err != nil {
				_ = m.closeStore()
				return err
			}
			return m.closeStore()
		case <-ctx.Done():
			m.cancel()
			<-m.retryDone
			<-m.prunerDone
			_ = m.stopDiagnosis(ctx)
			_ = m.closeStore()
			return ctx.Err()
		}
	case <-ctx.Done():
		m.cancel()
		<-m.done
		<-m.retryDone
		<-m.prunerDone
		_ = m.stopDiagnosis(ctx)
		_ = m.closeStore()
		return ctx.Err()
	}
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

package trace

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo/sip"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
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
	closed        atomic.Bool
	framer        *FrameAssembler
	onFrame       func(Frame)
	collector     *Collector
	store         Store
	cipher        PayloadCipher
	health        *healthTracker
	batchSize     int
	flushInterval time.Duration
	retryMin      time.Duration
	retryMax      time.Duration
	now           func() time.Time
	cancel        context.CancelFunc
	done          chan struct{}
	shutdownOnce  sync.Once
}

func NewRuntime(cfg gbconfig.TraceConfig) Runtime {
	loadedCipher, err := LoadCipherFromEnv(cfg.EncryptionKeyEnv, "v1")
	var payloadCipher PayloadCipher = loadedCipher
	if err != nil {
		payloadCipher = failingCipher{err: ErrInvalidEncryptionKey}
	}
	module := NewModule(cfg, unavailableStore{}, payloadCipher)
	if err != nil {
		module.health.degraded("trace encryption key is unavailable")
	} else {
		module.health.degraded("trace store is not configured")
	}
	return module
}

func NewModule(cfg gbconfig.TraceConfig, store Store, payloadCipher PayloadCipher) *Module {
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
		framer:        NewFrameAssembler(DefaultMaxFrameBytes),
		store:         store,
		cipher:        payloadCipher,
		health:        newHealthTracker(HealthReady, ""),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		retryMin:      10 * time.Millisecond,
		retryMax:      time.Second,
		now:           time.Now,
		done:          make(chan struct{}),
	}
	module.collector = NewCollector(cfg.QueueCapacity, func() {
		module.health.degraded("trace queue is full; events were dropped")
	})
	ctx, cancel := context.WithCancel(context.Background())
	module.cancel = cancel
	go module.runWriter(ctx)
	return module
}

func (m *Module) ReadFilter(props sip.TransportReadProps, data []byte) ([]byte, error) {
	if m.closed.Load() || m.framer == nil {
		return data, nil
	}
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
		OccurredAt: m.now().UTC(),
		Direction:  DirectionOutbound,
		Transport:  props.Transport,
		LocalAddr:  addrString(props.LocalAddr),
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
			OccurredAt: m.now().UTC(),
			Direction:  DirectionInbound,
			Transport:  frame.Props.Transport,
			LocalAddr:  addrString(frame.Props.LocalAddr),
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
	return snapshot
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
		return nil
	case <-ctx.Done():
		m.cancel()
		<-m.done
		return ctx.Err()
	}
}

func addrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

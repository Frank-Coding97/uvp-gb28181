package trace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

type recordingStore struct {
	mu        sync.Mutex
	events    []Event
	err       error
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	panicOnce atomic.Bool
}

func (s *recordingStore) InsertBatch(ctx context.Context, events []Event) error {
	if s.panicOnce.CompareAndSwap(true, false) {
		panic("store panic")
	}
	if s.started != nil {
		s.startOnce.Do(func() { close(s.started) })
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	for _, event := range events {
		event.Raw = append([]byte(nil), event.Raw...)
		s.events = append(s.events, event)
	}
	s.mu.Unlock()
	return nil
}

func (s *recordingStore) snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

func TestCollectorQueueFullNeverBlocksProducer(t *testing.T) {
	store := &recordingStore{started: make(chan struct{}), release: make(chan struct{})}
	module := NewModule(testTraceConfig(1, 1, 5), store)

	module.WriteObserver(testWriteProps(), []byte("first"))
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start store call")
	}
	module.WriteObserver(testWriteProps(), []byte("queued"))
	startedAt := time.Now()
	module.WriteObserver(testWriteProps(), []byte("dropped"))
	require.Less(t, time.Since(startedAt), 50*time.Millisecond)
	require.EqualValues(t, 1, module.Health().Dropped)
	require.Equal(t, HealthDegraded, module.Health().State)

	close(store.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
}

func TestWriterRecoversStorePanicAndRetries(t *testing.T) {
	store := &recordingStore{}
	store.panicOnce.Store(true)
	module := NewModule(testTraceConfig(4, 1, 5), store)

	module.WriteObserver(testWriteProps(), []byte("retry-me"))
	require.Eventually(t, func() bool {
		return len(store.snapshot()) == 1 && module.Health().State == HealthReady
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestWriterFailureIsVisibleAndShutdownCancelsRetry(t *testing.T) {
	store := &recordingStore{err: errors.New("clickhouse unavailable")}
	module := NewModule(testTraceConfig(4, 1, 5), store)
	module.WriteObserver(testWriteProps(), []byte("fail"))

	require.Eventually(t, func() bool {
		health := module.Health()
		return health.State == HealthDegraded && health.LastError == "clickhouse unavailable"
	}, time.Second, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, module.Shutdown(ctx), context.DeadlineExceeded)
	select {
	case <-module.done:
	case <-time.After(time.Second):
		t.Fatal("writer goroutine did not stop after shutdown cancellation")
	}
}

func TestShutdownDrainsPendingBatch(t *testing.T) {
	store := &recordingStore{}
	module := NewModule(testTraceConfig(8, 10, 60_000), store)
	module.WriteObserver(testWriteProps(), []byte("one"))
	module.WriteObserver(testWriteProps(), []byte("two"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
	require.Len(t, store.snapshot(), 2)
}

func TestRuntimeEmitsInboundAndOutboundEvents(t *testing.T) {
	store := &recordingStore{}
	module := NewModule(testTraceConfig(8, 2, 5), store)
	readProps := testReadProps("UDP", 5060, 15060)
	rawIn := sipMessage("MESSAGE", "inbound", nil)
	rawOut := []byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n")

	filtered, err := module.ReadFilter(readProps, rawIn)
	require.NoError(t, err)
	require.Equal(t, rawIn, filtered)
	module.WriteObserver(testWriteProps(), rawOut)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 2 }, time.Second, 10*time.Millisecond)
	events := store.snapshot()
	require.Equal(t, DirectionInbound, events[0].Direction)
	require.Equal(t, rawIn, events[0].Raw)
	require.Equal(t, DirectionOutbound, events[1].Direction)
	require.Equal(t, rawOut, events[1].Raw)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestHealthStatesCoverDisabledDegradedAndReady(t *testing.T) {
	require.Equal(t, HealthDisabled, DisabledHealth().State)
	degraded := NewRuntime(gbconfig.TraceConfig{Enabled: true}).(*Module)
	require.Equal(t, HealthDegraded, degraded.Health().State)
	require.NoError(t, degraded.Shutdown(context.Background()))

	ready := NewModule(testTraceConfig(4, 1, 5), &recordingStore{})
	ready.WriteObserver(testWriteProps(), []byte("ok"))
	require.Eventually(t, func() bool { return ready.Health().State == HealthReady }, time.Second, 10*time.Millisecond)
	require.NoError(t, ready.Shutdown(context.Background()))
}

func testTraceConfig(queueCapacity, batchSize, flushMS int) gbconfig.TraceConfig {
	return gbconfig.TraceConfig{
		Enabled:         true,
		QueueCapacity:   queueCapacity,
		BatchSize:       batchSize,
		FlushIntervalMS: flushMS,
	}
}

func testWriteProps() sip.TransportWriteProps {
	return sip.TransportWriteProps{
		Transport:  "UDP",
		LocalAddr:  testReadProps("UDP", 5060, 15060).LocalAddr,
		RemoteAddr: testReadProps("UDP", 5060, 15060).RemoteAddr,
	}
}

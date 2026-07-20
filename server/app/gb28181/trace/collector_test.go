package trace

import (
	"context"
	"errors"
	"strings"
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
	events    []StoredEvent
	err       error
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	panicOnce atomic.Bool
}

func (s *recordingStore) InsertBatch(ctx context.Context, events []StoredEvent) error {
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
	s.mu.Lock()
	err := s.err
	if err == nil {
		s.events = append(s.events, events...)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

func (s *recordingStore) snapshot() []StoredEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StoredEvent(nil), s.events...)
}

func TestCollectorQueueFullNeverBlocksProducer(t *testing.T) {
	store := &recordingStore{started: make(chan struct{}), release: make(chan struct{})}
	module := NewModule(testTraceConfig(1, 1, 5), store, testPayloadCipher())

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
	module := NewModule(testTraceConfig(4, 1, 5), store, testPayloadCipher())

	module.WriteObserver(testWriteProps(), []byte("retry-me"))
	require.Eventually(t, func() bool {
		return len(store.snapshot()) == 1 && module.Health().State == HealthReady
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestWriterFailureIsVisibleAndShutdownCancelsRetry(t *testing.T) {
	store := &recordingStore{err: errors.New("clickhouse unavailable")}
	module := NewModule(testTraceConfig(4, 1, 5), store, testPayloadCipher())
	module.WriteObserver(testWriteProps(), []byte("fail"))

	require.Eventually(t, func() bool {
		health := module.Health()
		return health.State == HealthDegraded && health.LastError == "clickhouse unavailable"
	}, time.Second, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
	select {
	case <-module.done:
	case <-time.After(time.Second):
		t.Fatal("writer goroutine did not stop after shutdown cancellation")
	}
}

func TestWriterFailureDoesNotStopConsumingLaterEvents(t *testing.T) {
	store := &recordingStore{err: errors.New("clickhouse unavailable")}
	module := NewModule(testTraceConfig(16, 1, 1), store, testPayloadCipher())
	for i := 0; i < 8; i++ {
		module.WriteObserver(testWriteProps(), []byte("event"))
	}
	require.Eventually(t, func() bool {
		health := module.Health()
		return health.CurrentGap != nil && health.CurrentGap.EventCount >= 8
	}, time.Second, 10*time.Millisecond)
	require.Less(t, module.Health().QueueDepth, module.Health().QueueCapacity)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
}

func TestHealthGapClosesAfterRetryRecovery(t *testing.T) {
	store := &recordingStore{}
	store.err = errors.New("temporary outage")
	module := NewModule(testTraceConfig(8, 1, 1), store, testPayloadCipher())
	module.WriteObserver(testWriteProps(), []byte("recover"))
	require.Eventually(t, func() bool { return module.Health().CurrentGap != nil }, time.Second, 10*time.Millisecond)
	store.mu.Lock()
	store.err = nil
	store.mu.Unlock()
	require.Eventually(t, func() bool {
		health := module.Health()
		return health.State == HealthReady && health.CurrentGap == nil && health.LastGap != nil
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestShutdownDrainsPendingBatch(t *testing.T) {
	store := &recordingStore{}
	module := NewModule(testTraceConfig(8, 10, 60_000), store, testPayloadCipher())
	module.WriteObserver(testWriteProps(), []byte("one"))
	module.WriteObserver(testWriteProps(), []byte("two"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, module.Shutdown(ctx))
	require.Len(t, store.snapshot(), 2)
}

func TestRuntimeEmitsInboundAndOutboundEvents(t *testing.T) {
	store := &recordingStore{}
	cipher := testPayloadCipher()
	module := NewModule(testTraceConfig(8, 2, 5), store, cipher)
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
	decrypted, err := cipher.Decrypt(events[0].Payload)
	require.NoError(t, err)
	require.Equal(t, rawIn, decrypted)
	require.Equal(t, DirectionOutbound, events[1].Direction)
	decrypted, err = cipher.Decrypt(events[1].Payload)
	require.NoError(t, err)
	require.Equal(t, rawOut, decrypted)
	require.NoError(t, module.Shutdown(context.Background()))
}

func TestHealthStatesCoverDisabledDegradedAndReady(t *testing.T) {
	require.Equal(t, HealthDisabled, DisabledHealth().State)
	degraded := NewRuntime(gbconfig.TraceConfig{Enabled: true}).(*Module)
	require.Equal(t, HealthDegraded, degraded.Health().State)
	require.NoError(t, degraded.Shutdown(context.Background()))

	ready := NewModule(testTraceConfig(4, 1, 5), &recordingStore{}, testPayloadCipher())
	ready.WriteObserver(testWriteProps(), []byte("ok"))
	require.Eventually(t, func() bool { return ready.Health().State == HealthReady }, time.Second, 10*time.Millisecond)
	require.NoError(t, ready.Shutdown(context.Background()))
}

func testPayloadCipher() *Cipher {
	cipher, err := NewCipher([]byte(strings.Repeat("t", 32)), "test-v1")
	if err != nil {
		panic(err)
	}
	return cipher
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

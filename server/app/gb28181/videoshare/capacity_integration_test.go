package videoshare

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// TestVideoShareCapacitySample is a bounded resource-lifecycle sample, not a
// throughput, maximum-viewer, ZLM, browser, or production capacity commitment.
// Run explicitly: UVP_VIDEO_SHARE_CAPACITY=1 go test -race \
// ./app/gb28181/videoshare -run '^TestVideoShareCapacitySample$' -v -count=1 -timeout=45s
func TestVideoShareCapacitySample(t *testing.T) {
	if os.Getenv("UVP_VIDEO_SHARE_CAPACITY") != "1" {
		t.Skip("set UVP_VIDEO_SHARE_CAPACITY=1 to run the bounded 16-viewer media sample")
	}
	const viewersPerRound, slowViewers = 16, 4
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var sourceActive, relayActive, leaseActive atomic.Int64
	var acquired, released, duplicateReleases atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceActive.Add(1)
		defer sourceActive.Add(-1)
		w.Header().Set("Content-Type", "video/x-flv")
		control := http.NewResponseController(w)
		if _, err := w.Write([]byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}); err != nil {
			return
		}
		if err := control.Flush(); err != nil {
			return
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		frame := bytes.Repeat([]byte{0x56}, 512)
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_ = control.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if _, err := w.Write(frame); err != nil {
					return
				}
				if err := control.Flush(); err != nil {
					return
				}
			}
		}
	}))
	defer func() { upstream.CloseClientConnections(); upstream.Close() }()
	service, channelPK, _ := newServiceFixture(t)
	// Real request cancellation may discard a database/sql connection. A bare
	// :memory: fixture then loses its schema on reconnect; persist that exact
	// fixture to a disposable SQLite file for these repeated socket exits.
	fixturePath := filepath.Join(t.TempDir(), "capacity.db")
	require.NoError(t, service.db.Exec("VACUUM INTO ?", fixturePath).Error)
	durableDB, openErr := gorm.Open(sqlite.Open(fixturePath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, openErr)
	durableSQL, openErr := durableDB.DB()
	require.NoError(t, openErr)
	durableSQL.SetMaxOpenConns(1)
	defer durableSQL.Close()
	service.db = durableDB
	service.now = time.Now
	// Keep the production random source behavior while reusing the existing DB
	// fixture: its deterministic byte sequence is inappropriate for load samples.
	service.random = func(dst []byte) error { _, err := rand.Read(dst); return err }
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		acquired.Add(1)
		leaseActive.Add(1)
		var closed atomic.Bool
		return PlaybackSource{StreamID: "capacity-sample", Generation: 1, HTTPFLVURL: upstream.URL, Release: func() {
			if !closed.CompareAndSwap(false, true) {
				duplicateReleases.Add(1)
				return
			}
			released.Add(1)
			leaseActive.Add(-1)
		}}, nil
	}))
	defer service.DrainViewers()
	link, err := service.SetEnabled(ctx, channelPK, 9, true, 0)
	require.NoError(t, err)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/public/video-share/viewers/"
		if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, "/live.flv") {
			http.NotFound(w, r)
			return
		}
		token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), "/live.flv")
		relayActive.Add(1)
		defer relayActive.Add(-1)
		service.RelayHTTPFLV(r.Context(), w, token)
	}))
	defer func() { backend.CloseClientConnections(); backend.Close() }()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxConnsPerHost: viewersPerRound}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	defer cancel() // Cancel active sockets before the server Close defers run.
	viewerCount := func() int { service.viewerMu.Lock(); defer service.viewerMu.Unlock(); return len(service.viewers) }
	quiescent := func() bool {
		return viewerCount() == 0 && leaseActive.Load() == 0 && relayActive.Load() == 0 && sourceActive.Load() == 0
	}
	runtime.GC()
	var before, peak, after runtime.MemStats
	runtime.ReadMemStats(&before)
	baselineGoroutines, peakGoroutines := runtime.NumGoroutine(), runtime.NumGoroutine()
	started := time.Now()
	var totalBytes, totalSlowBytes, totalNormalBytes int64
	var sustained time.Duration
	for round, duration := range []time.Duration{10 * time.Second, time.Second, time.Second} {
		roundCtx, stopRound := context.WithCancel(ctx)
		defer stopRound()
		type observation struct {
			bytes int64
			slow  bool
			err   error
		}
		ready := make(chan error, viewersPerRound)
		finished := make(chan observation, viewersPerRound)
		for index := 0; index < viewersPerRound; index++ {
			go func(slow bool) {
				result := observation{slow: slow}
				defer func() { finished <- result }()
				viewer, createErr := service.CreateViewer(roundCtx, link.PublicID, link.Signature)
				if createErr != nil {
					result.err = createErr
					ready <- createErr
					return
				}
				defer service.CloseViewer(viewer.Token)
				request, requestErr := http.NewRequestWithContext(roundCtx, http.MethodGet, backend.URL+viewer.HTTPFLVPath, nil)
				if requestErr != nil {
					result.err = requestErr
					ready <- requestErr
					return
				}
				response, requestErr := client.Do(request)
				if requestErr != nil {
					result.err = requestErr
					ready <- requestErr
					return
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK {
					result.err = fmt.Errorf("relay status %d", response.StatusCode)
					ready <- result.err
					return
				}
				header := make([]byte, 13)
				n, readErr := io.ReadFull(response.Body, header)
				result.bytes += int64(n)
				if readErr != nil || string(header[:3]) != "FLV" {
					result.err = fmt.Errorf("invalid initial FLV response: %w", readErr)
					ready <- result.err
					return
				}
				ready <- nil
				buffer := make([]byte, 4096)
				if slow {
					buffer = buffer[:1024]
				}
				for {
					n, readErr = response.Body.Read(buffer)
					result.bytes += int64(n)
					if readErr != nil {
						if roundCtx.Err() == nil {
							result.err = fmt.Errorf("stream ended before round stop: %w", readErr)
						}
						return
					}
					if slow {
						timer := time.NewTimer(40 * time.Millisecond)
						select {
						case <-roundCtx.Done():
							timer.Stop()
							return
						case <-timer.C:
						}
					}
				}
			}(index < slowViewers)
		}
		for index := 0; index < viewersPerRound; index++ {
			select {
			case readyErr := <-ready:
				if readyErr != nil {
					stopRound()
					t.Fatalf("round %d viewer did not start: %v", round+1, readyErr)
				}
			case <-ctx.Done():
				stopRound()
				t.Fatal("capacity sample exceeded its 35s bound while starting viewers")
			}
		}
		require.Equal(t, viewersPerRound, viewerCount(), "all independent viewers must coexist")
		holdStarted := time.Now()
		timer := time.NewTimer(duration)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			stopRound()
			t.Fatal("capacity sample exceeded its 35s bound while consuming")
		}
		held := time.Since(holdStarted)
		sustained += held
		require.Equal(t, viewersPerRound, viewerCount(), "slow/normal viewers must remain alive throughout the sample")
		require.EqualValues(t, viewersPerRound, relayActive.Load())
		runtime.ReadMemStats(&peak)
		if current := runtime.NumGoroutine(); current > peakGoroutines {
			peakGoroutines = current
		}
		stopRound()
		var roundBytes int64
		for index := 0; index < viewersPerRound; index++ {
			select {
			case result := <-finished:
				require.NoError(t, result.err)
				require.Greater(t, result.bytes, int64(13), "every client must consume media beyond the FLV header")
				roundBytes += result.bytes
				if result.slow {
					totalSlowBytes += result.bytes
				} else {
					totalNormalBytes += result.bytes
				}
			case <-ctx.Done():
				t.Fatal("capacity sample exceeded its 35s bound while closing clients")
			}
		}
		totalBytes += roundBytes
		require.Eventually(t, quiescent, 2*time.Second, 10*time.Millisecond, "each round must return all viewers, leases, relays and upstream requests to zero")
		require.Equal(t, acquired.Load(), released.Load())
		require.Zero(t, duplicateReleases.Load())
		t.Logf("round=%d concurrent_viewers=%d slow_viewers=%d sustained=%s received_bytes=%d prepare_acquired=%d prepare_released=%d active_viewers=%d active_leases=%d active_relays=%d active_sources=%d", round+1, viewersPerRound, slowViewers, held, roundBytes, acquired.Load(), released.Load(), viewerCount(), leaseActive.Load(), relayActive.Load(), sourceActive.Load())
	}
	transport.CloseIdleConnections()
	flvHTTPClient.CloseIdleConnections()
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= baselineGoroutines+4 }, 2*time.Second, 20*time.Millisecond, "client and relay goroutines should return close to baseline")
	runtime.GC()
	runtime.ReadMemStats(&after)
	elapsed := time.Since(started)
	require.GreaterOrEqual(t, sustained, 12*time.Second)
	require.Less(t, elapsed, 35*time.Second)
	require.Greater(t, acquired.Load(), int64(viewersPerRound*3-1))
	require.Equal(t, acquired.Load(), released.Load())
	require.True(t, quiescent())
	t.Logf("sample_only=true source_payload_bytes=512 source_tick_ms=20 slow_read_bytes=1024 slow_pause_ms=40 rounds=3 elapsed=%s sustained=%s bytes=%d normal_bytes=%d slow_bytes=%d heap_before=%d heap_last_round=%d heap_after_gc=%d total_alloc_delta=%d goroutines_before=%d goroutines_peak=%d goroutines_after=%d prepares=%d releases=%d; fixture/socket lifecycle evidence only, not a production capacity promise", elapsed, sustained, totalBytes, totalNormalBytes, totalSlowBytes, before.HeapAlloc, peak.HeapAlloc, after.HeapAlloc, after.TotalAlloc-before.TotalAlloc, baselineGoroutines, peakGoroutines, runtime.NumGoroutine(), acquired.Load(), released.Load())
}

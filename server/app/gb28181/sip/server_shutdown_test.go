package sip

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	siplib "github.com/emiago/sipgo/sip"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
)

func TestServerShutdownUnstartedClosesOwnedTransport(t *testing.T) {
	server, err := NewServer(testConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.ua.Close() })
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	client, err := sipgo.NewClient(server.ua)
	require.NoError(t, err)
	request := siplib.NewRequest(siplib.OPTIONS, siplib.Uri{Scheme: "sip", Host: "127.0.0.1"})
	request.SetTransport("UDP")
	request.SetDestination(peer.LocalAddr().String())
	tx, err := client.TransactionRequest(context.Background(), request)
	require.NoError(t, err)
	defer tx.Terminate()
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = peer.ReadFrom(make([]byte, 4096))
	require.NoError(t, err)
	require.NoError(t, server.Shutdown(context.Background()))
	select {
	case <-tx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("an unstarted Server still owns its UA transaction/transport")
	}
	require.Error(t, server.Start(), "a shutdown instance cannot be restarted")
}

type retryShutdownTrace struct {
	lifecycleTraceRuntime
	calls int
}

func (r *retryShutdownTrace) Shutdown(context.Context) error {
	r.calls++
	if r.calls <= 2 {
		return errors.New("fixture trace not drained")
	}
	return nil
}

func TestServerShutdownDoesNotForgetTraceFailure(t *testing.T) {
	cfg := testConfig()
	cfg.Trace.Enabled = true
	runtime := &retryShutdownTrace{}
	server, err := NewServer(cfg, WithTraceFactory(func(gbconfig.TraceConfig) gbtrace.Runtime { return runtime }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.ua.Close() })
	require.Error(t, server.Shutdown(context.Background()))
	require.Error(t, server.Shutdown(context.Background()), "a once-local error cannot disappear on retry")
	require.Error(t, server.Shutdown(context.Background()))
	require.Equal(t, 1, runtime.calls, "an uncertain first close must remain sticky")
}

func TestServerConcurrentStartAndShutdownCannotRestart(t *testing.T) {
	cfg := testConfig()
	cfg.SIP.Transport = nil // Lifecycle contention; actual listener case is below.
	server, err := NewServer(cfg)
	require.NoError(t, err)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _ = server.Start() }()
		go func() { defer wg.Done(); <-start; results <- server.Shutdown(context.Background()) }()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Error(t, server.Start())
}

func TestServerShutdownRetainsListenersUntilRecoveryWorkerJoins(t *testing.T) {
	cfg := testConfig()
	// An ephemeral TCP port alone says nothing about UDP availability.
	for attempt := 0; ; attempt++ {
		require.Less(t, attempt, 20)
		port, err := net.Listen("tcp4", "127.0.0.1:0")
		require.NoError(t, err)
		udp, udpErr := net.ListenPacket("udp4", port.Addr().String())
		cfg.SIP.Port = port.Addr().(*net.TCPAddr).Port
		require.NoError(t, port.Close())
		if udpErr == nil {
			require.NoError(t, udp.Close())
			break
		}
	}
	server, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.ua.Close() })
	require.NoError(t, server.Start())
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shutdown.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	require.NoError(t, db.Exec("CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME)").Error)
	entered, released := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("fixture:blocked-discovery", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-released // Model an actual DB driver which has not returned yet.
		}
	}))
	defer func() { release(); _ = server.Shutdown(context.Background()) }()
	store := playauth.NewDeviceOperationIntentStore(db)
	devices := playauth.NewDeviceCleanupStore(db)
	barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))
	stop, err := server.UAC().StartPlaybackRecovery(context.Background(), devices, store, barrier, nil)
	require.NoError(t, err)
	defer func() { release(); _ = stop(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker never entered discovery SQL")
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.SIP.Port))
	require.Eventually(t, func() bool {
		return server.ua.TransportLayer().GetListenPort("udp") == cfg.SIP.Port && server.ua.TransportLayer().GetListenPort("tcp") == cfg.SIP.Port
	}, time.Second, time.Millisecond, "both listeners must belong to this actual UA")
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp4", address, 30*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, server.Shutdown(ctx), context.DeadlineExceeded)
	conn, err := net.DialTimeout("tcp4", address, time.Second)
	require.NoError(t, err, "shared reception must survive a failed persistent drain")
	_ = conn.Close()
	udp, err := net.ListenPacket("udp4", address)
	if err == nil {
		_ = udp.Close()
		t.Fatal("shared UDP listener was closed before worker joined")
	}
	_, err = server.UAC().StartPlaybackRecovery(context.Background(), devices, store, barrier, nil)
	require.Error(t, err)
	release()
	require.NoError(t, server.Shutdown(context.Background()))
	conn, err = net.DialTimeout("tcp4", address, 50*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("TCP listener remained after successful shutdown")
	}
	udp, err = net.ListenPacket("udp4", address)
	require.NoError(t, err)
	_ = udp.Close()
	require.Error(t, server.Start())
}

package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type qrRedisProcess struct {
	cmd      *exec.Cmd
	raw      *redis.Client
	addr     string
	password string
	dir      string
}

func startQRRedis(t *testing.T) *qrRedisProcess {
	t.Helper()
	binary := os.Getenv("UVP_T13_REDIS_SERVER")
	if binary == "" {
		binary = "/opt/homebrew/bin/redis-server"
	}
	if _, err := os.Stat(binary); err != nil {
		if resolved, lookErr := exec.LookPath(binary); lookErr == nil {
			binary = resolved
		} else {
			t.Skipf("T13 real Redis QR test requires redis-server (%s)", binary)
		}
	}

	dir := t.TempDir()
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := portListener.Addr().(*net.TCPAddr).Port
	require.NoError(t, portListener.Close())

	passwordBytes := make([]byte, 12)
	_, err = rand.Read(passwordBytes)
	require.NoError(t, err)
	password := "t13-" + hex.EncodeToString(passwordBytes)
	configPath := filepath.Join(dir, "redis.conf")
	config := fmt.Sprintf("bind 127.0.0.1\nport %d\nprotected-mode yes\ndaemonize no\nsupervised no\ndir %s\ndbfilename %s\nappendonly yes\nappendfsync always\nsave \"\"\nrequirepass %s\nmaxmemory-policy noeviction\nlogfile %s\n",
		port, strconv.Quote(dir), strconv.Quote("dump.rdb"), strconv.Quote(password), strconv.Quote(filepath.Join(dir, "redis.log")))
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cmd := exec.Command(binary, configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	raw := redis.NewClient(&redis.Options{Addr: addr, Password: password, DialTimeout: 250 * time.Millisecond, ReadTimeout: 250 * time.Millisecond, WriteTimeout: 250 * time.Millisecond})
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		err = raw.Ping(ctx).Err()
		cancel()
		if err == nil {
			server := &qrRedisProcess{cmd: cmd, raw: raw, addr: addr, password: password, dir: dir}
			t.Cleanup(server.stop)
			return server
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = raw.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatalf("isolated Redis did not become ready")
	return nil
}

func (s *qrRedisProcess) stop() {
	if s == nil || s.cmd == nil {
		return
	}
	if s.raw != nil {
		_ = s.raw.Close()
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd = nil
}

func newQRRedisService(t *testing.T, server *qrRedisProcess) (*QRService, *gorm.DB, app.CacheInterf) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "qr.sqlite")
	db, err := gormhelper.NewSQLiteClient(dbPath)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	cancel()
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	seedSIPConfig(t, db, "Str0ng!Passw0rd#2026")

	cache, err := cachehelper.NewRedisHelper(server.addr, server.password, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Close() })

	svc := NewQRService(cache, NewSIPConfigService(db), func() []string { return []string{"udp"} })
	svc.networkProvider = fakeInterfaceProvider{
		interfaces: []net.Interface{{Index: 1, Name: "en0", Flags: net.FlagUp}},
		addresses:  map[int][]net.Addr{1: {mustCIDR(t, "192.168.1.10/24")}},
	}
	return svc, db, cache
}

func TestQRServiceRealRedisValidExpiredAndMissingTokens(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		server := startQRRedis(t)
		svc, _, _ := newQRRedisService(t, server)
		token, _, err := svc.GenerateToken(context.Background())
		require.NoError(t, err)

		payload, err := svc.Exchange(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, "Str0ng!Passw0rd#2026", payload.Password)
	})

	t.Run("expired", func(t *testing.T) {
		server := startQRRedis(t)
		svc, _, _ := newQRRedisService(t, server)
		svc.ttl = 120 * time.Millisecond
		token, _, err := svc.GenerateToken(context.Background())
		require.NoError(t, err)
		time.Sleep(250 * time.Millisecond)

		payload, err := svc.Exchange(context.Background(), token)
		require.Nil(t, payload)
		require.ErrorIs(t, err, ErrTokenInvalid)
	})

	t.Run("missing", func(t *testing.T) {
		server := startQRRedis(t)
		svc, _, _ := newQRRedisService(t, server)
		payload, err := svc.Exchange(context.Background(), strings.Repeat("A", qrTokenLen))
		require.Nil(t, payload)
		require.ErrorIs(t, err, ErrTokenInvalid)
	})
}

func TestQRServiceRealRedisConcurrentExchangeExactlyOnce(t *testing.T) {
	server := startQRRedis(t)
	svc, _, _ := newQRRedisService(t, server)
	svc.limiter = rate.NewLimiter(rate.Inf, 20)
	token, _, err := svc.GenerateToken(context.Background())
	require.NoError(t, err)

	const consumers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes, invalid, other := 0, 0, []error{}
	start := make(chan struct{})
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			payload, err := svc.Exchange(context.Background(), token)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				if payload == nil || payload.Password != "Str0ng!Passw0rd#2026" {
					other = append(other, errors.New("successful QR exchange returned invalid payload"))
				}
			case errors.Is(err, ErrTokenInvalid):
				invalid++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, other)
	require.Equal(t, 1, successes)
	require.Equal(t, consumers-1, invalid)
}

func TestQRServiceRealRedisFailureDoesNotReturnSIPCredentials(t *testing.T) {
	server := startQRRedis(t)
	svc, _, _ := newQRRedisService(t, server)
	token, _, err := svc.GenerateToken(context.Background())
	require.NoError(t, err)
	server.stop()

	payload, err := svc.Exchange(context.Background(), token)
	require.Nil(t, payload)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "Str0ng!Passw0rd#2026")
}

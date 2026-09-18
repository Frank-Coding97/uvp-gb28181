package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
)

type shutdownHTTPConfig struct {
	app.YmlConfigInterf
	address string
}

func (c shutdownHTTPConfig) GetString(key string) string {
	if key == "httpserver.port" {
		return c.address
	}
	return ""
}
func (shutdownHTTPConfig) GetInt(string) int   { return 0 }
func (shutdownHTTPConfig) GetBool(string) bool { return false }

// Explicit source-file invocation excludes main.go and its production bootstrap
// init. The child uses actual StartServer, SIP Server, and recovery worker, with
// a dedicated SQLite DB; this is not a full configured API restart acceptance.
func TestHTTPBindFailureProcessRetainsPersistentSIPWorker(t *testing.T) {
	if os.Getenv("UVP_ROOT_SHUTDOWN_CHILD") == "1" {
		runShutdownChild(t)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPBindFailureProcessRetainsPersistentSIPWorker$")
	cmd.Env = append(os.Environ(), "UVP_ROOT_SHUTDOWN_CHILD=1", "UVP_ROOT_SHUTDOWN_DB="+filepath.Join(t.TempDir(), "root.sqlite"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer outputWriter.Close()
	cmd.Stdout = outputWriter
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	outputWriter.Close()
	events := make(chan string, 32)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			events <- scanner.Text()
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waitEvent := func(want string) {
		t.Helper()
		for {
			select {
			case got, ok := <-events:
				if !ok {
					t.Fatalf("child exited before %s", want)
				}
				if got == want {
					return
				}
			case <-ctx.Done():
				t.Fatalf("child did not reach %s", want)
			}
		}
	}
	waitEvent("ROOT_HELD")
	select {
	case err := <-done:
		t.Fatalf("process exited while persistent worker was blocked: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := io.WriteString(input, "release\n"); err != nil {
		t.Fatal(err)
	}
	waitEvent("ROOT_DRAINED_WITH_BIND_ERROR")
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 23 {
			t.Fatalf("original bind error exit lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("process remained after actual cleanup")
	}
}

func runShutdownChild(t *testing.T) {
	app.ZapLog = zap.NewNop()
	gin.SetMode(gin.ReleaseMode)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	app.ConfigYml = shutdownHTTPConfig{address: occupied.Addr().String()}
	db, err := gorm.Open(sqlite.Open(os.Getenv("UVP_ROOT_SHUTDOWN_DB")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := db.Exec("CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce sync.Once
	if err := db.Callback().Row().Before("gorm:row").Register("fixture:root-blocked-discovery", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" {
			enteredOnce.Do(func() { close(entered) })
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	server, err := gbsip.NewServer(gbconfig.Config{SIP: gbconfig.SIPConfig{ListenIP: "127.0.0.1", AdvertiseIP: "127.0.0.1", Domain: "3402000000", ServerID: "34020000002000000001"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	stop, err := server.UAC().StartPlaybackRecovery(context.Background(), playauth.NewDeviceCleanupStore(db), playauth.NewDeviceOperationIntentStore(db), playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stop(context.Background()) }()
	<-entered
	serveErr := ginhelper.StartServer(gin.New(), func(context.Context) error { return nil })
	var bindErr *net.OpError
	if !errors.As(serveErr, &bindErr) || bindErr.Op != "listen" {
		t.Fatalf("expected actual bind error: %v", serveErr)
	}
	go func() { _, _ = bufio.NewReader(os.Stdin).ReadString('\n'); close(release) }()
	var reported sync.Once
	waitForSIPShutdown(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		return server.Shutdown(ctx)
	}, func(err error) {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("unexpected SIP shutdown error: %v", err)
		}
		reported.Do(func() { fmt.Println("ROOT_HELD") })
	}, time.Millisecond)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ROOT_DRAINED_WITH_BIND_ERROR")
	os.Exit(23) // Test-only distinguishable failure status, after actual cleanup.
}

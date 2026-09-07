package ginhelper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestHTTPBindFailureReturnsToRoot(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := &http.Server{Addr: occupied.Addr().String()}
	done := make(chan error, 1)
	go func() {
		done <- serveHTTPUntilShutdown(server, make(chan os.Signal), func(err error) { t.Errorf("shutdown: %v", err) }, time.Second, time.Millisecond)
	}()
	select {
	case err := <-done:
		var op *net.OpError
		if !errors.As(err, &op) || op.Op != "listen" {
			t.Fatalf("bind failure lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bind failure left root waiting for an exit signal")
	}
}

func TestHTTPShutdownTimeoutRetainsInFlightHandler(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release // Model a dependency that does not honor request cancellation.
		w.WriteHeader(http.StatusNoContent)
	})}
	quit, reported, done := make(chan os.Signal, 1), make(chan error, 1), make(chan error, 1)
	go func() {
		done <- serveHTTPUntilShutdown(server, quit, func(err error) {
			select {
			case reported <- err:
			default:
			}
		}, 20*time.Millisecond, time.Millisecond)
	}()
	t.Cleanup(func() { server.Close() })
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for {
			res, err := client.Get("http://" + addr)
			if err == nil {
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				response <- nil
				return
			}
			if time.Now().After(deadline) {
				response <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	// Always release the handler even if an assertion fails.
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHandler()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP handler did not start")
	}
	quit <- syscall.SIGTERM
	select {
	case err := <-reported:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected shutdown error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown deadline was not reported")
	}
	select {
	case err := <-done:
		t.Fatalf("root released dependencies with live handler: %v", err)
	default:
	}
	if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		conn.Close()
		t.Error("shutdown still admits new connections")
	}
	if os.Getenv("UVP_HTTP_DRAIN_CHILD") == "1" {
		fmt.Println("HTTP_HELD")
		if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
			t.Fatal(err)
		}
	}
	releaseHandler()
	select {
	case err := <-response:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("response did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("drained server did not return")
	}
}

func TestHTTPShutdownTimeoutProcessStaysAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPShutdownTimeoutRetainsInFlightHandler$")
	cmd.Env = append(os.Environ(), "UVP_HTTP_DRAIN_CHILD=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer writer.Close()
	cmd.Stdout, cmd.Stderr = writer, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	held := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if scanner.Text() == "HTTP_HELD" {
				held <- true
				io.Copy(io.Discard, output)
				return
			}
		}
		held <- false
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case ok := <-held:
		if !ok {
			t.Fatal("child exited before HTTP drain retry")
		}
	case <-ctx.Done():
		t.Fatal("child did not reach HTTP drain retry")
	}
	select {
	case err := <-done:
		t.Fatalf("process exited with an active ordinary handler: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := io.WriteString(input, "release\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("process failed to exit after HTTP drain")
	}
}

package control

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestEndpointSeparatesRolesAndInstances(t *testing.T) {
	root := t.TempDir()
	n, k, err := Endpoint(root, "launcher", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ root, role string }{{root, "backend"}, {filepath.Join(root, "other"), "launcher"}} {
		n2, k2, err := Endpoint(tc.root, tc.role, "fixture-secret")
		if err != nil || n == n2 || k == k2 {
			t.Fatalf("endpoint not bound: %v", err)
		}
	}
	if len(n) != 64 || k == "fixture-secret" {
		t.Fatal("invalid derived endpoint")
	}
	if _, _, err := Endpoint("relative", "launcher", "fixture-secret"); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, _, err := Endpoint(root, "other", "fixture-secret"); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestExchangeAndHandle(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Handle(ctx, b, func(_ context.Context, c Command) (Reply, error) {
			if c != Quiesce {
				t.Error("wrong command")
			}
			return MediaReady, nil
		})
	}()
	reply, err := Exchange(ctx, a, Quiesce)
	if err != nil || reply != MediaReady {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCommandNeverCallsHandler(t *testing.T) {
	for _, input := range []string{"restart\n", "stop extra\n", "STOP\n", string(make([]byte, 100))} {
		a, b := net.Pipe()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		called := false
		done := make(chan error, 1)
		go func() {
			defer b.Close()
			done <- Handle(ctx, b, func(context.Context, Command) (Reply, error) { called = true; return Ready, nil })
		}()
		_, _ = a.Write([]byte(input))
		if err := <-done; err == nil || called {
			t.Fatalf("invalid input handled: err=%v called=%v", err, called)
		}
		a.Close()
		cancel()
	}
}

func TestExchangeCancellationInterruptsIO(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := Exchange(ctx, a, Probe); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled exchange succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled exchange stuck")
	}
}

func TestHandlerFailureIsConstantOnWire(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Handle(ctx, b, func(context.Context, Command) (Reply, error) { return "", context.Canceled })
	}()
	reply, err := Exchange(ctx, a, Finalize)
	if err != nil || reply != Failed {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if err := <-done; err != context.Canceled && !errors.Is(err, context.Canceled) {
		t.Fatalf("local failure lost: %v", err)
	}
}

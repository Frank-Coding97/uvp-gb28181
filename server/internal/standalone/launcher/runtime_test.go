package launcher

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestPreflightPortConflictPreservesUnrelatedListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = checkPorts([]string{listener.Addr().String()}); err == nil {
		t.Fatal("occupied port accepted")
	}
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal("unrelated listener affected", err)
	}
	connection.Close()
}

func TestBackendProbeAddressUsesLocalConnection(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"127.0.0.1:8280", "127.0.0.1:8280", true}, {":8280", "127.0.0.1:8280", true},
		{"0.0.0.0:8280", "127.0.0.1:8280", true}, {"[::]:8280", "[::1]:8280", true},
		{"192.0.2.1:8280", "", false}, {"127.0.0.1:0", "", false}, {"localhost:8280", "", false},
	} {
		got, err := localProbeAddress(tc.input)
		if (err == nil) != tc.valid || got != tc.want {
			t.Fatalf("input=%s got=%s err=%v", tc.input, got, err)
		}
	}
}

func TestAliveButUnreadyHasBoundedDeadlineAndRetainsCause(t *testing.T) {
	cause := errors.New("HTTP 200 without valid instance readiness proof")
	started := time.Now()
	err := awaitReady(context.Background(), nil, 20*time.Millisecond, func(context.Context) error { return cause })
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, cause) || time.Since(started) > time.Second {
		t.Fatalf("unready wait not bounded: %v", err)
	}
}

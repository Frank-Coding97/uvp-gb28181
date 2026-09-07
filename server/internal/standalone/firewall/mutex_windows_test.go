//go:build windows

package firewall

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The opt-in test exercises the real named mutex across processes without
// touching firewall rules. It is skipped during ordinary package tests.
func TestFirewallGlobalMutexCrossProcessOptIn(t *testing.T) {
	if os.Getenv("UVP_FIREWALL_NATIVE_MUTEX") != "1" {
		t.Skip("set UVP_FIREWALL_NATIVE_MUTEX=1 for native mutex validation")
	}
	root := t.TempDir()
	unlock, err := lockFirewallInstance(context.Background(), root)
	if err != nil {
		t.Fatalf("acquire parent mutex: %v", err)
	}

	runHelper := func(expect string) error {
		cmd := exec.Command(os.Args[0], "-test.run=TestFirewallGlobalMutexHelper", "--")
		cmd.Env = append(os.Environ(),
			"UVP_FIREWALL_MUTEX_HELPER=1",
			"UVP_FIREWALL_MUTEX_ROOT="+root,
			"UVP_FIREWALL_MUTEX_EXPECT="+expect,
		)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		return cmd.Run()
	}
	if err := runHelper("blocked"); err != nil {
		unlock()
		t.Fatalf("child acquired held mutex: %v", err)
	}
	unlock()
	if err := runHelper("acquired"); err != nil {
		t.Fatalf("child did not acquire released mutex: %v", err)
	}
}

func TestFirewallGlobalMutexHelper(t *testing.T) {
	if os.Getenv("UVP_FIREWALL_MUTEX_HELPER") != "1" {
		return
	}
	root := os.Getenv("UVP_FIREWALL_MUTEX_ROOT")
	expect := os.Getenv("UVP_FIREWALL_MUTEX_EXPECT")
	if root == "" || (expect != "blocked" && expect != "acquired") {
		t.Fatal("invalid mutex helper environment")
	}
	timeout := 2 * time.Second
	if expect == "blocked" {
		timeout = 150 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	unlock, err := lockFirewallInstance(ctx, root)
	if expect == "blocked" {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked mutex error = %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("released mutex error = %v", err)
	}
	unlock()
}

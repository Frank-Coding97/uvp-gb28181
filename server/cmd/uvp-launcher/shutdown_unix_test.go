//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestShutdownContextReceivesTermination(t *testing.T) {
	if os.Getenv("UVP_SHUTDOWN_SIGNAL_HELPER") == "1" {
		ctx, stop := shutdownContext(context.Background())
		defer stop()
		fmt.Println("ready")
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("shutdown context did not cancel")
		}
		return
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownContextReceivesTermination$")
			cmd.Env = append(os.Environ(), "UVP_SHUTDOWN_SIGNAL_HELPER=1")
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "ready\n" {
				t.Fatalf("child readiness = %q, %v", line, err)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("signal must cancel context and allow clean exit: %v", err)
			}
		})
	}
}

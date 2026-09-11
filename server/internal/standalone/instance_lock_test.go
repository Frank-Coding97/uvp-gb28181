package standalone

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstanceLockRejectsDuplicateAndIgnoresPIDFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "run"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run", "pid"), []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := AcquireInstanceLock(dir)
	if second != nil {
		second.Close()
	}
	if !errors.Is(err, ErrInstanceRunning) {
		t.Fatalf("duplicate must be rejected: %v", err)
	}
	other, err := AcquireInstanceLock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	raw, err := os.ReadFile(filepath.Join(dir, "run", "pid"))
	if err != nil || string(raw) != "1" {
		t.Fatal("must not take over PID metadata")
	}
}

func TestInstanceLockProcessHelper(t *testing.T) {
	dir := os.Getenv("UVP_TEST_INSTANCE_LOCK_DIR")
	if dir == "" {
		return
	}
	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("LOCKED")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestInstanceLockReleasedAfterOwnerCrash(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstanceLockProcessHelper$")
	cmd.Env = append(os.Environ(), "UVP_TEST_INSTANCE_LOCK_DIR="+dir)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- strings.TrimSpace(line) }()
	select {
	case line := <-ready:
		if line != "LOCKED" {
			t.Fatalf("helper failed: %s", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("helper timeout")
	}
	if lock, err := AcquireInstanceLock(dir); !errors.Is(err, ErrInstanceRunning) {
		if lock != nil {
			lock.Close()
		}
		t.Fatalf("cross-process lock missing: %v", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	lock, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("crashed owner left lock: %v", err)
	}
	lock.Close()
}

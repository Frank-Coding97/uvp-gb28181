//go:build windows

package winprocess

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	consoleHelperModeEnv = "UVP_WINPROCESS_CONSOLE_HELPER"
	consoleHelperDirEnv  = "UVP_WINPROCESS_CONSOLE_DIR"
	consoleBootstrapMode = "bootstrap"
	consoleHelperMode    = "owner"
	consoleChildMode     = "child"
	consoleHelperTestRun = "^TestWindowsJobConsoleHelper$"

	consoleOwnerReadyMarker       = "owner-console-ready"
	consoleChildReadyMarker       = "child-console-ready"
	consoleOwnerInterruptMarker   = "owner-interrupt"
	consoleChildInterruptMarker   = "child-interrupt"
	consoleOwnerVerifiedMarker    = "owner-console-verified"
	consoleOwnerJobClosedMarker   = "owner-job-closed"
	consoleOwnerConsoleFreed      = "owner-console-freed"
	consoleOwnerResultMarker      = "owner-result.json"
	consoleOwnerErrorMarker       = "owner-error.txt"
	consoleChildInterruptExitCode = 76
)

var consoleKernel32 = windows.NewLazySystemDLL("kernel32.dll")

var (
	consoleAllocConsole          = consoleKernel32.NewProc("AllocConsole")
	consoleFreeConsole           = consoleKernel32.NewProc("FreeConsole")
	consoleGetConsoleProcessList = consoleKernel32.NewProc("GetConsoleProcessList")
)

type consoleJobResult struct {
	OwnerPID             uint32   `json:"owner_pid"`
	ChildPID             uint32   `json:"child_pid"`
	ConsolePIDs          []uint32 `json:"console_pids"`
	ChildInJob           bool     `json:"child_in_job"`
	OwnerInterrupt       bool     `json:"owner_interrupt"`
	ChildInterrupt       bool     `json:"child_interrupt"`
	ChildAliveAfterCtrlC bool     `json:"child_alive_after_ctrl_c"`
	JobClosed            bool     `json:"job_closed"`
	ChildExited          bool     `json:"child_exited"`
	ChildExitCode        uint32   `json:"child_exit_code"`
	ConsoleFreed         bool     `json:"console_freed"`
}

func TestWindowsJobConsoleCtrlCIsolation(t *testing.T) {
	dir := t.TempDir()
	stdoutPath := filepath.Join(dir, "owner.stdout.log")
	stderrPath := filepath.Join(dir, "owner.stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	null, err := os.OpenFile("NUL", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()

	cmd := exec.Command(os.Args[0], "-test.run="+consoleHelperTestRun, "-test.count=1")
	cmd.Env = replaceEnvironment(os.Environ(), map[string]string{
		consoleHelperModeEnv: consoleBootstrapMode,
		consoleHelperDirEnv:  dir,
	})
	// DETACHED_PROCESS prevents this helper from inheriting the test runner's
	// console. The helper must create the only console it uses with AllocConsole.
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	cmd.Stdin = null
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := waitExternalCommand(cmd, 30*time.Second); err != nil {
		t.Fatalf("private-console helper failed: %v\n%s", err, readConsoleDiagnostics(dir, stdoutPath, stderrPath))
	}

	raw, err := os.ReadFile(filepath.Join(dir, consoleOwnerResultMarker))
	if err != nil {
		t.Fatalf("read helper result: %v\n%s", err, readConsoleDiagnostics(dir, stdoutPath, stderrPath))
	}
	var result consoleJobResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode helper result: %v; raw=%q", err, raw)
	}
	if result.OwnerPID == 0 {
		t.Fatal("owner did not report a process id")
	}
	if result.ChildPID == 0 || result.ChildPID == result.OwnerPID {
		t.Fatalf("invalid child pid %d", result.ChildPID)
	}
	if !samePIDSet(result.ConsolePIDs, []uint32{result.OwnerPID, result.ChildPID}) {
		t.Fatalf("private console process list = %v, want only owner and child", result.ConsolePIDs)
	}
	if !result.ChildInJob {
		t.Fatal("child was not verified in its creating Job")
	}
	if !result.OwnerInterrupt {
		t.Fatal("owner did not receive os.Interrupt")
	}
	if result.ChildInterrupt {
		t.Fatalf("child received Ctrl+C and exited with the isolation failure code %d", consoleChildInterruptExitCode)
	}
	if !result.ChildAliveAfterCtrlC {
		t.Fatal("child did not remain alive after the owner's Ctrl+C")
	}
	if !result.JobClosed || !result.ChildExited {
		t.Fatalf("Job cleanup result = job_closed:%t child_exited:%t", result.JobClosed, result.ChildExited)
	}
	if result.ConsoleFreed == false {
		t.Fatal("private console was not released")
	}
}

func TestWindowsJobConsoleHelper(t *testing.T) {
	mode := os.Getenv(consoleHelperModeEnv)
	if mode == "" {
		return
	}
	dir := os.Getenv(consoleHelperDirEnv)
	if dir == "" {
		t.Fatal("console helper directory is empty")
	}
	var err error
	switch mode {
	case consoleBootstrapMode:
		err = runConsoleBootstrap(dir)
	case consoleHelperMode:
		var result consoleJobResult
		result, err = runConsoleOwner(dir)
		if err == nil {
			var raw []byte
			raw, err = json.Marshal(result)
			if err == nil {
				err = writeConsoleMarker(dir, consoleOwnerResultMarker, string(raw))
			}
		}
	case consoleChildMode:
		err = runConsoleChild(dir)
	default:
		err = fmt.Errorf("unknown console helper mode %q", mode)
	}
	if err != nil {
		_ = writeConsoleError(dir, err.Error())
		t.Fatal(err)
	}
}

func runConsoleBootstrap(dir string) error {
	if _, err := consoleProcessIDs(); err == nil {
		return errors.New("detached bootstrap already has a console")
	}
	if err := allocPrivateConsole(); err != nil {
		return err
	}
	consoleAllocated := true
	defer func() {
		if consoleAllocated {
			_ = freePrivateConsole()
		}
	}()

	// The top-level helper is deliberately DETACHED_PROCESS. Starting the Go
	// owner after AllocConsole lets the Go runtime install its console handler
	// while a console is already present. The owner inherits this private
	// console and then becomes the only attached process before the event test.
	owner := exec.Command(os.Args[0], "-test.run="+consoleHelperTestRun, "-test.count=1")
	owner.Env = replaceEnvironment(os.Environ(), map[string]string{
		consoleHelperModeEnv: consoleHelperMode,
		consoleHelperDirEnv:  dir,
	})
	owner.SysProcAttr = &windows.SysProcAttr{}
	if err := owner.Start(); err != nil {
		return fmt.Errorf("start console owner: %w", err)
	}

	// Once the owner has been created, it has inherited the console. Leaving
	// it here keeps the owner process list limited to owner and Job child.
	if err := freePrivateConsole(); err != nil {
		_ = owner.Process.Kill()
		_ = waitExternalCommand(owner, 5*time.Second)
		return fmt.Errorf("detach bootstrap from private console: %w", err)
	}
	consoleAllocated = false

	if err := waitExternalCommand(owner, 30*time.Second); err != nil {
		if helperError, readErr := os.ReadFile(filepath.Join(dir, consoleOwnerErrorMarker)); readErr == nil {
			return fmt.Errorf("console owner failed: %s", strings.TrimSpace(string(helperError)))
		}
		return fmt.Errorf("console owner failed: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, consoleOwnerResultMarker)); err != nil {
		return fmt.Errorf("console owner result missing: %w", err)
	}
	return nil
}

func runConsoleOwner(dir string) (consoleJobResult, error) {
	var result consoleJobResult
	if _, err := consoleProcessIDs(); err != nil {
		return result, fmt.Errorf("owner does not have an inherited private console: %w", err)
	}

	result.OwnerPID = windows.GetCurrentProcessId()
	if err := writeConsoleMarker(dir, consoleOwnerReadyMarker, fmt.Sprintf("pid=%d", result.OwnerPID)); err != nil {
		return result, err
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	job, err := NewJob()
	if err != nil {
		return result, err
	}
	defer job.Close()
	child, err := job.Start(consoleChildStartSpec(dir))
	if err != nil {
		return result, err
	}
	defer child.Close()
	result.ChildPID = uint32(child.PID())
	if inJob, err := job.Contains(child); err != nil {
		return result, fmt.Errorf("verify child Job membership: %w", err)
	} else if !inJob {
		return result, errors.New("child is outside its creating Job")
	}
	result.ChildInJob = true
	childReady, err := waitConsoleMarker(dir, consoleChildReadyMarker, 5*time.Second)
	if err != nil {
		return result, err
	}
	childReadyPID, err := parseConsolePID(childReady)
	if err != nil {
		return result, fmt.Errorf("parse child ready marker: %w", err)
	}
	if childReadyPID != result.ChildPID {
		return result, fmt.Errorf("child ready pid = %d, Job child pid = %d", childReadyPID, result.ChildPID)
	}
	result.ConsolePIDs, err = consoleProcessIDs()
	if err != nil {
		return result, err
	}
	if !samePIDSet(result.ConsolePIDs, []uint32{result.OwnerPID, result.ChildPID}) {
		return result, fmt.Errorf("private console process list = %v, want only owner and child", result.ConsolePIDs)
	}
	if err := writeConsoleMarker(dir, consoleOwnerVerifiedMarker, "verified"); err != nil {
		return result, err
	}

	// The owner is attached to the newly allocated console and has verified its
	// complete process list. Group zero is therefore scoped to this console;
	// CREATE_NEW_PROCESS_GROUP on the child disables Ctrl+C for that child.
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0); err != nil {
		return result, fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	select {
	case sig := <-interrupts:
		if sig != os.Interrupt {
			return result, fmt.Errorf("owner received unexpected signal %v", sig)
		}
		result.OwnerInterrupt = true
		if err := writeConsoleMarker(dir, consoleOwnerInterruptMarker, "received"); err != nil {
			return result, err
		}
	case <-time.After(5 * time.Second):
		return result, errors.New("owner did not receive Ctrl+C")
	}
	if err := assertConsoleMarkerAbsent(dir, consoleChildInterruptMarker, 750*time.Millisecond); err != nil {
		return result, err
	}
	result.ChildAliveAfterCtrlC, err = processStillRunning(result.ChildPID)
	if err != nil {
		return result, err
	}
	if !result.ChildAliveAfterCtrlC {
		return result, errors.New("child exited after owner's Ctrl+C")
	}

	if err := job.Close(); err != nil {
		return result, err
	}
	result.JobClosed = true
	if err := writeConsoleMarker(dir, consoleOwnerJobClosedMarker, "closed"); err != nil {
		return result, err
	}
	result.ChildExitCode, err = waitProcessWithin(child, 5*time.Second)
	if err != nil {
		return result, err
	}
	result.ChildExited = true

	if err := freePrivateConsole(); err != nil {
		return result, err
	}
	result.ConsoleFreed = true
	if _, err := consoleProcessIDs(); err == nil {
		return result, errors.New("owner still has a console after FreeConsole")
	}
	if err := writeConsoleMarker(dir, consoleOwnerConsoleFreed, "freed"); err != nil {
		return result, err
	}
	return result, nil
}

func runConsoleChild(dir string) error {
	pid := windows.GetCurrentProcessId()
	ids, err := consoleProcessIDs()
	if err != nil {
		return fmt.Errorf("child has no inherited console: %w", err)
	}
	if !containsPID(ids, pid) {
		return fmt.Errorf("child pid %d is absent from inherited console process list %v", pid, ids)
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if err := writeConsoleMarker(dir, consoleChildReadyMarker, fmt.Sprintf("pid=%d", pid)); err != nil {
		return err
	}
	sig := <-interrupts
	if sig != os.Interrupt {
		return fmt.Errorf("child received unexpected signal %v", sig)
	}
	if err := writeConsoleMarker(dir, consoleChildInterruptMarker, "received"); err != nil {
		os.Exit(consoleChildInterruptExitCode)
	}
	os.Exit(consoleChildInterruptExitCode)
	return nil
}

func consoleChildStartSpec(dir string) StartSpec {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return StartSpec{
		Path: executable,
		Args: []string{"-test.run=" + consoleHelperTestRun, "-test.count=1"},
		Env: replaceEnvironment(os.Environ(), map[string]string{
			consoleHelperModeEnv: consoleChildMode,
			consoleHelperDirEnv:  dir,
		}),
		Dir: filepath.Dir(executable),
	}
}

func allocPrivateConsole() error {
	result, _, err := consoleAllocConsole.Call()
	if result == 0 {
		return fmt.Errorf("AllocConsole: %v", err)
	}
	return nil
}

func freePrivateConsole() error {
	result, _, err := consoleFreeConsole.Call()
	if result == 0 {
		return fmt.Errorf("FreeConsole: %v", err)
	}
	return nil
}

func consoleProcessIDs() ([]uint32, error) {
	ids := make([]uint32, 32)
	result, _, err := consoleGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	if result == 0 {
		return nil, fmt.Errorf("GetConsoleProcessList: %v", err)
	}
	count := int(result)
	if count > len(ids) {
		return nil, fmt.Errorf("GetConsoleProcessList returned %d IDs for a %d-entry buffer", count, len(ids))
	}
	return append([]uint32(nil), ids[:count]...), nil
}

func parseConsolePID(marker string) (uint32, error) {
	const prefix = "pid="
	value := strings.TrimSpace(marker)
	if !strings.HasPrefix(value, prefix) {
		return 0, fmt.Errorf("marker %q has no %q prefix", value, prefix)
	}
	pid, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	if err != nil || pid == 0 {
		return 0, fmt.Errorf("invalid pid in marker %q", value)
	}
	return uint32(pid), nil
}

func writeConsoleMarker(dir, name, content string) error {
	tmp := filepath.Join(dir, name+".tmp-"+strconv.FormatUint(uint64(os.Getpid()), 10))
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeConsoleError(dir, content string) error {
	file, err := os.OpenFile(filepath.Join(dir, consoleOwnerErrorMarker), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	if _, err := io.WriteString(file, content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func waitConsoleMarker(dir, name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return string(content), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if name != consoleOwnerErrorMarker {
			if helperError, helperErr := os.ReadFile(filepath.Join(dir, consoleOwnerErrorMarker)); helperErr == nil {
				return "", fmt.Errorf("console helper reported failure: %s", strings.TrimSpace(string(helperError)))
			}
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("timed out waiting for %s", name)
		}
		<-ticker.C
	}
}

func assertConsoleMarkerAbsent(dir, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			return fmt.Errorf("unexpected %s marker", name)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		<-ticker.C
	}
}

func processStillRunning(pid uint32) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	switch status {
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	case windows.WAIT_OBJECT_0:
		return false, nil
	default:
		return false, fmt.Errorf("process %d wait status 0x%x", pid, status)
	}
}

func waitProcessWithin(process *Process, timeout time.Duration) (uint32, error) {
	if process == nil || process.handle == windows.InvalidHandle {
		return 0, ErrProcessClosed
	}
	milliseconds := timeout.Milliseconds()
	if milliseconds <= 0 || milliseconds > int64(^uint32(0)) {
		return 0, errors.New("invalid process wait timeout")
	}
	status, err := windows.WaitForSingleObject(process.handle, uint32(milliseconds))
	if err != nil {
		return 0, err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return 0, fmt.Errorf("process %d did not exit within %s", process.PID(), timeout)
	}
	if status != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("process %d wait status 0x%x", process.PID(), status)
	}
	return process.Wait()
}

func samePIDSet(got, want []uint32) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[uint32]struct{}, len(got))
	for _, pid := range got {
		seen[pid] = struct{}{}
	}
	for _, pid := range want {
		if _, ok := seen[pid]; !ok {
			return false
		}
	}
	return true
}

func containsPID(pids []uint32, want uint32) bool {
	for _, pid := range pids {
		if pid == want {
			return true
		}
	}
	return false
}

func waitExternalCommand(cmd *exec.Cmd, timeout time.Duration) error {
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-finished:
		return err
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		cleanup := time.NewTimer(5 * time.Second)
		defer cleanup.Stop()
		select {
		case err := <-finished:
			if err == nil {
				return errors.New("helper exceeded timeout but exited successfully after kill")
			}
			return fmt.Errorf("helper exceeded %s: %w", timeout, err)
		case <-cleanup.C:
			return fmt.Errorf("helper exceeded %s and did not terminate after kill", timeout)
		}
	}
}

func readDiagnostic(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 16<<10))
	if err != nil {
		return err.Error()
	}
	return string(content)
}

func readConsoleDiagnostics(dir, stdoutPath, stderrPath string) string {
	return fmt.Sprintf("owner-error: %s\nowner-stdout: %s\nowner-stderr: %s", readDiagnostic(filepath.Join(dir, consoleOwnerErrorMarker)), readDiagnostic(stdoutPath), readDiagnostic(stderrPath))
}

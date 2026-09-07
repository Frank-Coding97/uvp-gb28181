//go:build windows

package winprocess

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processHelperEnv       = "UVP_WINPROCESS_TEST_HELPER"
	processHelperModeEnv   = "UVP_WINPROCESS_TEST_MODE"
	processHelperResultEnv = "UVP_WINPROCESS_TEST_RESULT"
	processHelperHandleEnv = "UVP_WINPROCESS_TEST_SENTINEL"
)

func TestWindowsEnvironmentBlockSortsAndKeepsDriveEntries(t *testing.T) {
	block, err := windowsEnvironmentBlock([]string{
		"z=last",
		"A=first",
		"=C:=C:\\work dir",
		"a=second",
		"=D:=D:\\other",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := decodeEnvironmentBlock(block)
	want := []string{"=C:=C:\\work dir", "=D:=D:\\other", "a=second", "z=last"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("environment block = %#v, want %#v", got, want)
	}
	if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatalf("environment block is not double-NUL terminated: %#v", block)
	}
}

func TestWindowsEnvironmentBlockRejectsMalformedEntries(t *testing.T) {
	if _, err := windowsEnvironmentBlock([]string{"missing-separator"}); err == nil {
		t.Fatal("malformed environment entry was accepted")
	}
	if _, err := windowsEnvironmentBlock([]string{"NAME=value\x00secret"}); err == nil {
		t.Fatal("NUL environment entry was accepted")
	}
}

func TestWindowsStartRequiresAbsoluteExecutableAndDirectory(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workingDir := filepath.Dir(executable)
	if _, err := job.Start(StartSpec{Path: filepath.Base(executable), Dir: workingDir}); err == nil {
		t.Fatal("relative executable path was accepted")
	}
	if _, err := job.Start(StartSpec{Path: executable, Dir: "."}); err == nil {
		t.Fatal("relative working directory was accepted")
	}
}

func TestWindowsJobStartsDescendantsAndCloseKillsOnlyOwned(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()

	ownedReader, ownedWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ownedReader.Close()
	owned, err := job.Start(helperStartSpec("spawn-grandchild", nil, ownedWriter, ownedWriter))
	if err != nil {
		t.Fatal(err)
	}
	ownedWriter.Close()
	ownedOutput := bufio.NewReader(ownedReader)
	grandchildLine := readLineWithin(t, ownedOutput)
	if !strings.HasPrefix(grandchildLine, "grandchild_pid=") {
		t.Fatalf("unexpected child output %q", grandchildLine)
	}
	grandchildPID, err := strconv.ParseUint(strings.TrimPrefix(grandchildLine, "grandchild_pid="), 10, 32)
	if err != nil {
		t.Fatalf("parse grandchild pid: %v", err)
	}
	if inJob, err := owned.IsInJob(); err != nil {
		t.Fatal(err)
	} else if !inJob {
		t.Fatal("created child is not in a Job Object")
	}
	if inJob, err := job.Contains(owned); err != nil {
		t.Fatal(err)
	} else if !inJob {
		t.Fatal("created child is not in its creating Job Object")
	}
	grandchildHandle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(grandchildPID))
	if err != nil {
		t.Fatalf("open grandchild: %v", err)
	}
	defer windows.CloseHandle(grandchildHandle)

	unrelated, unrelatedOutput := startUnownedHelper(t)
	defer func() {
		if unrelated.ProcessState == nil {
			_ = unrelated.Process.Kill()
			_ = unrelated.Wait()
		}
	}()
	_ = readLineWithin(t, unrelatedOutput)
	unrelatedHandle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(unrelated.Process.Pid))
	if err != nil {
		t.Fatalf("open unrelated helper: %v", err)
	}
	defer windows.CloseHandle(unrelatedHandle)

	if err := job.Close(); err != nil {
		t.Fatal(err)
	}
	waitForProcess(t, owned)
	if event, err := windows.WaitForSingleObject(grandchildHandle, 5000); err != nil {
		t.Fatalf("wait for grandchild: %v", err)
	} else if event != windows.WAIT_OBJECT_0 {
		t.Fatalf("grandchild still alive after Job.Close: wait status 0x%x", event)
	}
	if event, err := windows.WaitForSingleObject(unrelatedHandle, 0); err != nil {
		t.Fatalf("probe unrelated helper: %v", err)
	} else if event != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("Job.Close affected unrelated helper: wait status 0x%x", event)
	}
}

func TestWindowsJobKillsChildrenWhenOwnerExits(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "owner-result.txt")
	cmd := helperCommand("owner-exit", map[string]string{processHelperResultEnv: resultPath})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("owner helper failed: %v; output=%s", err, output)
	}
	contents, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 32)
	if err != nil {
		t.Fatalf("parse owner child pid: %v", err)
	}
	waitForPIDExit(t, uint32(pid))
}

func TestWindowsJobKillsChildIfOwnerCrashesBeforeRegistration(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "crash-result.txt")
	cmd := helperCommand("owner-crash-before-registration", map[string]string{processHelperResultEnv: resultPath})
	err := cmd.Run()
	if err == nil || cmd.ProcessState.ExitCode() != 73 {
		t.Fatalf("expected injected owner crash: %v", err)
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	waitForPIDExit(t, uint32(pid))
}

func TestWindowsJobUsesExplicitStandardIO(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		t.Fatal(err)
	}
	process, err := job.Start(helperStartSpec("stdio", stdinReader, stdoutWriter, stderrWriter))
	if err != nil {
		t.Fatal(err)
	}
	stdinReader.Close()
	stdoutWriter.Close()
	stderrWriter.Close()
	if _, err := stdinWriter.Write([]byte("input\n")); err != nil {
		t.Fatal(err)
	}
	stdinWriter.Close()
	stdout, stdoutErr := io.ReadAll(stdoutReader)
	stderr, stderrErr := io.ReadAll(stderrReader)
	stdoutReader.Close()
	stderrReader.Close()
	if code, err := process.Wait(); err != nil {
		t.Fatal(err)
	} else if code != 0 {
		t.Fatalf("stdio helper exit code = %d", code)
	}
	if stdoutErr != nil {
		t.Fatal(stdoutErr)
	}
	if stderrErr != nil {
		t.Fatal(stderrErr)
	}
	if string(stdout) != "stdout:input\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if string(stderr) != "stderr:ok\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestWindowsFailedCreateAddsNoJobMember(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	owned, err := job.Start(helperStartSpec("sleep", nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	before := jobProcessIDs(t, job)
	if len(before) != 1 {
		t.Fatalf("initial Job members = %v, want one process", before)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(filepath.Dir(executable), "uvp-winprocess-missing.exe")
	if _, err := job.Start(StartSpec{
		Path: missing,
		Env:  os.Environ(),
		Dir:  filepath.Dir(executable),
	}); err == nil {
		t.Fatal("failed process creation unexpectedly succeeded")
	}
	after := jobProcessIDs(t, job)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Job members after failed create = %v, before = %v", after, before)
	}
	if err := job.Close(); err != nil {
		t.Fatal(err)
	}
	waitForProcess(t, owned)
}

func TestWindowsAfterCreateFailureDoesNotExposeProcess(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	var createdPID uint32
	_, err = job.start(helperStartSpec("sleep", nil, nil, nil), func(info windows.ProcessInformation) error {
		createdPID = info.ProcessId
		if err := windows.TerminateProcess(info.Process, 73); err != nil {
			return err
		}
		return errors.New("injected post-create failure")
	})
	if err == nil {
		t.Fatal("post-create failure was ignored")
	}
	if createdPID == 0 {
		t.Fatal("post-create hook did not observe a process")
	}
	waitForPIDExit(t, createdPID)
	if members := jobProcessIDs(t, job); len(members) != 0 {
		t.Fatalf("Job members after post-create failure = %v", members)
	}
}

func TestWindowsJobDoesNotLeakUnlistedInheritableHandle(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	sentinelPath := filepath.Join(t.TempDir(), "sentinel.txt")
	sentinel, err := os.OpenFile(sentinelPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer sentinel.Close()
	if err := windows.SetHandleInformation(windows.Handle(sentinel.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		t.Fatal(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutReader.Close()
	values := map[string]string{processHelperHandleEnv: strconv.FormatUint(uint64(sentinel.Fd()), 10)}
	process, err := job.Start(helperStartSpecWithValues("check-sentinel", nil, stdoutWriter, stdoutWriter, values))
	if err != nil {
		stdoutWriter.Close()
		t.Fatal(err)
	}
	stdoutWriter.Close()
	if line := readLineWithin(t, bufio.NewReader(stdoutReader)); line != "sentinel-absent" {
		t.Fatalf("unlisted handle probe = %q", line)
	}
	waitForProcess(t, process)
}

func TestWinProcessHelper(t *testing.T) {
	if os.Getenv(processHelperEnv) != "1" {
		return
	}
	switch os.Getenv(processHelperModeEnv) {
	case "sleep":
		fmt.Fprintf(os.Stdout, "ready pid=%d\n", os.Getpid())
		for {
			time.Sleep(100 * time.Millisecond)
		}
	case "grandchild":
		fmt.Fprintf(os.Stdout, "grandchild-ready pid=%d\n", os.Getpid())
		for {
			time.Sleep(100 * time.Millisecond)
		}
	case "spawn-grandchild":
		cmd := helperCommand("grandchild", nil)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatalf("start grandchild: %v", err)
		}
		fmt.Fprintf(os.Stdout, "grandchild_pid=%d\n", cmd.Process.Pid)
		for {
			time.Sleep(100 * time.Millisecond)
		}
	case "stdio":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatalf("read stdin: %v", err)
		}
		fmt.Fprintf(os.Stdout, "stdout:%s", input)
		fmt.Fprintln(os.Stderr, "stderr:ok")
		os.Exit(0)
	case "check-sentinel":
		handleValue, err := strconv.ParseUint(os.Getenv(processHelperHandleEnv), 10, 64)
		if err != nil {
			t.Fatalf("parse sentinel handle: %v", err)
		}
		if _, err := windows.GetFileType(windows.Handle(handleValue)); err == nil {
			fmt.Fprintln(os.Stdout, "sentinel-present")
		} else {
			fmt.Fprintln(os.Stdout, "sentinel-absent")
		}
	case "owner-crash-before-registration":
		job, err := NewJob()
		if err != nil {
			t.Fatal(err)
		}
		_, err = job.start(helperStartSpec("sleep", nil, nil, nil), func(info windows.ProcessInformation) error {
			if err := os.WriteFile(os.Getenv(processHelperResultEnv), []byte(strconv.FormatUint(uint64(info.ProcessId), 10)), 0600); err != nil {
				return err
			}
			return windows.TerminateProcess(windows.CurrentProcess(), 73)
		})
		t.Fatalf("owner crash did not occur: %v", err)
	case "owner-exit":
		job, err := NewJob()
		if err != nil {
			t.Fatalf("create owner job: %v", err)
		}
		child, err := job.Start(helperStartSpecWithValues("sleep", nil, nil, nil, nil))
		if err != nil {
			t.Fatalf("start owner child: %v", err)
		}
		if err := os.WriteFile(os.Getenv(processHelperResultEnv), []byte(strconv.Itoa(child.PID())), 0o600); err != nil {
			t.Fatalf("write owner result: %v", err)
		}
		os.Exit(0)
	default:
		t.Fatalf("unknown helper mode")
	}
}

func helperStartSpec(mode string, stdin, stdout, stderr *os.File) StartSpec {
	return helperStartSpecWithValues(mode, stdin, stdout, stderr, nil)
}

func helperStartSpecWithValues(mode string, stdin, stdout, stderr *os.File, values map[string]string) StartSpec {
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	envValues := map[string]string{
		processHelperEnv:     "1",
		processHelperModeEnv: mode,
	}
	for key, value := range values {
		envValues[key] = value
	}
	return StartSpec{
		Path:   executable,
		Args:   []string{"-test.run=^TestWinProcessHelper$", "-test.count=1"},
		Env:    replaceEnvironment(os.Environ(), envValues),
		Dir:    filepath.Dir(executable),
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}
}

func helperCommand(mode string, values map[string]string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWinProcessHelper$", "-test.count=1")
	envValues := map[string]string{
		processHelperEnv:     "1",
		processHelperModeEnv: mode,
	}
	for key, value := range values {
		envValues[key] = value
	}
	cmd.Env = replaceEnvironment(os.Environ(), envValues)
	return cmd
}

func startUnownedHelper(t *testing.T) (*exec.Cmd, *bufio.Reader) {
	t.Helper()
	cmd := helperCommand("sleep", nil)
	readerPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd, bufio.NewReader(readerPipe)
}

func replaceEnvironment(base []string, values map[string]string) []string {
	overrides := make(map[string]string, len(values))
	for key, value := range values {
		overrides[strings.ToLower(key)] = value
	}
	out := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, ok := windowsEnvironmentKey(entry)
		if ok {
			if _, replace := overrides[strings.ToLower(key)]; replace {
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func decodeEnvironmentBlock(block []uint16) []string {
	entries := make([]string, 0)
	start := 0
	for index, value := range block {
		if value != 0 {
			continue
		}
		if index == start {
			break
		}
		entries = append(entries, windows.UTF16ToString(block[start:index]))
		start = index + 1
	}
	return entries
}

func readLineWithin(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	lines := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			errs <- err
			return
		}
		lines <- strings.TrimSuffix(line, "\n")
	}()
	select {
	case line := <-lines:
		return line
	case err := <-errs:
		t.Fatalf("read helper output: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for helper output")
	}
	return ""
}

func waitForProcess(t *testing.T, process *Process) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for owned process")
	}
}

func waitForPIDExit(t *testing.T, pid uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		event, waitErr := windows.WaitForSingleObject(handle, 0)
		windows.CloseHandle(handle)
		if waitErr != nil {
			t.Fatal(waitErr)
		}
		if event == windows.WAIT_OBJECT_0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d remained alive after owner exit", pid)
}

func jobProcessIDs(t *testing.T, job *Job) []uint32 {
	t.Helper()
	var info struct {
		NumberOfAssignedProcesses uint32
		NumberOfProcessIdsInList  uint32
		ProcessIdList             [16]uint32
	}
	if err := windows.QueryInformationJobObject(
		job.handle,
		windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	count := int(info.NumberOfProcessIdsInList)
	if count > len(info.ProcessIdList) {
		t.Fatalf("Job process list count %d exceeds test buffer", count)
	}
	return append([]uint32(nil), info.ProcessIdList[:count]...)
}

func TestWindowsMembershipQueryCannotBlockJobClose(t *testing.T) {
	job, err := NewJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	process, err := job.Start(helperStartSpec("sleep", nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	// Model the process lock held by Wait without relying on scheduler timing.
	process.mu.Lock()
	queryDone := make(chan struct{})
	go func() { _, _ = job.Contains(process); close(queryDone) }()
	time.Sleep(25 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- job.Close() }()
	select {
	case err := <-closed:
		process.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		process.mu.Unlock()
		t.Fatal("membership query blocked Job.Close")
	}
	<-queryDone
	waitForProcess(t, process)
}

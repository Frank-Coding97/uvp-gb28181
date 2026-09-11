//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const consoleEntryHelperEnv = "UVP_T16_CONSOLE_HELPER"

var consoleEntryKernel = windows.NewLazySystemDLL("kernel32.dll")

type consoleEntryEvidence struct {
	Mode          string   `json:"mode"`
	ConsolePIDs   []uint32 `json:"consolePids"`
	ExitCode      int      `json:"exitCode"`
	MarkerPresent bool     `json:"markerPresent"`
	ElapsedMS     int64    `json:"elapsedMs"`
}

// This owns the explicitly supplied, stopped fixture. It never attaches to
// the test runner's console and must not target a customer installation.
func TestWindowsStandaloneConsoleEntry(t *testing.T) {
	root := os.Getenv("UVP_T16_CONSOLE_ROOT")
	if root == "" {
		t.Skip("requires UVP_T16_CONSOLE_ROOT pointing to an isolated stopped fixture")
	}
	if mode := os.Getenv(consoleEntryHelperEnv); mode != "" {
		evidence, err := runConsoleEntry(root, mode)
		raw, marshalErr := json.Marshal(evidence)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Log(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, mode := range []string{"ctrl-c", "window-close"} {
		if !t.Run(mode, func(t *testing.T) {
			budget := 160 * time.Second
			if os.Getenv("UVP_T16_ACTIVE_READY_FILE") != "" {
				budget = 240 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsStandaloneConsoleEntry$", "-test.v", "-test.count=1")
			cmd.Env = append(os.Environ(), consoleEntryHelperEnv+"="+mode)
			cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
			output, err := cmd.CombinedOutput()
			t.Log(string(output))
			if err != nil {
				t.Fatalf("private console %s failed: %v", mode, err)
			}
		}) {
			break
		}
	}
}

func runConsoleEntry(root, mode string) (result consoleEntryEvidence, resultErr error) {
	result.Mode = mode
	if mode != "ctrl-c" && mode != "window-close" {
		return result, errors.New("unknown console event mode")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	marker := filepath.Join(root, "data", ".uvp-running.json")
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		return result, errors.New("fixture must be stopped cleanly with no running marker")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil {
		return result, err
	}
	if ids, err := consoleEntryPIDs(); err == nil || len(ids) != 0 {
		return result, errors.New("detached helper unexpectedly has a console")
	}
	if ok, _, err := consoleEntryKernel.NewProc("AllocConsole").Call(); ok == 0 {
		return result, fmt.Errorf("allocate private console: %v", err)
	}
	attached := true
	defer func() {
		if attached {
			_, _, _ = consoleEntryKernel.NewProc("FreeConsole").Call()
		}
	}()
	// DETACHED_PROCESS can inherit Ctrl+C ignoring. Clear it before starting
	// UVP, whose Go runtime installs its handler on the inherited console.
	if ok, _, err := consoleEntryKernel.NewProc("SetConsoleCtrlHandler").Call(0, 0); ok == 0 {
		return result, fmt.Errorf("clear inherited ignore flag: %v", err)
	}
	log, err := os.CreateTemp(root, "t16-console-"+mode+"-*.log")
	if err != nil {
		return result, err
	}
	defer log.Close()
	cmd := exec.Command(filepath.Join(root, "UVP.exe"), "--no-browser")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		return result, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if !exited {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
	}()
	if ok, _, err := consoleEntryKernel.NewProc("FreeConsole").Call(); ok == 0 {
		return result, fmt.Errorf("detach bootstrap: %v", err)
	}
	attached = false
	deadline := time.Now().Add(60 * time.Second)
	for {
		raw, err := os.ReadFile(log.Name())
		if err != nil {
			return result, err
		}
		if strings.Contains(string(raw), "UVP: MediaReady") {
			break
		}
		select {
		case err := <-done:
			exited = true
			return result, fmt.Errorf("UVP exited before MediaReady: %v (log %s)", err, log.Name())
		default:
		}
		if time.Now().After(deadline) {
			return result, fmt.Errorf("MediaReady timed out (log %s)", log.Name())
		}
		time.Sleep(50 * time.Millisecond)
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir})
	if err != nil {
		return result, err
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		return result, err
	}
	if _, err := os.Stat(marker); err != nil {
		return result, errors.New("running instance has no marker")
	}
	if ok, _, err := consoleEntryKernel.NewProc("AttachConsole").Call(uintptr(cmd.Process.Pid)); ok == 0 {
		return result, fmt.Errorf("attach owned UVP console: %v", err)
	}
	attached = true
	result.ConsolePIDs, err = consoleEntryPIDs()
	if err != nil {
		return result, err
	}
	ownerPID, controllerPID := uint32(cmd.Process.Pid), windows.GetCurrentProcessId()
	if len(result.ConsolePIDs) != 2 || !((result.ConsolePIDs[0] == ownerPID && result.ConsolePIDs[1] == controllerPID) || (result.ConsolePIDs[1] == ownerPID && result.ConsolePIDs[0] == controllerPID)) {
		return result, fmt.Errorf("private console must contain only UVP and controller: %v", result.ConsolePIDs)
	}
	allowed := []string{release.BackendExe, release.RedisExe, release.MediaExe}
	handles, err := consoleEntryOwnedProcesses(ownerPID, allowed)
	if err != nil {
		return result, err
	}
	defer func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}()
	if ready := os.Getenv("UVP_T16_ACTIVE_READY_FILE"); ready != "" {
		proceed := os.Getenv("UVP_T16_ACTIVE_CONTINUE_FILE")
		if proceed == "" {
			return result, errors.New("active console test requires a continue file")
		}
		if _, err := os.Stat(proceed); !errors.Is(err, os.ErrNotExist) {
			return result, errors.New("active console continue marker must not already exist")
		}
		file, err := os.OpenFile(ready, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return result, errors.New("cannot publish active console readiness marker")
		}
		if err := file.Close(); err != nil {
			return result, err
		}
		if !coreWaitForFile(proceed, 150*time.Second) {
			return result, errors.New("active console work was not confirmed")
		}
	}
	started := time.Now()
	if mode == "ctrl-c" {
		if ok, _, err := consoleEntryKernel.NewProc("SetConsoleCtrlHandler").Call(0, 1); ok == 0 {
			return result, fmt.Errorf("ignore controller Ctrl+C: %v", err)
		}
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0); err != nil {
			return result, err
		}
	} else {
		hwnd, _, _ := consoleEntryKernel.NewProc("GetConsoleWindow").Call()
		if hwnd == 0 {
			return result, errors.New("private console has no window")
		}
		// Leave before WM_CLOSE so Windows cannot terminate the test controller.
		if ok, _, err := consoleEntryKernel.NewProc("FreeConsole").Call(); ok == 0 {
			return result, fmt.Errorf("detach event controller: %v", err)
		}
		attached = false
		if ok, _, err := windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0); ok == 0 {
			return result, fmt.Errorf("close verified private console: %v", err)
		}
	}
	select {
	case err = <-done:
		exited = true
	case <-time.After(70 * time.Second):
		return result, errors.New("UVP did not exit within shutdown budget")
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	result.ExitCode = cmd.ProcessState.ExitCode()
	_, markerErr := os.Stat(marker)
	result.MarkerPresent = markerErr == nil
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return result, markerErr
	}
	for _, handle := range handles {
		state, waitErr := windows.WaitForSingleObject(handle, 5000)
		if waitErr != nil || state != windows.WAIT_OBJECT_0 {
			return result, errors.New("owned console component remains after UVP exit")
		}
	}
	if err := checkPorts([]string{config.RedisAddress(), config.BackendAddress()}, config.MediaListeners()); err != nil {
		return result, fmt.Errorf("instance ports not released: %w", err)
	}
	if err != nil {
		if !result.MarkerPresent {
			return result, errors.New("abnormal exit lost running marker")
		}
		return result, fmt.Errorf("UVP exited abnormally with code %d; marker retained (log %s)", result.ExitCode, log.Name())
	}
	if result.MarkerPresent {
		return result, errors.New("clean console stop retained running marker")
	}
	return result, nil
}

func consoleEntryPIDs() ([]uint32, error) {
	pids := make([]uint32, 16)
	count, _, err := consoleEntryKernel.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if count == 0 {
		return nil, err
	}
	if count > uintptr(len(pids)) {
		return nil, errors.New("unexpected private console process count")
	}
	return pids[:count], nil
}

func consoleEntryOwnedProcesses(launcherPID uint32, allowed []string) (result []windows.Handle, resultErr error) {
	var handles []windows.Handle
	defer func() {
		if resultErr != nil {
			for _, handle := range handles {
				_ = windows.CloseHandle(handle)
			}
		}
	}()
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	found := make(map[string]bool)
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ParentProcessID != launcherPID {
			continue
		}
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, entry.ProcessID)
		if err != nil {
			return nil, err
		}
		handles = append(handles, handle)
		name := make([]uint16, 32768)
		size := uint32(len(name))
		err = windows.QueryFullProcessImageName(handle, 0, &name[0], &size)
		matched := false
		if err == nil {
			for _, path := range allowed {
				if strings.EqualFold(filepath.Clean(windows.UTF16ToString(name[:size])), filepath.Clean(path)) {
					if found[path] {
						return nil, fmt.Errorf("duplicate owned component %s", filepath.Base(path))
					}
					found[path] = true
					matched = true
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("launcher has unverified child process %d", entry.ProcessID)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	if len(found) != 3 || len(handles) != 3 {
		return nil, fmt.Errorf("expected three owned components, found %d", len(found))
	}
	return handles, nil
}

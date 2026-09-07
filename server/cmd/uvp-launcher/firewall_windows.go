//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

const seeMaskNoCloseProcess = 0x00000040

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

var shell32 = windows.NewLazySystemDLL("shell32.dll")
var shellExecuteEx = shell32.NewProc("ShellExecuteExW")

func requireFirewallElevation() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return firewall.NewError(firewall.ReasonPermissionDenied)
	}
	return nil
}

func elevateFirewall(ctx context.Context, action firewall.Action, interfaceID, planHash string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if action != firewall.ActionApply && action != firewall.ActionRemove {
		return firewall.NewError(firewall.ReasonInvalidInput)
	}
	if !validFirewallPlanHash(planHash) {
		return firewall.NewError(firewall.ReasonInvalidInput)
	}
	arguments := "--uvp-firewall-internal " + string(action)
	if action == firewall.ActionApply {
		parsed, err := uuid.Parse(strings.Trim(strings.TrimSpace(interfaceID), "{}"))
		if err != nil {
			return firewall.NewError(firewall.ReasonInvalidInput)
		}
		arguments += " --interface-id " + parsed.String()
	} else if interfaceID != "" {
		return firewall.NewError(firewall.ReasonInvalidInput)
	}
	arguments += " --plan-sha256 " + planHash
	executable, err := os.Executable()
	if err != nil {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	parameters, err := windows.UTF16PtrFromString(arguments)
	if err != nil {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	directory, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	info := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		lpDirectory:  directory,
		nShow:        int32(windows.SW_SHOWNORMAL),
	}
	ok, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return firewall.NewError(firewall.ReasonElevationCanceled)
		}
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	if info.hProcess == 0 || info.hProcess == windows.InvalidHandle {
		return firewall.NewError(firewall.ReasonElevationFailed)
	}
	return waitElevatedFirewall(ctx, info.hProcess)
}

func waitElevatedFirewall(ctx context.Context, process windows.Handle) error {
	defer windows.CloseHandle(process)
	for {
		status, err := windows.WaitForSingleObject(process, 100)
		if err != nil {
			return firewall.NewError(firewall.ReasonElevationFailed)
		}
		switch status {
		case windows.WAIT_OBJECT_0:
			var exitCode uint32
			if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
				return firewall.NewError(firewall.ReasonElevationFailed)
			}
			if exitCode != 0 {
				return firewall.NewError(firewallReasonFromExitCode(exitCode))
			}
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			select {
			case <-ctx.Done():
				_ = windows.TerminateProcess(process, 1)
				return ctx.Err()
			default:
			}
		default:
			return firewall.NewError(firewall.ReasonElevationFailed)
		}
	}
}

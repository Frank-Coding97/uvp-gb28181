//go:build windows

package firewall

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

const firewallMutexPrefix = "Global\\UVP-Firewall-"

func lockFirewallInstance(ctx context.Context, root string) (func(), error) {
	hash, ok := instanceHash(root)
	if !ok {
		return nil, errorFor(ReasonInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Windows mutex ownership belongs to the OS thread. Keep the goroutine on
	// one thread from before the wait through ReleaseMutex.
	runtime.LockOSThread()
	threadLocked := true
	unlockThread := func() {
		if threadLocked {
			threadLocked = false
			runtime.UnlockOSThread()
		}
	}
	name, err := windows.UTF16PtrFromString(firewallMutexPrefix + hash)
	if err != nil {
		unlockThread()
		return nil, errorFor(ReasonInvalidInput)
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		unlockThread()
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, errorFor(ReasonPermissionDenied)
		}
		return nil, errorFor(ReasonOperationFailed)
	}
	if handle == 0 || handle == windows.InvalidHandle {
		unlockThread()
		return nil, errorFor(ReasonOperationFailed)
	}
	for {
		status, waitErr := windows.WaitForSingleObject(handle, 100)
		if waitErr != nil {
			_ = windows.CloseHandle(handle)
			unlockThread()
			if errors.Is(waitErr, windows.ERROR_ACCESS_DENIED) {
				return nil, errorFor(ReasonPermissionDenied)
			}
			return nil, errorFor(ReasonOperationFailed)
		}
		switch status {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			var once sync.Once
			return func() {
				once.Do(func() {
					_ = windows.ReleaseMutex(handle)
					_ = windows.CloseHandle(handle)
					unlockThread()
				})
			}, nil
		case uint32(windows.WAIT_TIMEOUT):
			select {
			case <-ctx.Done():
				_ = windows.CloseHandle(handle)
				unlockThread()
				return nil, ctx.Err()
			default:
			}
		default:
			_ = windows.CloseHandle(handle)
			unlockThread()
			return nil, errorFor(ReasonOperationFailed)
		}
	}
}

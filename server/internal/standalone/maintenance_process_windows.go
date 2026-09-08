//go:build windows

package standalone

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Called with the installation lock held; never terminates a process.
func backupComponentsStopped(release Release) error {
	paths := []string{release.BackendExe, release.RedisExe, release.MediaExe}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return fmt.Errorf("inspect backup components: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		candidate := false
		for _, path := range paths {
			if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(path)) {
				candidate = true
			}
		}
		if !candidate {
			continue
		}
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if errors.Is(openErr, windows.ERROR_INVALID_PARAMETER) {
			continue
		}
		if openErr != nil {
			return fmt.Errorf("cannot verify backup component process %d: %w", entry.ProcessID, openErr)
		}
		name := make([]uint16, 32768)
		size := uint32(len(name))
		queryErr := windows.QueryFullProcessImageName(handle, 0, &name[0], &size)
		windows.CloseHandle(handle)
		if queryErr != nil {
			return fmt.Errorf("cannot verify backup component process %d: %w", entry.ProcessID, queryErr)
		}
		for _, path := range paths {
			if strings.EqualFold(filepath.Clean(windows.UTF16ToString(name[:size])), filepath.Clean(path)) {
				return fmt.Errorf("backup component still running: %s", filepath.Base(path))
			}
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("enumerate backup components: %w", err)
	}
	return nil
}

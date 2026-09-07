//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
)

// ShellExecute uses the desktop's default browser outside the component Job.
func openManagementBrowser(value string) error {
	if !validManagementURL(value) && !validBootstrapURL(value) {
		return errors.New("invalid management URL")
	}
	target, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, target, nil, nil, windows.SW_SHOWNORMAL)
}

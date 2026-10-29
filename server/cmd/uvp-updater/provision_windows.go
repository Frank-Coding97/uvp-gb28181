//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// defaultUpdaterState is only a fallback: the Windows control script always
// passes an explicit --state inside the installation root, because the helper
// must keep its task state next to the deployment it is allowed to modify.
func defaultUpdaterState() string {
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "uvp", "updater-state")
}

// ensureRealDirectory is the only structural guarantee Windows gives us for
// free: the path must resolve to a directory and must not be a reparse point
// (a junction or symlink would let the helper write outside its own tree).
func ensureRealDirectory(path, label string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat %s directory: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s path is not a real directory", label)
	}
	return nil
}

// ensurePrivateDirectory keeps the Unix signature so the assembly code stays
// identical; Windows has no POSIX owners to validate.
func ensurePrivateDirectory(path, label string, _ []uint32) error {
	return ensureRealDirectory(path, label)
}

func ensureBusinessInbox(path string, _, _ uint32) error {
	return ensureRealDirectory(path, "updater inbox")
}

func ensureSocketParent(socketPath string) error {
	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return errors.New("updater socket path must be absolute")
	}
	return ensureRealDirectory(filepath.Dir(filepath.Clean(socketPath)), "updater socket")
}

// registeredBusinessIdentity has no meaning on Windows: the helper and the
// business process run as the same account, so there is no separate identity to
// authorize.
func registeredBusinessIdentity(upgrade.Registration) (uint32, uint32) {
	return 0, 0
}

func provisionSharedGate(layout upgrade.RegisteredLayout, _ ...uint32) (*upgrade.GateStore, error) {
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	path := filepath.Join(layout.Root, "updater-gate", "gate.json")
	if err := ensureRealDirectory(filepath.Dir(path), "shared updater gate"); err != nil {
		return nil, err
	}
	gate, err := upgrade.OpenSharedGateStore(path)
	if err != nil {
		return nil, err
	}
	if err := initializeSharedGate(path); err != nil {
		return nil, err
	}
	return gate, nil
}

// initializeSharedGate creates normal state only with O_EXCL. An existing
// recovery or candidate gate is always preserved across helper restarts.
func initializeSharedGate(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("shared updater gate is not a regular file")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat shared updater gate: %w", err)
	}
	state := upgrade.GateState{Mode: upgrade.GateNormal, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if errors.Is(err, os.ErrExist) {
		return initializeSharedGate(path)
	}
	if err != nil {
		return fmt.Errorf("create shared updater gate: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write shared updater gate: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("sync shared updater gate: %w", err)
	}
	return file.Close()
}

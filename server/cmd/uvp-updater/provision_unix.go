//go:build linux || darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func defaultUpdaterState() string { return "/var/lib/uvp/updater-state" }

func provisionSharedGate(layout upgrade.RegisteredLayout, groupIDs ...uint32) (*upgrade.GateStore, error) {
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	path := filepath.Join(layout.Root, "updater-gate", "gate.json")
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(parent, 0750); err != nil {
			return nil, fmt.Errorf("create shared updater gate directory: %w", err)
		}
		if err := configureSharedGateOwnership(parent, groupID); err != nil {
			return nil, err
		}
		info, err = os.Lstat(parent)
	}
	if err != nil {
		return nil, fmt.Errorf("stat shared updater gate directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("shared updater gate parent is not a real directory")
	}
	if err := upgrade.VerifySharedGateParent(info); err != nil {
		return nil, err
	}
	gate, err := upgrade.OpenSharedGateStore(path)
	if err != nil {
		return nil, err
	}
	if err := initializeSharedGate(path, groupID); err != nil {
		return nil, err
	}
	return gate, nil
}

// ensurePrivateDirectory provisions helper-owned state and verified roots. A
// pre-existing directory is never made trusted by merely chmod'ing away a
// write bit: its owner and every replaceable parent are checked first.
func ensurePrivateDirectory(path, label string, allowedOwners []uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	path = filepath.Clean(path)
	if err := ensureTrustedAncestors(filepath.Dir(path), allowedOwners); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate %s parent: %w", label, err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	if err := ensureTrustedAncestors(filepath.Dir(path), allowedOwners); err != nil {
		return fmt.Errorf("validate %s parent: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat %s directory: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s path is not a real directory", label)
	}
	owner, ok := fileOwnerUID(info)
	if !ok || !containsUID(allowedOwners, owner) {
		return fmt.Errorf("%s directory owner UID %d is not trusted", label, owner)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("%s directory must not be group/world writable", label)
	}
	if info.Mode().Perm() != 0700 {
		if err := os.Chmod(path, 0700); err != nil {
			return fmt.Errorf("protect %s directory: %w", label, err)
		}
	}
	return nil
}

// ensureBusinessInbox is the only relaxed permission root in the helper. The
// business process may create one opaque upload token here, while verified
// artifacts, task state, rollback data, and event logs remain helper-private.
// The parent must still be administrator-owned and non-writable so the
// business process cannot replace the inbox directory itself.
func ensureBusinessInbox(path string, businessUID, businessGID uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("updater inbox path must be absolute")
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(path, 0770); err != nil {
		return fmt.Errorf("create updater inbox directory: %w", err)
	}
	if err := ensureTrustedAncestors(filepath.Dir(path), []uint32{uint32(os.Geteuid()), 0}); err != nil {
		return fmt.Errorf("validate updater inbox parent: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat updater inbox directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("updater inbox path is not a real directory")
	}
	owner, ownerOK := fileOwnerUID(info)
	if !ownerOK || (owner != 0 && owner != uint32(os.Geteuid())) {
		return fmt.Errorf("updater inbox owner UID %d is not trusted", owner)
	}
	group, groupOK := fileGroupID(info)
	if !groupOK {
		return errors.New("updater inbox group is unavailable")
	}
	if os.Geteuid() == 0 && group != businessGID {
		if err := os.Chown(path, 0, int(businessGID)); err != nil {
			return fmt.Errorf("set updater inbox group: %w", err)
		}
		info, err = os.Lstat(path)
		if err != nil {
			return fmt.Errorf("restat updater inbox directory: %w", err)
		}
		group, groupOK = fileGroupID(info)
	}
	if !groupOK || group != businessGID {
		return fmt.Errorf("updater inbox group %d does not match registered business GID %d", group, businessGID)
	}
	if info.Mode().Perm()&0007 != 0 || info.Mode().Perm() != 0770 {
		if err := os.Chmod(path, 0770); err != nil {
			return fmt.Errorf("protect updater inbox directory: %w", err)
		}
	}
	if businessUID == ^uint32(0) || businessGID == ^uint32(0) {
		return errors.New("registered business identity is outside the supported range")
	}
	return nil
}

// ensureTrustedAncestors rejects a writable or user-owned ancestor before a
// privileged helper relies on a path. A root-owned sticky directory such as
// /tmp is safe for an already-created root-owned child and is allowed; the
// immediate parent of the state/inbox is still required to be non-writable.
func ensureTrustedAncestors(path string, allowedOwners []uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	for current := filepath.Clean(path); current != string(filepath.Separator); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// macOS exposes /var as a root-owned symlink to /private/var. It is
			// safe when the link and its resolved directory ancestry are trusted;
			// rejecting every system symlink would make the registered helper
			// unusable on the supported desktop platform.
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return fmt.Errorf("resolve %s: %w", current, resolveErr)
			}
			resolvedInfo, statErr := os.Stat(resolved)
			if statErr != nil {
				return fmt.Errorf("stat resolved ancestor %s: %w", resolved, statErr)
			}
			if !resolvedInfo.IsDir() {
				return fmt.Errorf("%s is not a real directory", current)
			}
			if err := validateTrustedDirectory(resolved, resolvedInfo, allowedOwners); err != nil {
				return err
			}
			if err := ensureTrustedAncestors(filepath.Dir(resolved), allowedOwners); err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a real directory", current)
		}
		if err := validateTrustedDirectory(current, info, allowedOwners); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedDirectory(path string, info os.FileInfo, allowedOwners []uint32) error {
	if info == nil || !info.IsDir() {
		return fmt.Errorf("%s is not a real directory", path)
	}
	owner, ok := fileOwnerUID(info)
	if !ok || !containsUID(allowedOwners, owner) {
		return fmt.Errorf("%s owner UID %d is not trusted", path, owner)
	}
	mode := info.Mode()
	if mode.Perm()&0022 != 0 && !(owner == 0 && mode&os.ModeSticky != 0) {
		return fmt.Errorf("%s must not be group/world writable", path)
	}
	return nil
}

func ensureSocketParent(socketPath string) error {
	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return errors.New("updater socket path must be absolute")
	}
	parent := filepath.Dir(filepath.Clean(socketPath))
	if err := os.MkdirAll(parent, 0750); err != nil {
		return fmt.Errorf("create updater socket directory: %w", err)
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("stat updater socket directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("updater socket parent is not a real directory")
	}
	owner, ok := fileOwnerUID(info)
	if !ok || (owner != 0 && owner != uint32(os.Geteuid())) {
		return fmt.Errorf("updater socket parent owner UID %d is not trusted", owner)
	}
	if info.Mode().Perm()&0022 != 0 {
		return errors.New("updater socket parent must not be group/world writable")
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint32(stat.Uid), true
}

func fileGroupID(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint32(stat.Gid), true
}

func registeredBusinessIdentity(registration upgrade.Registration) (uint32, uint32) {
	uid, gid := registration.BusinessUID, registration.BusinessGID
	if uid == 0 && os.Geteuid() != 0 {
		uid = uint32(os.Geteuid())
	}
	if gid == 0 && os.Geteuid() != 0 {
		gid = uint32(os.Getegid())
	}
	return uid, gid
}

func configureSharedGateOwnership(path string, groupIDs ...uint32) error {
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	if err := os.Chmod(path, 0750); err != nil {
		return fmt.Errorf("protect shared updater gate directory: %w", err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, 0, int(groupID)); err != nil {
			return fmt.Errorf("set shared updater gate directory ownership: %w", err)
		}
	}
	return nil
}

// initializeSharedGate creates normal state only with O_EXCL. An existing
// recovery or candidate gate is always preserved across helper restarts.
func initializeSharedGate(path string, groupIDs ...uint32) error {
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("shared updater gate is not a regular file")
		}
		if err := os.Chmod(path, 0640); err != nil {
			return fmt.Errorf("protect existing shared updater gate: %w", err)
		}
		if os.Geteuid() == 0 {
			if err := os.Chown(path, 0, int(groupID)); err != nil {
				return fmt.Errorf("set existing shared updater gate ownership: %w", err)
			}
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
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if errors.Is(err, os.ErrExist) {
		return initializeSharedGate(path, groupID)
	}
	if err != nil {
		return fmt.Errorf("create shared updater gate: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0640); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("protect shared updater gate: %w", err)
	}
	if os.Geteuid() == 0 {
		if err := file.Chown(0, int(groupID)); err != nil {
			_ = os.Remove(path)
			return fmt.Errorf("set shared updater gate ownership: %w", err)
		}
	}
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write shared updater gate: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("sync shared updater gate: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close shared updater gate: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open shared updater gate directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync shared updater gate directory: %w", err)
	}
	return nil
}

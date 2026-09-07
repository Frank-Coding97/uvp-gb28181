//go:build !windows

package standalone

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const (
	secureTempPrefix = ".uvp-secure-"
	secureLockName   = ".uvp-config.lock"
)

var (
	errSecurePath        = errors.New("standalone: secure path rejected")
	errSecureACL         = errors.New("standalone: secure permissions rejected")
	errSecureLockTimeout = errors.New("standalone: configuration lock timed out")
)

const secureLockWait = 5 * time.Second

func withConfigLock(dir string, fn func() error) error {
	if fn == nil {
		return errors.New("standalone: configuration lock callback is required")
	}
	if err := ensureOtherDirectory(dir); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, secureLockName)
	if _, err := ensureOtherTarget(lockPath, true, false); err != nil {
		return err
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open configuration lock %q: %w", lockPath, err)
	}
	defer lockFile.Close()
	if err := os.Chmod(lockPath, 0o600); err != nil {
		return fmt.Errorf("protect configuration lock %q: %w", lockPath, err)
	}

	deadline := time.Now().Add(secureLockWait)
	for {
		err = unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return fmt.Errorf("lock configuration directory %q: %w", dir, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock configuration directory %q: %w", dir, errSecureLockTimeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer func() { _ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN) }()
	return fn()
}

func protectConfigDir(dir string, initializing bool) error {
	if err := ensureOtherDirectory(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("inspect configuration directory %q: %w", dir, err)
	}
	if initializing && len(entries) == 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("protect configuration directory %q: %w", dir, err)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspect configuration directory %q: %w", dir, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("configuration directory %q has overbroad permissions: %w", dir, errSecureACL)
	}
	return nil
}

func readSecureConfigFile(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := protectConfigDir(dir, false); err != nil {
		return nil, err
	}
	if _, err := ensureOtherTarget(path, false, false); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect secure file %q: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("secure file %q has overbroad permissions: %w", path, errSecureACL)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secure file %q: %w", path, err)
	}
	// Recheck the name after reading. This catches ordinary symlink swaps; the
	// same-privilege race window is intentionally outside this helper's claim.
	if _, err := ensureOtherTarget(path, false, false); err != nil {
		return nil, err
	}
	return data, nil
}

func writeSecureConfigFile(path string, data []byte, replace bool, hook func(stage string) error) error {
	dir := filepath.Dir(path)
	if err := protectConfigDir(dir, false); err != nil {
		return err
	}
	exists, err := ensureOtherTarget(path, true, false)
	if err != nil {
		return err
	}
	if exists && !replace {
		return fs.ErrExist
	}
	if exists {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return fmt.Errorf("inspect existing secure file %q: %w", path, statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("existing secure file %q has overbroad permissions: %w", path, errSecureACL)
		}
	}

	tempPath, tempFile, err := createOtherTempFile(dir, path, hook)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := callSecureHook(hook, "write", path); err != nil {
		return err
	}
	n, err := tempFile.Write(data)
	if err != nil {
		return fmt.Errorf("write secure file %q: %w", path, err)
	}
	if n != len(data) {
		return fmt.Errorf("write secure file %q: %w", path, io.ErrShortWrite)
	}
	if err := callSecureHook(hook, "flush", path); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("flush secure file %q: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close secure file %q: %w", path, err)
	}
	if err := callSecureHook(hook, "acl", path); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("protect secure file %q: %w", path, err)
	}
	if _, err := ensureOtherTarget(tempPath, false, false); err != nil {
		return err
	}
	if info, statErr := os.Stat(tempPath); statErr != nil {
		return fmt.Errorf("inspect temporary secure file %q: %w", path, statErr)
	} else if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("temporary secure file %q has overbroad permissions: %w", path, errSecureACL)
	}

	// Check both names again immediately before publication. The helper does
	// not claim to defeat a same-privilege attacker racing this check.
	if err := protectConfigDir(dir, false); err != nil {
		return err
	}
	if exists, err := ensureOtherTarget(path, true, false); err != nil {
		return err
	} else if exists && !replace {
		return fs.ErrExist
	}
	if err := callSecureHook(hook, "publish", path); err != nil {
		return err
	}
	if replace {
		if err := os.Rename(tempPath, path); err != nil {
			return fmt.Errorf("publish secure file %q: %w", path, err)
		}
	} else {
		// A hard link creates the destination atomically and fails when another
		// initializer won the no-replace race. Both names are in one directory.
		if err := os.Link(tempPath, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fs.ErrExist
			}
			return fmt.Errorf("publish secure file %q: %w", path, err)
		}
		if err := os.Remove(tempPath); err != nil {
			return fmt.Errorf("remove temporary secure file %q: %w", path, err)
		}
	}
	removeTemp = false
	if _, err := ensureOtherTarget(path, false, false); err != nil {
		return err
	}
	if info, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("inspect published secure file %q: %w", path, statErr)
	} else if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("published secure file %q has overbroad permissions: %w", path, errSecureACL)
	}
	return nil
}

func createOtherTempFile(dir, target string, hook func(stage string) error) (string, *os.File, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := randomSecureTempName()
		if err != nil {
			return "", nil, errors.New("generate secure temporary name")
		}
		path := filepath.Join(dir, name)
		if err := callSecureHook(hook, "create", target); err != nil {
			return "", nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return path, file, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, fmt.Errorf("create secure temporary file %q: %w", target, err)
		}
	}
	return "", nil, errors.New("create secure temporary file: random name collision")
}

func randomSecureTempName() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return secureTempPrefix + hex.EncodeToString(random[:]), nil
}

func callSecureHook(hook func(stage string) error, stage, path string) error {
	if hook == nil {
		return nil
	}
	if err := hook(stage); err != nil {
		// Do not wrap the callback's text: callbacks may have been supplied by a
		// parser and must not accidentally echo configuration bytes.
		return fmt.Errorf("secure file %q %s hook failed", path, stage)
	}
	return nil
}

func ensureOtherDirectory(path string) error {
	exists, err := ensureOtherTarget(path, false, true)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("secure directory %q: %w", path, fs.ErrNotExist)
	}
	return nil
}

func ensureOtherTarget(path string, allowMissing, directory bool) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if allowMissing && errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect secure path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("secure path %q is a symbolic link: %w", path, errSecurePath)
	}
	if directory {
		if !info.IsDir() {
			return false, fmt.Errorf("secure path %q is not a directory: %w", path, errSecurePath)
		}
	} else if !info.Mode().IsRegular() {
		return false, fmt.Errorf("secure path %q is not a regular file: %w", path, errSecurePath)
	}
	return true, nil
}

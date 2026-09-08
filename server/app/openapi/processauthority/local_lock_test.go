package processauthority

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func privateStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	privateAuthorityTestPermissions(t, dir)
	return dir
}

func TestLocalAuthorityLockPersistsDomainAndSerializesOwners(t *testing.T) {
	dir := privateStateDir(t)
	owner, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = owner.Close() })
	domain, err := owner.DomainID()
	require.NoError(t, err)
	require.Len(t, domain, 32)
	other, err := AcquireLocalLock(dir)
	require.ErrorIs(t, err, ErrLocalAuthorityBusy)
	require.Nil(t, other)
	copy := *owner
	require.NoError(t, copy.Close())
	require.NoError(t, owner.Close())
	_, err = owner.DomainID()
	require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
	next, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	defer next.Close()
	nextDomain, err := next.DomainID()
	require.NoError(t, err)
	require.Equal(t, domain, nextDomain)
}

func TestLocalAuthorityProcessHelper(t *testing.T) {
	dir := os.Getenv("UVP_LOCAL_AUTHORITY_CHILD")
	if dir == "" {
		return
	}
	owner, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	domain, err := owner.DomainID()
	require.NoError(t, err)
	fmt.Fprintln(os.Stdout, domain)
	_, _ = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, owner.Close())
}

func TestLocalAuthorityLockRequiresActualProcessExit(t *testing.T) {
	dir := privateStateDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLocalAuthorityProcessHelper$")
	cmd.Env = append(os.Environ(), "UVP_LOCAL_AUTHORITY_CHILD="+dir)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer input.Close()
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	domain, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err)
	require.Len(t, strings.TrimSpace(domain), 32)
	owner, err := AcquireLocalLock(dir)
	require.ErrorIs(t, err, ErrLocalAuthorityBusy)
	require.Nil(t, owner)
	require.NoError(t, cmd.Process.Kill())
	err = cmd.Wait()
	waited = true
	require.Error(t, err, "the old owner was killed, not gracefully closed")
	owner, err = AcquireLocalLock(dir)
	require.NoError(t, err)
	defer owner.Close()
	next, err := owner.DomainID()
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(domain), next)
}

func TestLocalAuthorityRejectsUntrustedAndDamagedState(t *testing.T) {
	for _, kind := range []string{"relative", "missing", "public-directory", "empty-file", "invalid-file", "oversize", "public-file", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			if runtime.GOOS == "windows" && (kind == "public-directory" || kind == "public-file" || kind == "symlink") {
				t.Skip("POSIX modes/symlink fixture; Windows ACL cases are separate")
			}
			dir := privateStateDir(t)
			path := filepath.Join(dir, "api-authority.lock")
			switch kind {
			case "relative":
				dir = "relative-state"
			case "missing":
				dir = filepath.Join(dir, "missing")
			case "public-directory":
				require.NoError(t, os.Chmod(dir, 0777))
			case "empty-file", "invalid-file", "oversize", "public-file":
				data := []byte("invalid")
				if kind == "empty-file" {
					data = nil
				}
				if kind == "oversize" {
					data = []byte(strings.Repeat("x", 513))
				}
				require.NoError(t, os.WriteFile(path, data, 0600))
				if kind == "public-file" {
					require.NoError(t, os.Chmod(path, 0666))
				}
			case "symlink", "hardlink":
				target := filepath.Join(dir, "other")
				require.NoError(t, os.WriteFile(target, []byte("not-a-domain"), 0600))
				if kind == "symlink" {
					require.NoError(t, os.Symlink(target, path))
				} else {
					require.NoError(t, os.Link(target, path))
				}
			}
			owner, err := AcquireLocalLock(dir)
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
			require.Nil(t, owner)
		})
	}
}

func TestLocalAuthorityDetectsReplacementAndStaysPoisoned(t *testing.T) {
	for _, kind := range []string{"content", "file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateStateDir(t)
			owner, err := AcquireLocalLock(dir)
			require.NoError(t, err)
			defer owner.Close()
			path := filepath.Join(dir, "api-authority.lock")
			original, err := readLocalDomain(owner.state.file)
			require.NoError(t, err)
			switch kind {
			case "content":
				// Write using the owned descriptor because Windows byte locks
				// correctly deny unrelated handles, even in this same process.
				_, err = owner.state.file.WriteAt([]byte("!"), 0)
				require.NoError(t, err)
			case "file":
				err = os.Rename(path, path+".old")
				if runtime.GOOS == "windows" && err != nil {
					require.True(t, errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(32)), "unexpected rename failure: %v", err)
					require.NoError(t, owner.Check(), "OS denied rename without invalidating the owner")
					require.NoFileExists(t, path+".old")
					return
				}
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, original, 0600))
				replacement, err := AcquireLocalLock(dir)
				if replacement != nil {
					defer replacement.Close()
				}
				require.ErrorIs(t, err, ErrLocalAuthorityUnavailable, "copying domain bytes to a new file cannot inherit authority")
			case "directory":
				err = os.Rename(dir, dir+".old")
				if runtime.GOOS == "windows" && err != nil {
					require.True(t, errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(32)), "unexpected rename failure: %v", err)
					require.NoError(t, owner.Check())
					require.NoDirExists(t, dir+".old")
					return
				}
				require.NoError(t, err)
				t.Cleanup(func() { _ = os.Rename(dir+".old", dir) })
			}
			_, err = owner.DomainID()
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
			if kind == "content" {
				_, err = owner.state.file.WriteAt(original, 0)
				require.NoError(t, err)
				_, err = owner.DomainID()
				require.ErrorIs(t, err, ErrLocalAuthorityUnavailable, "repair cannot revive poisoned authority")
			}
		})
	}
}

func TestLocalAuthorityCopiedRecordCannotInheritClosedOwner(t *testing.T) {
	dir := privateStateDir(t)
	owner, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	original, err := readLocalDomain(owner.state.file)
	require.NoError(t, err)
	require.NoError(t, owner.Close())
	path := filepath.Join(dir, lockName)
	require.NoError(t, os.Rename(path, path+".old"))
	require.NoError(t, os.WriteFile(path, original, 0600))
	replacement, err := AcquireLocalLock(dir)
	if replacement != nil {
		defer replacement.Close()
	}
	require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
}

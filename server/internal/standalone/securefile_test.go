package standalone

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSecureFileAtomicWriteReadAndNoReplace(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "config 中文 #1.yml")
	data := []byte("server:\n  appdebug: false\n")

	var stages []string
	err := writeSecureConfigFile(target, data, false, func(stage string) error {
		stages = append(stages, stage)
		return nil
	})
	require.NoError(t, err)
	got, err := readSecureConfigFile(target)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.Equal(t, []string{"create", "write", "flush", "acl", "publish"}, stages)

	err = writeSecureConfigFile(target, []byte("changed\n"), false, nil)
	require.ErrorIs(t, err, fs.ErrExist)
	got, err = readSecureConfigFile(target)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

func TestSecureFileReplaceIsAtomicAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "derived #配置.conf")
	require.NoError(t, writeSecureConfigFile(target, []byte("one\n"), false, nil))
	require.NoError(t, writeSecureConfigFile(target, []byte("two\n"), true, nil))

	got, err := readSecureConfigFile(target)
	require.NoError(t, err)
	require.Equal(t, []byte("two\n"), got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), secureTempPrefix), "temporary file remains: %s", entry.Name())
	}
}

func TestSecureFileFaultHooksDoNotPublishPartialData(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))

	for _, stage := range []string{"create", "write", "flush", "acl", "publish"} {
		t.Run(stage, func(t *testing.T) {
			target := filepath.Join(dir, "fault-"+stage+".yml")
			payload := []byte("secret-like payload that must not be published\n")
			err := writeSecureConfigFile(target, payload, false, func(got string) error {
				if got == stage {
					return errors.New("injected hook failure")
				}
				return nil
			})
			require.Error(t, err)
			require.NotContains(t, err.Error(), string(payload))
			_, statErr := os.Stat(target)
			require.ErrorIs(t, statErr, fs.ErrNotExist)
			entries, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)
			for _, entry := range entries {
				require.False(t, strings.HasPrefix(entry.Name(), secureTempPrefix), "temporary file remains: %s", entry.Name())
			}
		})
	}
}

func TestSecureFileLockSerializesConcurrentInitializers(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withConfigLock(dir, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first initializer did not acquire lock")
	}

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withConfigLock(dir, func() error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second initializer entered while first held lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-firstDone)
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("second initializer did not acquire released lock")
	}
	require.NoError(t, <-secondDone)
}

func TestSecureFileLockReleasesAfterCallbackError(t *testing.T) {
	dir := t.TempDir()
	want := errors.New("callback failed")
	require.ErrorIs(t, withConfigLock(dir, func() error { return want }), want)
	require.NoError(t, withConfigLock(dir, func() error { return nil }))
}

func TestSecureFileRejectsReparseOrSymlinkTargets(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	target := filepath.Join(dir, "config.yml")
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	err := writeSecureConfigFile(target, []byte("replacement"), true, nil)
	require.Error(t, err)
	got, readErr := os.ReadFile(outside)
	require.NoError(t, readErr)
	require.Equal(t, []byte("outside"), got)
	_, err = readSecureConfigFile(target)
	require.Error(t, err)
}

func TestSecureFileRejectsOverbroadNonWindowsModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses DACL checks rather than POSIX mode bits")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o755))
	// An empty first-use directory may be tightened by the helper.
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "mode.yml")
	require.NoError(t, writeSecureConfigFile(target, []byte("ok"), false, nil))
	require.NoError(t, os.Chmod(target, 0o644))
	_, err := readSecureConfigFile(target)
	require.Error(t, err)
}

func TestSecureFileReplaceRejectsOverbroadExistingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses DACL checks rather than POSIX mode bits")
	}
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "derived.conf")
	require.NoError(t, writeSecureConfigFile(target, []byte("old"), false, nil))
	require.NoError(t, os.Chmod(target, 0o644))
	err := writeSecureConfigFile(target, []byte("new"), true, nil)
	require.Error(t, err)
	got, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, []byte("old"), got)
}

func TestSecureFileConcurrentReplaceReadersSeeCompleteValues(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "concurrent.yml")
	require.NoError(t, writeSecureConfigFile(target, []byte("initial"), false, nil))

	const readerCount = 8
	const writeCount = 32
	expected := map[string]struct{}{"initial": {}}
	for i := 0; i < writeCount; i++ {
		expected[fmt.Sprintf("value-%03d", i)] = struct{}{}
	}

	var wg sync.WaitGroup
	errs := make(chan error, readerCount)
	ready := make(chan struct{}, readerCount)
	start := make(chan struct{})
	done := make(chan struct{})
	var readCount atomic.Int64
	for i := 0; i < readerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ready <- struct{}{}
			for {
				select {
				case <-done:
					return
				default:
				}
				payload, err := readSecureConfigFile(target)
				if err != nil {
					errs <- fmt.Errorf("concurrent read: %w", err)
					return
				}
				if _, ok := expected[string(payload)]; !ok {
					errs <- fmt.Errorf("concurrent read returned incomplete payload %q", payload)
					return
				}
				readCount.Add(1)
			}
		}()
	}
	close(start)
	for i := 0; i < readerCount; i++ {
		<-ready
	}
	var writerErr error
	for i := 0; i < writeCount; i++ {
		payload := []byte(fmt.Sprintf("value-%03d", i))
		if err := writeSecureConfigFile(target, payload, true, nil); err != nil {
			writerErr = err
			break
		}
	}
	close(done)
	wg.Wait()
	close(errs)
	require.NoError(t, writerErr)
	require.Positive(t, readCount.Load())
	for err := range errs {
		require.NoError(t, err)
	}
	got, err := readSecureConfigFile(target)
	require.NoError(t, err)
	_, ok := expected[string(got)]
	require.True(t, ok, "final payload = %q", got)
}

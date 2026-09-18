package logging

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writerOptionsForTest(t *testing.T) writerOptions {
	t.Helper()
	return writerOptions{path: filepath.Join(t.TempDir(), "server.log"), maxBytes: 128, maxBackups: 2, maxAge: 15 * 24 * time.Hour, now: func() time.Time { return time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC) }}
}
func mustWriter(t *testing.T, o writerOptions) *fileWriter {
	t.Helper()
	w, e := openFileWriter(o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}
func managedFiles(t *testing.T, o writerOptions) (int, int64) {
	t.Helper()
	entries, e := os.ReadDir(filepath.Dir(o.path))
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	var size int64
	for _, e := range entries {
		if e.Name() == filepath.Base(o.path) || strings.Contains(e.Name(), ".uvp-v1-") {
			s, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			if s.Mode().IsRegular() {
				count++
				size += s.Size()
			}
		}
	}
	return count, size
}
func TestLoggingWriterRotationAndAge(t *testing.T) {
	o := writerOptionsForTest(t)
	now := o.now()
	o.now = func() time.Time { return now }
	w := mustWriter(t, o)
	p := bytes.Repeat([]byte("x"), 100)
	for i := 0; i < 10; i++ {
		if n, e := w.Write(p); e != nil || n != len(p) {
			t.Fatalf("write %d/%v", n, e)
		}
	}
	count, size := managedFiles(t, o)
	if count != 3 || size > 384 {
		t.Fatalf("retention count=%d bytes=%d", count, size)
	}
	now = now.Add(o.maxAge + time.Second)
	if e := w.Maintain(); e != nil {
		t.Fatal(e)
	}
	count, _ = managedFiles(t, o)
	if count != 1 {
		t.Fatalf("idle expiry count=%d", count)
	}
}
func TestLoggingWriterCompressionBudget(t *testing.T) {
	o := writerOptionsForTest(t)
	o.maxBytes = 5 << 20
	o.maxBackups = 7
	o.compress = true
	peak := int64(0)
	o.before = func(string) error {
		_, n := managedFiles(t, o)
		if n > peak {
			peak = n
		}
		return nil
	}
	w := mustWriter(t, o)
	data := make([]byte, 1<<20)
	if _, e := rand.Read(data); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 45; i++ {
		if _, e := w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	_, size := managedFiles(t, o)
	if size > 40<<20 || peak > 46<<20 {
		t.Fatalf("steady %d peak %d", size, peak)
	}
	if size < 35<<20 {
		t.Fatalf("incompressible records lost: %d", size)
	}
}
func TestLoggingWriterLegacy(t *testing.T) {
	o := writerOptionsForTest(t)
	old := bytes.Repeat([]byte("legacy"), 100)
	if e := os.WriteFile(o.path, old, 0644); e != nil {
		t.Fatal(e)
	}
	unmanaged := o.path + ".old"
	if e := os.WriteFile(unmanaged, old, 0644); e != nil {
		t.Fatal(e)
	}
	w := mustWriter(t, o)
	if w.HistoricalBytes() < int64(2*len(old)) {
		t.Fatal("historical bytes not reported")
	}
	if _, e := w.Write([]byte("new\n")); e != nil {
		t.Fatal(e)
	}
	if e := w.Maintain(); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(unmanaged)
	if e != nil || !bytes.Equal(got, old) {
		t.Fatal("historical file altered")
	}
	matches, _ := filepath.Glob(o.path + ".legacy-*")
	if len(matches) != 1 {
		t.Fatal("oversized active not archived once")
	}
	got, _ = os.ReadFile(matches[0])
	if !bytes.Equal(got, old) {
		t.Fatal("oversized history truncated")
	}
}
func TestLoggingWriterFaults(t *testing.T) {
	for _, operation := range []string{"open", "write", "rename", "remove", "compress", "compress_write", "compress_close", "publish", "sync", "directory_sync"} {
		t.Run(operation, func(t *testing.T) {
			o := writerOptionsForTest(t)
			o.compress = strings.HasPrefix(operation, "compress") || operation == "publish"
			fail := false
			o.before = func(op string) error {
				if fail && op == operation {
					return errors.New("injected")
				}
				return nil
			}
			w := mustWriter(t, o)
			if _, e := w.Write(bytes.Repeat([]byte("a"), 100)); e != nil {
				t.Fatal(e)
			}
			if operation == "remove" {
				for i := 0; i < 2; i++ {
					_, _ = w.Write(bytes.Repeat([]byte("b"), 100))
				}
			}
			fail = true
			var err error
			if operation == "sync" {
				err = w.Sync()
			} else {
				_, err = w.Write(bytes.Repeat([]byte("c"), 100))
			}
			if err == nil {
				t.Fatalf("%s error hidden", operation)
			}
			_, before := managedFiles(t, o)
			for i := 0; i < 20; i++ {
				_, _ = w.Write(bytes.Repeat([]byte("d"), 100))
			}
			_, after := managedFiles(t, o)
			if after > before {
				t.Fatalf("fault grew retained output %d -> %d", before, after)
			}
		})
	}
	t.Run("short write", func(t *testing.T) {
		o := writerOptionsForTest(t)
		o.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:len(b)/2]) }
		w := mustWriter(t, o)
		n, e := w.Write([]byte("partial record"))
		if n >= 14 || !errors.Is(e, io.ErrShortWrite) {
			t.Fatalf("short write %d/%v", n, e)
		}
	})
}
func TestLoggingWriterProcessLock(t *testing.T) {
	if p := os.Getenv("UVP_LOG_LOCK_CHILD"); p != "" {
		o := writerOptions{path: p, maxBytes: 128, maxBackups: 2, maxAge: time.Hour}
		w, e := openFileWriter(o)
		if e != nil {
			os.Exit(23)
		}
		_ = w.Close()
		return
	}
	o := writerOptionsForTest(t)
	w := mustWriter(t, o)
	run := func(path string) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLoggingWriterProcessLock$")
		cmd.Env = append(os.Environ(), "UVP_LOG_LOCK_CHILD="+path)
		return cmd.Run()
	}
	if e := run(o.path); e == nil {
		t.Fatal("second process acquired same lock")
	}
	if e := run(o.path + ".other"); e != nil {
		t.Fatalf("independent path blocked: %v", e)
	}
	_ = w.Close()
	if e := run(o.path); e != nil {
		t.Fatalf("lock not released: %v", e)
	}
}
func TestLoggingWriterSymlinkSafety(t *testing.T) {
	t.Run("parent link", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "persistent")
		_ = os.Mkdir(target, 0700)
		link := filepath.Join(dir, "logs")
		_ = os.Symlink(target, link)
		o := writerOptionsForTest(t)
		o.path = filepath.Join(link, "server.log")
		w := mustWriter(t, o)
		if _, e := w.Write([]byte("ok\n")); e != nil {
			t.Fatal(e)
		}
		if _, e := os.Stat(filepath.Join(target, "server.log")); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("active link", func(t *testing.T) {
		o := writerOptionsForTest(t)
		target := o.path + ".target"
		_ = os.WriteFile(target, []byte("untouched"), 0600)
		_ = os.Symlink(target, o.path)
		w, e := openFileWriter(o)
		if e == nil {
			_ = w.Close()
			t.Fatal("active symlink accepted")
		}
		got, _ := os.ReadFile(target)
		if string(got) != "untouched" {
			t.Fatal("target modified")
		}
	})
	t.Run("replace before rotate", func(t *testing.T) {
		o := writerOptionsForTest(t)
		target := o.path + ".target"
		_ = os.WriteFile(target, []byte("untouched"), 0600)
		swapped := false
		o.before = func(op string) error {
			if op == "rename" && !swapped {
				swapped = true
				_ = os.Rename(o.path, o.path+".external")
				return os.Symlink(target, o.path)
			}
			return nil
		}
		w := mustWriter(t, o)
		_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
		if _, e := w.Write(bytes.Repeat([]byte("b"), 100)); e == nil {
			t.Fatal("replacement before rotate accepted")
		}
		got, _ := os.ReadFile(target)
		if string(got) != "untouched" {
			t.Fatal("target modified")
		}
	})
}
func TestLoggingWriterBlockingClose(t *testing.T) {
	o := writerOptionsForTest(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	once := sync.Once{}
	o.before = func(op string) error {
		if op == "write" {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	}
	w := mustWriter(t, o)
	go func() { _, _ = w.Write([]byte("record")); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("write was not performed")
	}
	closed := make(chan struct{})
	go func() { _ = w.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close did not wait for write")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-done
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not complete")
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestLoggingWriterCompressionRecovery(t *testing.T) {
	for _, failPublish := range []bool{false, true} {
		t.Run(fmt.Sprint(failPublish), func(t *testing.T) {
			o := writerOptionsForTest(t)
			o.maxBytes = 1024
			o.compress = true
			o.before = func(op string) error {
				if failPublish && op == "publish" {
					return errors.New("publish failure")
				}
				return nil
			}
			w := mustWriter(t, o)
			p := bytes.Repeat([]byte("record\n"), 130)
			if _, e := w.Write(p); e != nil {
				t.Fatal(e)
			}
			_, err := w.Write(bytes.Repeat([]byte("next\n"), 100))
			if failPublish && err == nil {
				t.Fatal("publish failure hidden")
			}
			if !failPublish && err != nil {
				t.Fatal(err)
			}
			if failPublish {
				raw, _ := filepath.Glob(o.path + ".uvp-v1-*.log")
				if len(raw) != 1 {
					t.Fatal("publish failure deleted original")
				}
				got, _ := os.ReadFile(raw[0])
				if !bytes.Equal(got, p) {
					t.Fatal("publish failure changed original")
				}
			}
			_ = w.Close()
			o.before = nil
			w = mustWriter(t, o)
			if failPublish {
				return
			}
			files, _ := filepath.Glob(o.path + ".uvp-v1-*.gz")
			if len(files) != 1 {
				t.Fatalf("complete compressed backup count=%d", len(files))
			}
			f, e := os.Open(files[0])
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			z, e := gzip.NewReader(f)
			if e != nil {
				t.Fatal(e)
			}
			got, e := io.ReadAll(z)
			_ = z.Close()
			if e != nil || !bytes.Equal(got, p) {
				t.Fatal("compressed backup changed original records")
			}
		})
	}
}
func TestLoggingWriterDeletionIdentityAndPermissions(t *testing.T) {
	t.Run("permissions and hardlink", func(t *testing.T) {
		o := writerOptionsForTest(t)
		_ = os.WriteFile(o.path, []byte("old"), 0600)
		_ = os.Chmod(o.path, 0666)
		if w, e := openFileWriter(o); e == nil {
			_ = w.Close()
			t.Fatal("writable-by-others accepted")
		}
		_ = os.Chmod(o.path, 0600)
		_ = os.Link(o.path, o.path+".hardlink")
		if w, e := openFileWriter(o); e == nil {
			_ = w.Close()
			t.Fatal("hardlinked active accepted")
		}
	})
	for _, link := range []bool{false, true} {
		t.Run(fmt.Sprint(link), func(t *testing.T) {
			o := writerOptionsForTest(t)
			o.maxBackups = 1
			w := mustWriter(t, o)
			for i := 0; i < 2; i++ {
				_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
			}
			files, _ := filepath.Glob(o.path + ".uvp-v1-*.log")
			if len(files) != 1 {
				t.Fatal("no backup")
			}
			target := o.path + ".external-target"
			_ = os.WriteFile(target, []byte("untouched"), 0600)
			w.options.before = func(op string) error {
				if op == "remove" {
					_ = os.Rename(files[0], files[0]+".held")
					if link {
						return os.Symlink(target, files[0])
					}
					return os.WriteFile(files[0], []byte("replacement"), 0600)
				}
				return nil
			}
			if _, e := w.Write(bytes.Repeat([]byte("b"), 100)); e == nil {
				t.Fatal("changed backup removed")
			}
			got, _ := os.ReadFile(target)
			if string(got) != "untouched" {
				t.Fatal("target touched")
			}
			if _, e := os.Lstat(files[0]); e != nil {
				t.Fatal("replacement was removed")
			}
		})
	}
}

func TestLoggingWriterCompressionFailureCopies(t *testing.T) {
	for _, stage := range []string{"rollback_remove", "unlink_directory_sync"} {
		t.Run(stage, func(t *testing.T) {
			o := writerOptionsForTest(t)
			o.compress = true
			o.maxBytes = 1024
			removeStarted := false
			o.before = func(op string) error {
				if op == "remove" {
					removeStarted = true
					if stage == "rollback_remove" {
						return errors.New("unlink failure")
					}
				}
				if op == "directory_sync" && removeStarted && stage == "unlink_directory_sync" {
					return errors.New("directory sync failure")
				}
				return nil
			}
			w := mustWriter(t, o)
			p := bytes.Repeat([]byte("record\n"), 130)
			_, _ = w.Write(p)
			if _, e := w.Write(bytes.Repeat([]byte("next\n"), 100)); e == nil {
				t.Fatal("expected compression failure")
			}
			_ = w.Close()
			raw, _ := filepath.Glob(o.path + ".uvp-v1-*.log")
			gz, _ := filepath.Glob(o.path + ".uvp-v1-*.gz")
			if stage == "rollback_remove" && len(raw) != 1 {
				t.Fatal("failed unlink lost raw")
			}
			if stage == "unlink_directory_sync" && (len(raw) != 0 || len(gz) != 1) {
				t.Fatal("sync failure removed sole gzip")
			}
			o.before = nil
			w = mustWriter(t, o)
			_, size := managedFiles(t, o)
			if size > o.maxBytes*int64(o.maxBackups+1) {
				t.Fatal("recovery failed to restore budget")
			}
			if stage == "rollback_remove" {
				gz, _ = filepath.Glob(o.path + ".uvp-v1-*.gz")
				if len(gz) != 0 {
					t.Fatal("duplicate gzip survived recovery")
				}
				got, _ := os.ReadFile(raw[0])
				if !bytes.Equal(got, p) {
					t.Fatal("raw changed")
				}
			} else {
				f, _ := os.Open(gz[0])
				defer f.Close()
				z, e := gzip.NewReader(f)
				if e != nil {
					t.Fatal(e)
				}
				got, e := io.ReadAll(z)
				_ = z.Close()
				if e != nil || !bytes.Equal(got, p) {
					t.Fatal("sole gzip corrupted")
				}
			}
		})
	}
}

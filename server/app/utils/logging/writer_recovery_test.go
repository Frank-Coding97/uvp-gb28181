package logging

import (
	"bytes"
	"errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestLoggingWriterTransientFaultRecovery(t *testing.T) {
	for _, stage := range []string{"write", "sync", "maintain", "rotate_sync", "rotate_rename"} {
		t.Run(stage, func(t *testing.T) {
			options := writerOptionsForTest(t)
			now := options.now()
			options.now = func() time.Time { return now }
			failing := false
			options.before = func(op string) error {
				if !failing {
					return nil
				}
				if (stage == "sync" || stage == "rotate_sync") && op == "sync" {
					return syscall.EIO
				}
				if stage == "maintain" && op == "remove" {
					return syscall.ENOSPC
				}
				if stage == "rotate_rename" && op == "rename" {
					return syscall.ENOSPC
				}
				return nil
			}
			options.write = func(f *os.File, p []byte) (int, error) {
				if failing && stage == "write" {
					return 0, syscall.ENOSPC
				}
				return f.Write(p)
			}
			writer := mustWriter(t, options)
			record := append(append([]byte(`{"seed":"`), bytes.Repeat([]byte("x"), 68)...), []byte("\"}\n")...)
			if _, err := writer.Write(record); err != nil {
				t.Fatal(err)
			}
			if stage == "maintain" {
				if _, err := writer.Write(record); err != nil {
					t.Fatal(err)
				}
				now = now.Add(options.maxAge + time.Second)
			}
			failing = true
			var fault error
			switch stage {
			case "sync":
				fault = writer.Sync()
			case "maintain":
				fault = writer.Maintain()
			case "write":
				_, fault = writer.Write([]byte("{}\n"))
			default:
				_, fault = writer.Write(record)
			}
			if fault == nil {
				t.Fatal("injected failure was hidden")
			}
			failing = false
			if err := writer.Maintain(); err != nil {
				t.Fatalf("maintenance after transient fault: %v", err)
			}
			if n, err := writer.Write([]byte("{}\n")); err != nil || n != 3 {
				t.Fatalf("write after transient fault: n=%d err=%v", n, err)
			}
			if err := writer.Sync(); err != nil {
				t.Fatalf("sync after recovery: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close after recovery: %v", err)
			}
			contents, err := os.ReadFile(options.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(contents, append(record, []byte("{}\n")...)) {
				t.Fatal("recovery lost, replayed or damaged a record")
			}
		})
	}
}

func TestLoggingWriterPartialRecordRemainsStopped(t *testing.T) {
	options := writerOptionsForTest(t)
	failing := true
	options.write = func(f *os.File, p []byte) (int, error) {
		if failing {
			n, _ := f.Write(p[:2])
			return n, syscall.ENOSPC
		}
		return f.Write(p)
	}
	writer := mustWriter(t, options)
	if _, err := writer.Write([]byte("{\"record\":1}\n")); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	failing = false
	before, err := os.ReadFile(options.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Maintain(); err == nil {
		t.Fatal("partial record must require intervention")
	}
	if _, err := writer.Write([]byte("{\"record\":2}\n")); err == nil {
		t.Fatal("appended onto a partial record")
	}
	after, err := os.ReadFile(options.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("partial output modified during failed recovery")
	}
}

func TestLoggingWriterRecoveryPreservesUncertainRotation(t *testing.T) {
	for _, stage := range []string{"directory_sync", "open"} {
		t.Run(stage, func(t *testing.T) {
			options := writerOptionsForTest(t)
			failing := false
			options.before = func(op string) error {
				if failing && op == stage {
					return syscall.EIO
				}
				return nil
			}
			writer := mustWriter(t, options)
			record := bytes.Repeat([]byte("x"), 100)
			if _, err := writer.Write(record); err != nil {
				t.Fatal(err)
			}
			failing = true
			if _, err := writer.Write(record); err == nil {
				t.Fatal("rotation fault hidden")
			}
			failing = false
			if err := writer.Maintain(); err == nil {
				t.Fatal("uncertain namespace transition was recovered")
			}
			if _, err := writer.Write([]byte("new\n")); err == nil {
				t.Fatal("uncertain active was adopted")
			}
			if _, err := os.Stat(options.path); !os.IsNotExist(err) {
				t.Fatalf("active unexpectedly recreated: %v", err)
			}
			backups, err := writer.backups()
			if err != nil || len(backups) != 1 {
				t.Fatalf("original backup lost: files=%d err=%v", len(backups), err)
			}
		})
	}
}

func TestLoggingWriterCompressionFaultRecovery(t *testing.T) {
	for _, stage := range []string{"compress", "compress_write", "compress_close", "publish"} {
		t.Run(stage, func(t *testing.T) {
			options := writerOptionsForTest(t)
			options.compress = true
			failing := false
			options.before = func(op string) error {
				if failing && op == stage {
					return syscall.ENOSPC
				}
				return nil
			}
			writer := mustWriter(t, options)
			record := bytes.Repeat([]byte("x"), 100)
			if _, err := writer.Write(record); err != nil {
				t.Fatal(err)
			}
			failing = true
			if _, err := writer.Write(record); err == nil {
				t.Fatal("compression fault hidden")
			}
			failing = false
			if err := writer.Maintain(); err != nil {
				t.Fatalf("repair compression: %v", err)
			}
			if _, err := writer.Write([]byte("{}\n")); err != nil {
				t.Fatal(err)
			}
			backups, err := writer.backups()
			if err != nil || len(backups) != 1 {
				t.Fatalf("backup count=%d err=%v", len(backups), err)
			}
			current, err := os.ReadFile(options.path)
			if err != nil || string(current) != "{}\n" {
				t.Fatalf("active contents=%q err=%v", current, err)
			}
			count, size := managedFiles(t, options)
			if count != 2 || size > options.maxBytes*2 {
				t.Fatalf("recovery retention: count=%d bytes=%d", count, size)
			}
		})
	}
}

func TestLoggingWriterZeroCountCannotHideChangedLength(t *testing.T) {
	options := writerOptionsForTest(t)
	options.write = func(f *os.File, p []byte) (int, error) { _, _ = f.Write(p[:2]); return 0, syscall.EIO }
	writer := mustWriter(t, options)
	if _, err := writer.Write([]byte("record")); err == nil {
		t.Fatal("fault hidden")
	}
	if err := writer.Maintain(); err == nil {
		t.Fatal("n=0 with changed file length was recovered")
	}
}

func TestLoggingWriterRecoveryIdentityFailureIsSticky(t *testing.T) {
	options := writerOptionsForTest(t)
	options.write = func(*os.File, []byte) (int, error) { return 0, syscall.ENOSPC }
	writer := mustWriter(t, options)
	if _, err := writer.Write([]byte("{}\n")); err == nil {
		t.Fatal("fault hidden")
	}
	saved := options.path + ".saved"
	if err := os.Rename(options.path, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writer.Maintain(); err == nil {
		t.Fatal("replacement inode adopted")
	}
	if err := os.Remove(options.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, options.path); err != nil {
		t.Fatal(err)
	}
	if err := writer.Maintain(); err == nil {
		t.Fatal("identity violation lost its sticky state")
	}
}

func TestLoggingWriterRecoveryDoesNotReplayTee(t *testing.T) {
	options := writerOptionsForTest(t)
	options.maxBytes = 4096
	failing := true
	options.write = func(f *os.File, p []byte) (int, error) {
		if failing {
			return 0, syscall.ENOSPC
		}
		return f.Write(p)
	}
	writer := mustWriter(t, options)
	good, emergency := &memorySink{}, &memorySink{}
	cfg := configForTest(t, values{"logs.outputs": []string{"file", "stdout"}, "logs.textformat": "json", "logs.stdoutformat": "json"})
	runtime, err := NewRuntime(Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"file": writer, "stdout": good}, ErrorOutput: emergency})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	runtime.Root.Info("lost in file", zap.String("event", "test.first"))
	failing = false
	if err := runtime.Maintain(); err != nil {
		t.Fatal(err)
	}
	runtime.Root.Info("written in file", zap.String("event", "test.second"))
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	stats := runtime.Stats()
	if stats["file"].Attempted != 2 || stats["file"].Failed != 1 || stats["file"].Written != 1 || stats["stdout"].Written != 2 {
		t.Fatalf("recovery counters: %+v", stats)
	}
	if len(records(t, good)) != 2 {
		t.Fatal("healthy Tee sink was replayed")
	}
	contents, err := os.ReadFile(options.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte("test.first")) || !bytes.Contains(contents, []byte("test.second")) {
		t.Fatal("failed record replayed or subsequent record missing")
	}
}

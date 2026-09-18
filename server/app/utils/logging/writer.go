package logging

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type writerOptions struct {
	path       string
	maxBytes   int64
	maxBackups int
	maxAge     time.Duration
	compress   bool
	now        func() time.Time
	before     func(string) error
	write      func(*os.File, []byte) (int, error)
}
type fileWriter struct {
	mu           sync.Mutex
	options      writerOptions
	dir          *logDirectory
	active, lock *os.File
	base         string
	pattern      *regexp.Regexp
	lastStamp    int64
	historical   int64
	failed       error
	recoverable  bool
	failedSize   int64
	closed       bool
	closeErr     error
}

var errUnsafeLogFile = errors.New("unsafe logging file")
var errLogWriterClosed = errors.New("log writer is closed")

func openFileWriter(o writerOptions) (w *fileWriter, err error) {
	if o.maxBytes <= 0 || o.maxBackups <= 0 || o.maxAge <= 0 {
		return nil, errors.New("invalid log file limits")
	}
	if o.now == nil {
		o.now = time.Now
	}
	w = &fileWriter{options: o, base: filepath.Base(o.path)}
	w.pattern = regexp.MustCompile("^" + regexp.QuoteMeta(w.base) + `\.uvp-v1-([0-9]{20})\.log(\.gz|\.tmp)?$`)
	opened := w
	defer func() {
		if err != nil {
			if opened.active != nil {
				_ = opened.active.Close()
			}
			if opened.lock != nil {
				_ = opened.lock.Close()
			}
			if opened.dir != nil {
				_ = opened.dir.file.Close()
			}
		}
	}()
	if err = w.call("open"); err != nil {
		return nil, err
	}
	w.dir, err = openLogDirectory(filepath.Dir(o.path))
	if err != nil {
		return nil, err
	}
	w.dir.beforeSync = func() error { return w.call("directory_sync") }
	w.lock, err = w.dir.open(w.base+".uvp.lock", logAppend|logCreate)
	if err != nil {
		return nil, err
	}
	if err = lockLogFile(w.lock); err != nil {
		return nil, errors.New("log file is managed by another process")
	}
	if err = w.recoverTemps(); err != nil {
		return nil, err
	}
	if err = w.cleanup(o.maxBackups); err != nil {
		return nil, err
	}
	w.active, err = w.dir.open(w.base, logAppend|logCreate)
	if err != nil {
		return nil, err
	}
	s, e := w.active.Stat()
	if e != nil {
		return nil, e
	}
	if s.Size() > o.maxBytes {
		if err = w.call("rename"); err != nil {
			return nil, err
		}
		legacy := fmt.Sprintf("%s.legacy-%020d", w.base, o.now().UnixNano())
		if err = w.dir.rename(w.base, legacy, w.active); err != nil {
			return nil, err
		}
		_ = w.active.Close()
		w.active = nil
		w.active, err = w.dir.open(w.base, logAppend|logCreate|logExclusive)
		if err != nil {
			return nil, err
		}
	}
	entries, e := w.dir.entries()
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if entry.Name() == w.base || entry.Name() == w.base+".uvp.lock" || w.pattern.MatchString(entry.Name()) {
			continue
		}
		s, e := w.dir.stat(entry.Name())
		if e == nil {
			w.historical += s.Size
		}
	}
	return w, nil
}
func (w *fileWriter) call(op string) error {
	if w.options.before != nil {
		return w.options.before(op)
	}
	return nil
}
func (w *fileWriter) HistoricalBytes() int64 { return w.historical }
func (w *fileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errLogWriterClosed
	}
	if w.failed != nil {
		return 0, w.failed
	}
	if int64(len(p)) > w.options.maxBytes {
		return 0, errors.New("log record exceeds file capacity")
	}
	if e := w.dir.matches(w.base, w.active); e != nil {
		return 0, w.rememberFailure(e, false, -1)
	}
	stat, e := w.active.Stat()
	if e != nil {
		return 0, w.rememberFailure(e, false, -1)
	}
	writeSize := stat.Size()
	if stat.Size()+int64(len(p)) > w.options.maxBytes {
		w.recoverable = true // rotate narrows this at namespace mutation boundaries.
		if e = w.rotate(); e != nil {
			return 0, w.rememberFailure(e, w.recoverable, -1)
		}
		writeSize = 0 // rotate created this active file exclusively.
	}
	if e = w.call("write"); e != nil {
		return 0, w.rememberFailure(e, true, writeSize)
	}
	if e = w.dir.matches(w.base, w.active); e != nil {
		return 0, w.rememberFailure(e, false, -1)
	}
	var n int
	if w.options.write != nil {
		n, e = w.options.write(w.active, p)
	} else {
		n, e = w.active.Write(p)
	}
	if n != len(p) && e == nil {
		e = io.ErrShortWrite
	}
	if e != nil {
		w.rememberFailure(e, n == 0, writeSize)
	}
	return n, e
}
func (w *fileWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errLogWriterClosed
	}
	if w.failed != nil {
		return w.failed
	}
	e := w.call("sync")
	if e == nil && w.active != nil {
		e = w.active.Sync()
	}
	if e != nil {
		return w.rememberFailure(e, true, -1)
	}
	return e
}
func (w *fileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.closeErr
	}
	w.closed = true
	if w.active != nil {
		w.closeErr = errors.Join(w.call("sync"), w.active.Sync(), w.active.Close())
	}
	w.closeErr = errors.Join(w.closeErr, w.failed, w.lock.Close(), w.dir.file.Close())
	return w.closeErr
}
func (w *fileWriter) Maintain() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errLogWriterClosed
	}
	if w.failed != nil {
		return w.recoverFailure()
	}
	e := w.cleanup(w.options.maxBackups)
	if e != nil {
		return w.rememberFailure(e, true, -1)
	}
	return e
}

// Only the existing periodic maintenance retries a failed writer. Failed
// records are never retained or replayed, including when another Tee sink won.
func (w *fileWriter) rememberFailure(err error, recoverable bool, expectedSize int64) error {
	if w.failed != nil && !w.recoverable {
		return w.failed
	}
	w.failed = err
	w.recoverable = recoverable && !errors.Is(err, errUnsafeLogFile) && w.active != nil
	if w.recoverable {
		stat, e := w.active.Stat()
		if e != nil || (expectedSize >= 0 && stat.Size() != expectedSize) {
			w.recoverable = false
		} else {
			w.failedSize = stat.Size()
		}
	}
	return err
}

func (w *fileWriter) recoverFailure() error {
	if !w.recoverable {
		return w.failed
	}
	if e := w.dir.matches(w.base, w.active); e != nil {
		return w.rememberFailure(e, false, -1)
	}
	stat, e := w.active.Stat()
	if e != nil || stat.Size() != w.failedSize || stat.Size() > w.options.maxBytes {
		return w.rememberFailure(errUnsafeLogFile, false, -1)
	}
	// Finish durability and interrupted compression before permitting another
	// rotation. A renamed/closed/missing active is never adopted or reopened.
	for _, repair := range []func() error{w.dir.sync, w.recoverTemps, func() error { return w.cleanup(w.options.maxBackups) }, func() error {
		if e := w.call("sync"); e != nil {
			return e
		}
		return w.active.Sync()
	}} {
		if e := repair(); e != nil {
			w.failed = e
			w.recoverable = !errors.Is(e, errUnsafeLogFile)
			return e
		}
	}
	w.failed, w.recoverable = nil, false
	return nil
}

func (w *fileWriter) rotate() error {
	if e := w.cleanup(w.options.maxBackups - 1); e != nil {
		return e
	}
	stamp := w.options.now().UnixNano()
	if stamp <= w.lastStamp {
		stamp = w.lastStamp + 1
	}
	w.lastStamp = stamp
	name := fmt.Sprintf("%s.uvp-v1-%020d.log", w.base, stamp)
	if e := w.call("sync"); e != nil {
		return e
	}
	if e := w.active.Sync(); e != nil {
		return e
	}
	if e := w.call("rename"); e != nil {
		return e
	}
	w.recoverable = false // rename may commit before directory sync reports failure.
	if e := w.dir.rename(w.base, name, w.active); e != nil {
		return e
	}
	if e := w.active.Close(); e != nil {
		return e
	}
	w.active = nil
	if e := w.call("open"); e != nil {
		return e
	}
	f, e := w.dir.open(w.base, logAppend|logCreate|logExclusive)
	if e != nil {
		return e
	}
	w.active = f
	w.recoverable = true // a fresh active exists; compression can be repaired.
	if w.options.compress {
		return w.compress(name)
	}
	return nil
}

type backup struct {
	name  string
	stamp int64
	size  int64
	ref   fileIdentity
}

func (w *fileWriter) backups() ([]backup, error) {
	entries, e := w.dir.entries()
	if e != nil {
		return nil, e
	}
	var out []backup
	for _, entry := range entries {
		m := w.pattern.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		s, e := w.dir.stat(entry.Name())
		if e != nil {
			return nil, e
		}
		stamp, e := strconv.ParseInt(m[1], 10, 64)
		if e != nil {
			return nil, errUnsafeLogFile
		}
		if stamp > w.lastStamp {
			w.lastStamp = stamp
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			return nil, errors.New("unfinished log compression")
		}
		if s.Size > w.options.maxBytes {
			return nil, fmt.Errorf("%w: managed backup exceeds capacity", errUnsafeLogFile)
		}
		out = append(out, backup{entry.Name(), stamp, s.Size, identity(s)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].stamp < out[j].stamp })
	return out, nil
}
func (w *fileWriter) remove(name string) error {
	s, e := w.dir.stat(name)
	if e != nil {
		return e
	}
	return w.removeRef(name, identity(s))
}
func (w *fileWriter) removeRef(name string, ref fileIdentity) error {
	if e := w.call("remove"); e != nil {
		return e
	}
	return w.dir.remove(name, ref)
}
func (w *fileWriter) cleanup(limit int) error {
	files, e := w.backups()
	if e != nil {
		return e
	}
	remaining := len(files)
	cutoff := w.options.now().Add(-w.options.maxAge).UnixNano()
	for _, f := range files {
		if f.stamp < cutoff || remaining > limit {
			if e = w.removeRef(f.name, f.ref); e != nil {
				return e
			}
			remaining--
		}
	}
	return nil
}

// Compression publishes a complete synced gzip before removing raw. Rollback
// is permitted only while the original inode is still present.
func (w *fileWriter) compress(name string) (err error) {
	if err = w.call("compress"); err != nil {
		return err
	}
	source, err := w.dir.open(name, logRead)
	if err != nil {
		return err
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil {
		return err
	}
	if stat.Size() > w.options.maxBytes {
		return errUnsafeLogFile
	}
	sourceRef, err := openedIdentity(source)
	if err != nil {
		return err
	}
	tempName := name + ".tmp"
	temp, err := w.dir.open(tempName, logAppend|logCreate|logExclusive)
	if err != nil {
		return err
	}
	tempRef, err := openedIdentity(temp)
	if err != nil {
		_ = temp.Close()
		return err
	}
	cleanupName := tempName
	defer func() {
		if err != nil && w.dir.matches(name, source) == nil {
			cleanupErr := w.removeRef(cleanupName, tempRef)
			if !errors.Is(cleanupErr, os.ErrNotExist) {
				err = errors.Join(err, cleanupErr)
			}
		}
		_ = temp.Close()
	}()
	output := &boundedCompressionWriter{writer: temp, remaining: w.options.maxBytes + (1 << 20), before: func() error { return w.call("compress_write") }}
	z := gzip.NewWriter(output)
	_, copyErr := io.Copy(z, io.LimitReader(source, w.options.maxBytes+1))
	err = errors.Join(copyErr, w.call("compress_close"), z.Close())
	if err != nil {
		return err
	}
	if err = w.call("sync"); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	info, err := temp.Stat()
	if err != nil {
		return err
	}
	if info.Size() >= stat.Size() {
		return w.removeRef(tempName, tempRef)
	}
	if err = w.call("publish"); err != nil {
		return err
	}
	if err = w.dir.matches(name, source); err != nil {
		return err
	}
	err = w.dir.rename(tempName, name+".gz", temp)
	if w.dir.matches(name+".gz", temp) == nil {
		cleanupName = name + ".gz"
	}
	if err != nil {
		return err
	}
	// If unlink succeeds but its directory sync fails, raw is absent: the defer
	// deliberately keeps gzip as the only complete copy.
	return w.removeRef(name, sourceRef)
}

type boundedCompressionWriter struct {
	writer    io.Writer
	remaining int64
	before    func() error
}

func (w *boundedCompressionWriter) Write(p []byte) (int, error) {
	if w.before != nil {
		if e := w.before(); e != nil {
			return 0, e
		}
	}
	if int64(len(p)) > w.remaining {
		return 0, errors.New("compressed log exceeds temporary capacity")
	}
	n, e := w.writer.Write(p)
	w.remaining -= int64(n)
	if n != len(p) && e == nil {
		e = io.ErrShortWrite
	}
	return n, e
}
func (w *fileWriter) validateGzip(name string) error {
	f, e := w.dir.open(name, logRead)
	if e != nil {
		return e
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil {
		return e
	}
	if stat.Size() > w.options.maxBytes+(1<<20) {
		return errUnsafeLogFile
	}
	z, e := gzip.NewReader(f)
	if e != nil {
		return errors.Join(errUnsafeLogFile, e)
	}
	n, readErr := io.Copy(io.Discard, io.LimitReader(z, w.options.maxBytes+1))
	e = errors.Join(readErr, z.Close())
	if n > w.options.maxBytes {
		return errUnsafeLogFile
	}
	if e != nil {
		return errors.Join(errUnsafeLogFile, e)
	}
	return nil
}
func (w *fileWriter) recoverTemps() error {
	entries, e := w.dir.entries()
	if e != nil {
		return e
	}
	for _, entry := range entries {
		name := entry.Name()
		if !w.pattern.MatchString(name) {
			continue
		}
		if _, e = w.dir.stat(name); e != nil {
			return e
		}
		switch {
		case strings.HasSuffix(name, ".tmp"):
			raw := strings.TrimSuffix(name, ".tmp")
			if _, e = w.dir.stat(raw); e != nil {
				return errors.Join(errUnsafeLogFile, errors.New("orphan log compression temporary file"), e)
			}
			if e = w.remove(name); e != nil {
				return e
			}
		case strings.HasSuffix(name, ".gz"):
			if e = w.validateGzip(name); e != nil {
				return e
			}
			raw := strings.TrimSuffix(name, ".gz")
			if _, e = w.dir.stat(raw); e == nil {
				if e = w.remove(name); e != nil {
					return e
				}
			} else if !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
	}
	return nil
}

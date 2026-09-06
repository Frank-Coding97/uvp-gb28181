package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type SinkStats struct{ Attempted, Written, Failed, DurabilityUnknown uint64 }
type trackedSink struct {
	target                                 string
	writer                                 zapcore.WriteSyncer
	emergency                              *emergencyWriter
	attempted, written, failed, durability atomic.Uint64
}

const reportedSinkFailure = "logging sink failure already reported"

var errReportedSink = errors.New(reportedSinkFailure)

func (s *trackedSink) Write(p []byte) (int, error) {
	s.attempted.Add(1)
	n, e := s.writer.Write(p)
	if n != len(p) && e == nil {
		e = io.ErrShortWrite
	}
	if e != nil {
		s.failed.Add(1)
		s.report(e)
		return n, errReportedSink
	}
	s.written.Add(1)
	return n, nil
}
func (s *trackedSink) Sync() error {
	if e := s.writer.Sync(); e != nil {
		s.durability.Add(1)
		s.report(e)
		return errReportedSink
	}
	return nil
}
func (s *trackedSink) Close() error {
	if c, ok := s.writer.(io.Closer); ok {
		if e := c.Close(); e != nil {
			s.durability.Add(1)
			s.report(e)
			return errReportedSink
		}
	}
	return nil
}
func (s *trackedSink) report(e error) {
	s.emergency.report(s.target, ErrorClass(e), s.failed.Load(), s.durability.Load())
}
func (s *trackedSink) snapshot() SinkStats {
	return SinkStats{s.attempted.Load(), s.written.Load(), s.failed.Load(), s.durability.Load()}
}
func (r *Runtime) Stats() map[string]SinkStats {
	out := make(map[string]SinkStats, len(r.tracked))
	for key, s := range r.tracked {
		out[key] = s.snapshot()
	}
	return out
}

type emergencyState struct {
	last               time.Time
	failed, durability uint64
	class              string
	pending            bool
	emitted            bool
}
type emergencyWriter struct {
	mu        sync.Mutex
	out       io.Writer
	now       func() time.Time
	states    map[string]*emergencyState
	zapErrors uint64
	failed    bool
}

func newEmergencyWriter(out io.Writer, now func() time.Time) *emergencyWriter {
	if out == nil {
		out = os.Stderr
	}
	if now == nil {
		now = time.Now
	}
	return &emergencyWriter{out: out, now: now, states: map[string]*emergencyState{}}
}
func (e *emergencyWriter) report(target, class string, failed, durability uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reportLocked(target, class, failed, durability)
}
func (e *emergencyWriter) reportLocked(target, class string, failed, durability uint64) {
	s := e.states[target]
	if s == nil {
		s = &emergencyState{}
		e.states[target] = s
	}
	s.failed = failed
	s.durability = durability
	s.class = class
	s.pending = true
	now := e.now()
	if !s.emitted || now.Sub(s.last) >= time.Minute {
		e.emit(target, s, now)
	}
}
func (e *emergencyWriter) emit(target string, s *emergencyState, now time.Time) {
	row := struct {
		Time       string `json:"created_at"`
		Event      string `json:"event"`
		Sink       string `json:"sink"`
		Class      string `json:"error_class"`
		Failed     uint64 `json:"failed"`
		Durability uint64 `json:"durability_unknown"`
	}{now.Format("2006-01-02T15:04:05.000Z07:00"), "logging.sink_failure", target, s.class, s.failed, s.durability}
	p, err := json.Marshal(row)
	if err == nil {
		p = append(p, '\n')
		var n int
		n, err = e.out.Write(p)
		if n != len(p) && err == nil {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		e.failed = true
	}
	s.last = now
	s.emitted = true
	s.pending = false
}

// Zap formats its internal errors before calling ErrorOutput. Never copy that
// text. Failures already observed by trackedSink are not counted twice.
func (e *emergencyWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(reportedSinkFailure)) {
		return len(p), nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.zapErrors++
	e.reportLocked("zap", "internal", e.zapErrors, 0)
	return len(p), nil
}
func (e *emergencyWriter) Sync() error { return nil }
func (e *emergencyWriter) flush(force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	for name, s := range e.states {
		if s.pending && (force || now.Sub(s.last) >= time.Minute) {
			e.emit(name, s, now)
		}
	}
	if e.failed {
		return errors.New("emergency logging output failed")
	}
	return nil
}

type standardStream struct{ io.Writer }

// Standard streams are unbuffered and owned by the process, not the runtime.
func (standardStream) Sync() error { return nil }

func OpenRuntime(opts Options) (*Runtime, error) {
	if opts.Sinks != nil {
		return NewRuntime(opts)
	}
	if e := opts.Config.validate(); e != nil {
		return nil, e
	}
	opts.Sinks = make(map[string]zapcore.WriteSyncer, len(opts.Config.Outputs))
	var file *fileWriter
	for _, target := range opts.Config.Outputs {
		switch target {
		case "stdout":
			opts.Sinks[target] = standardStream{os.Stdout}
		case "file":
			var e error
			file, e = openFileWriter(writerOptions{path: opts.Config.FilePath, maxBytes: int64(opts.Config.MaxSizeMB) << 20, maxBackups: opts.Config.MaxBackups, maxAge: time.Duration(opts.Config.MaxAgeDays) * 24 * time.Hour, compress: opts.Config.Compress})
			if e != nil {
				return nil, e
			}
			opts.Sinks[target] = file
		}
	}
	r, e := NewRuntime(opts)
	if e != nil && file != nil {
		_ = file.Close()
	}
	return r, e
}

// Maintain is called by the one shared maintenance loop (retention and repeat
// summaries); it never starts its own goroutine.
func (r *Runtime) Maintain() error {
	// Never recursively acquire gate.RLock while emitting a summary; Close may
	// already be waiting for the write lock.
	r.repeats.maintain()
	r.gate.RLock()
	defer r.gate.RUnlock()
	if r.closed.Load() {
		return errLogWriterClosed
	}
	var result error
	for _, s := range r.tracked {
		if w, ok := s.writer.(interface{ Maintain() error }); ok {
			if e := w.Maintain(); e != nil {
				s.report(e)
				result = errors.Join(result, errReportedSink)
			}
		}
	}
	return errors.Join(result, r.emergency.flush(false))
}

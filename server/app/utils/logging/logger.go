package logging

import (
	"errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Config                     Config
	Service, Version, Instance string
	Clock                      zapcore.Clock
	// Runtime owns supplied sinks. Standard-stream wrappers must not close
	// process descriptors. OpenRuntime handles real file creation.
	Sinks       map[string]zapcore.WriteSyncer
	ErrorOutput zapcore.WriteSyncer
}
type Runtime struct {
	Root                             *zap.Logger
	gate                             sync.RWMutex
	tracked                          map[string]*trackedSink
	emergency                        *emergencyWriter
	config                           Config
	sinks                            []zapcore.WriteSyncer
	closed                           atomic.Bool
	closeOnce, reloadOnce            sync.Once
	closeErr                         error
	repeats                          *Repeater
	maintenanceStop, maintenanceDone chan struct{}
}

func NewRuntime(opts Options) (*Runtime, error) {
	cfg := opts.Config.clone()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if _, ok := cfg.Modules["access"]; !ok {
		cfg.Modules["access"] = zapcore.InfoLevel
	}
	minLevel := cfg.Level
	uniformLevel := true
	for _, l := range cfg.Modules {
		uniformLevel = uniformLevel && l == cfg.Level
		if l < minLevel {
			minLevel = l
		}
	}
	if opts.Service == "" {
		opts.Service = "uvp-gb28181"
	}
	if opts.Version == "" {
		opts.Version = "unknown"
	}
	if opts.Instance == "" {
		opts.Instance = "unknown"
	}
	opts.Service, _ = clipJSON(opts.Service, 256)
	opts.Version, _ = clipJSON(opts.Version, 256)
	opts.Instance, _ = clipJSON(opts.Instance, 256)
	now := time.Now
	if opts.Clock != nil {
		now = opts.Clock.Now
	}
	r := &Runtime{config: cfg, tracked: make(map[string]*trackedSink), emergency: newEmergencyWriter(opts.ErrorOutput, now)}
	cores := make([]zapcore.Core, 0, len(cfg.Outputs))
	for _, target := range cfg.Outputs {
		sink := opts.Sinks[target]
		if sink == nil {
			return nil, errors.New("configured logging sink is unavailable")
		}
		tracked := &trackedSink{target: target, writer: sink, emergency: r.emergency}
		r.tracked[target] = tracked
		sink = tracked
		format := cfg.FileFormat
		if target == "stdout" {
			format = cfg.StdoutFormat
		}
		enc := zap.NewProductionEncoderConfig()
		enc.TimeKey = "created_at"
		enc.MessageKey = "message"
		enc.NameKey = "component"
		enc.StacktraceKey = "stack"
		enc.EncodeTime = func(t time.Time, a zapcore.PrimitiveArrayEncoder) {
			a.AppendString(t.Format("2006-01-02T15:04:05.000Z07:00"))
		}
		enc.EncodeDuration = zapcore.MillisDurationEncoder
		var encoder zapcore.Encoder
		if format == "json" {
			encoder = zapcore.NewJSONEncoder(enc)
		} else {
			encoder = zapcore.NewConsoleEncoder(enc)
		}
		cores = append(cores, zapcore.NewCore(encoder, sink, zapcore.DebugLevel))
		r.sinks = append(r.sinks, sink)
	}
	inner := zapcore.NewTee(cores...).With([]zap.Field{zap.String("service", opts.Service), zap.String("version", opts.Version), zap.String("instance", opts.Instance)})
	core := &runtimeCore{inner: inner, config: cfg, min: minLevel, uniformLevel: uniformLevel, keyPolicies: &fieldKeyCache{}, closed: &r.closed, gate: &r.gate, bound: map[string]bool{}}
	options := []zap.Option{zap.ErrorOutput(r.emergency)}
	if opts.Clock != nil {
		options = append(options, zap.WithClock(opts.Clock))
	}
	r.Root = zap.New(core, options...)
	r.repeats = newRepeater(r.Root, now)
	r.maintenanceStop = make(chan struct{})
	r.maintenanceDone = make(chan struct{})
	clock := opts.Clock
	if clock == nil {
		clock = zapcore.DefaultClock
	}
	ticker := clock.NewTicker(time.Minute)
	go func() {
		defer close(r.maintenanceDone)
		defer ticker.Stop()
		for {
			select {
			case <-r.maintenanceStop:
				return
			case <-ticker.C:
				_ = r.Maintain()
			}
		}
	}()
	return r, nil
}
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		close(r.maintenanceStop)
		<-r.maintenanceDone
		// Flush before taking the write gate: summary emission takes its read lock.
		r.repeats.close()
		r.gate.Lock()
		defer r.gate.Unlock()
		r.closed.Store(true)
		r.closeErr = r.Root.Sync()
		for _, sink := range r.sinks {
			if c, ok := sink.(io.Closer); ok {
				r.closeErr = errors.Join(r.closeErr, c.Close())
			}
		}
		r.closeErr = errors.Join(r.closeErr, r.emergency.flush(true))
	})
	return r.closeErr
}

// Repeats provides explicit background-state aggregation; ordinary logs bypass it.
func (r *Runtime) Repeats() *Repeater {
	if r == nil {
		return nil
	}
	return r.repeats
}
func (r *Runtime) Config() Config { return r.config.clone() }
func (r *Runtime) NoticeReload(cfg Config) {
	if !reflect.DeepEqual(r.config, cfg) {
		r.reloadOnce.Do(func() {
			r.Root.Warn("logging configuration changed; restart required", zap.String("event", "logging.restart_required"))
		})
	}
}

// WithIdentity binds trusted correlation fields once. Ordinary With/entry
// fields cannot overwrite them. Callers generate or validate identifiers
// before binding; no global logger is modified.
func WithIdentity(logger *zap.Logger, fields ...zap.Field) *zap.Logger {
	if logger == nil {
		return zap.NewNop()
	}
	return logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		if r, ok := c.(*runtimeCore); ok {
			return r.with(fields, true)
		}
		return c.With(fields)
	}))
}

type runtimeCore struct {
	inner        zapcore.Core
	config       Config
	min          zapcore.Level
	uniformLevel bool
	keyPolicies  *fieldKeyCache
	closed       *atomic.Bool
	gate         *sync.RWMutex
	bound        map[string]bool
	used         int
	truncated    bool
}

func (c *runtimeCore) Enabled(l zapcore.Level) bool { return l >= c.min }
func (c *runtimeCore) level(name string) zapcore.Level {
	for name != "" {
		if l, ok := c.config.Modules[name]; ok {
			return l
		}
		i := strings.LastIndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return c.config.Level
}
func (c *runtimeCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if (c.uniformLevel && e.Level >= c.config.Level) || (!c.uniformLevel && e.Level >= c.level(e.LoggerName)) {
		return ce.AddCore(e, c)
	}
	return ce
}
func (c *runtimeCore) With(fields []zapcore.Field) zapcore.Core { return c.with(fields, false) }
func (c *runtimeCore) with(fields []zap.Field, trusted bool) *runtimeCore {
	clone := *c
	clone.bound = make(map[string]bool, len(c.bound)+len(fields))
	for k, v := range c.bound {
		clone.bound[k] = v
	}
	clean := make([]zap.Field, 0, min(len(fields), maxFields))
	for _, f := range fields {
		if clone.bound[f.Key] || fixedKey(f.Key) || (!trusted && identityKey(f.Key)) {
			continue
		}
		if trusted && !identityKey(f.Key) {
			continue
		}
		clean = append(clean, f)
		clone.bound[f.Key] = true
	}
	clone.bound = make(map[string]bool, len(c.bound))
	for k, v := range c.bound {
		clone.bound[k] = v
	}
	clean, used, truncated := sanitizeFields(clean, max(0, fieldBudget-c.used-128), max(0, maxFields-len(c.bound)-1))
	for _, f := range clean {
		clone.bound[f.Key] = true
	}
	clone.used += used
	clone.truncated = c.truncated || truncated
	clone.inner = c.inner.With(clean)
	return &clone
}
func (c *runtimeCore) Write(e zapcore.Entry, fields []zap.Field) error {
	c.gate.RLock()
	defer c.gate.RUnlock()
	if c.closed.Load() {
		return errors.New("logging runtime is closed")
	}
	if e.LoggerName == "" {
		e.LoggerName = "app"
	}
	if c.canWriteUnchanged(e, fields) {
		return c.inner.Write(e, fields)
	}
	clean := make([]zap.Field, 0, len(fields)+1)
	hasEvent := c.bound["event"]
	for _, f := range fields {
		if fixedKey(f.Key) || identityKey(f.Key) || c.bound[f.Key] {
			continue
		}
		if f.Key == "event" {
			hasEvent = true
		}
		clean = append(clean, f)
	}
	if !hasEvent {
		clean = append(clean, zap.String("event", "legacy.log"))
	}
	var cutMessage, cutName, cutStack bool
	e.Message, cutMessage = clipJSON(e.Message, 1024)
	e.LoggerName, cutName = clipJSON(e.LoggerName, 256)
	e.Stack, cutStack = clipJSON(e.Stack, min(MaxStackBytes, max(0, fieldBudget-c.used-128)))
	clean, _, cutFields := sanitizeFields(clean, fieldBudget-c.used-encodedStringSize(e.Stack), maxFields-len(c.bound))
	if c.truncated || cutMessage || cutName || cutStack || cutFields {
		clean = append(clean, zap.Bool("truncated", true))
	}
	return c.inner.Write(e, clean)
}
func (c *runtimeCore) Sync() error { return c.inner.Sync() }
func fixedKey(k string) bool {
	switch k {
	case "created_at", "level", "service", "version", "instance", "component", "message", "truncated":
		return true
	}
	return false
}
func identityKey(k string) bool {
	switch k {
	case "request_id", "client_request_id", "execution_id":
		return true
	}
	return false
}

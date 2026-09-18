package logging

import (
	"context"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"log"
	"log/slog"
	"strings"
)

type standardWriter struct{ logger *zap.Logger }

func StandardWriter(logger *zap.Logger) io.Writer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return standardWriter{logger}
}
func (w standardWriter) Write(p []byte) (int, error) {
	w.logger.Info("third-party log message omitted", zap.String("event", "legacy.log"), zap.String("source", "stdlib"), zap.Bool("text_omitted", true))
	return len(p), nil
}

// InstallStandardBridge is one-way. It does not replace slog.Default, so the
// standard library cannot build a log -> slog -> log recursion.
func InstallStandardBridge(logger *zap.Logger) func() {
	out, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(StandardWriter(logger))
	log.SetFlags(0)
	log.SetPrefix("")
	return func() { log.SetOutput(out); log.SetFlags(flags); log.SetPrefix(prefix) }
}

type slogHandler struct {
	logger *zap.Logger
	group  string
}

func NewSlogHandler(logger *zap.Logger) slog.Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &slogHandler{logger: logger}
}
func (h *slogHandler) Enabled(_ context.Context, l slog.Level) bool {
	return h.logger.Core().Enabled(slogLevel(l))
}
func slogLevel(l slog.Level) zapcore.Level {
	switch {
	case l >= slog.LevelError:
		return zapcore.ErrorLevel
	case l >= slog.LevelWarn:
		return zapcore.WarnLevel
	case l >= slog.LevelInfo:
		return zapcore.InfoLevel
	default:
		return zapcore.DebugLevel
	}
}
func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make([]zap.Field, 0, min(r.NumAttrs()+3, maxFields))
	fields = append(fields, zap.String("event", "legacy.log"), zap.String("source", "slog"), zap.Bool("text_omitted", true))
	r.Attrs(func(a slog.Attr) bool { fields = appendSlogAttr(fields, h.group, a, 0); return len(fields) < maxFields })
	h.logger.Log(slogLevel(r.Level), "third-party log message omitted", fields...)
	return nil
}
func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := make([]zap.Field, 0, min(len(attrs), maxFields))
	for _, a := range attrs {
		fields = appendSlogAttr(fields, h.group, a, 0)
		if len(fields) >= maxFields {
			break
		}
	}
	return &slogHandler{logger: h.logger.With(fields...), group: h.group}
}
func (h *slogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	name, _ = clipJSON(name, 64)
	group, _ := clipJSON(strings.TrimPrefix(h.group+"."+name, "."), 128)
	return &slogHandler{logger: h.logger, group: group}
}
func appendSlogAttr(fields []zap.Field, group string, a slog.Attr, depth int) []zap.Field {
	if len(fields) >= maxFields || depth >= 4 {
		return fields
	}
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	switch a.Value.Kind() {
	case slog.KindGroup:
		for _, child := range a.Value.Group() {
			fields = appendSlogAttr(fields, key, child, depth+1)
		}
	case slog.KindString:
		fields = append(fields, zap.String(key, a.Value.String()))
	case slog.KindBool:
		fields = append(fields, zap.Bool(key, a.Value.Bool()))
	case slog.KindInt64:
		fields = append(fields, zap.Int64(key, a.Value.Int64()))
	case slog.KindUint64:
		fields = append(fields, zap.Uint64(key, a.Value.Uint64()))
	case slog.KindFloat64:
		fields = append(fields, zap.Float64(key, a.Value.Float64()))
	case slog.KindDuration:
		fields = append(fields, zap.Duration(key, a.Value.Duration()))
	case slog.KindTime:
		fields = append(fields, zap.Time(key, a.Value.Time()))
	default:
		// Do not call Value.Resolve: arbitrary LogValuers may expose credentials.
		value := a.Value.Any()
		if err, ok := value.(error); ok {
			fields = append(fields, zap.NamedError(key, err))
		} else {
			fields = append(fields, zap.Reflect(key, value))
		}
	}
	return fields
}

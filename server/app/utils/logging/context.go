package logging

import (
	"context"
	"go.uber.org/zap"
)

type loggerContextKey struct{}

// WithContext attaches immutable log scope to standard context. Existing scope
// is retained when child work derives cancellation, deadlines or values.
func WithContext(ctx context.Context, logger *zap.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}
func FromContext(ctx context.Context, fallback *zap.Logger) *zap.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerContextKey{}).(*zap.Logger); ok && logger != nil {
			return logger
		}
	}
	if fallback == nil {
		return zap.NewNop()
	}
	return fallback
}

package policyexitboundaries

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Run(logger *zap.Logger, entry *zapcore.CheckedEntry, value any) {
	logger.Sugar().With("event", "fixture.format").Infof("%v", value)
	_ = logger.Check(zap.InfoLevel, "check")
	if entry != nil {
		entry.Write()
	}
	logger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return zapcore.NewNopCore()
	}))
}

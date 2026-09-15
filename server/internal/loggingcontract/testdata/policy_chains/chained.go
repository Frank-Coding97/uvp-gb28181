package policychains

import (
	"log"
	"log/slog"

	"go.uber.org/zap"
)

func Chained(logger *zap.Logger, slogger *slog.Logger, payload any, err error) {
	logger.Named("child").With(zap.String("event", "fixture.chain")).Info("chained logger")
	logger.With(zap.String("event", "fixture.with"), zap.Any("payload", payload)).Info("with logger")
	logger.WithOptions(zap.AddCaller()).Info("option logger", zap.String("event", "fixture.options"))
	logger.Log(zap.InfoLevel, "level logger", zap.String("event", "fixture.log"))

	fields := []zap.Field{zap.String("event", "fixture.slice"), zap.Any("payload", payload)}
	logger.Info("slice logger", fields...)

	slogger.Info("raw slog", "event", "fixture.slog.raw", "payload", payload)
	slogger.With("event", "fixture.slog.chain", "payload", payload).Info("slog chain")

	logger.Sugar().Infof("failed: %s", err.Error())
	log.Printf("failed: %s", err.Error())
}

func Independent() *zap.Logger {
	return zap.NewExample()
}

func IndependentBuilt() *zap.Logger {
	logger, _ := zap.NewProductionConfig().Build()
	return logger
}

package policyfunctionalias

import "go.uber.org/zap"

func Run() *zap.Logger {
	factory := zap.NewProduction
	logger, _ := factory()
	return logger
}

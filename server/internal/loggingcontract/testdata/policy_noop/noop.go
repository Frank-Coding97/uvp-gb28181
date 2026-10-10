package policynoop

import "go.uber.org/zap"

func Fallback() *zap.Logger {
	return zap.NewNop()
}

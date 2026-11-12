package negativeclean

import "go.uber.org/zap"

func Good() {
	logger := zap.NewNop()
	logger.Info("safe payload", zap.String("kind", "sample"))
}

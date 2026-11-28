package policylegacy

import "go.uber.org/zap"

func Historical(logger *zap.Logger) {
	logger.Info("historic message")
}
